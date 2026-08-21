package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/k8ssource"
	"ops-cmdb-backend/logx"
)

// K8sClusterHandler 管理多集群纳管（只读）。凭据 AES 加密存，测连通只 list nodes。
type K8sClusterHandler struct {
	Store  *store.Store
	DB     *sql.DB
	Cipher *crypto.Cipher
	Pool   *k8ssource.Pool
}

func NewK8sClusterHandler(st *store.Store, db *sql.DB, cipher *crypto.Cipher, pool *k8ssource.Pool) *K8sClusterHandler {
	return &K8sClusterHandler{Store: st, DB: db, Cipher: cipher, Pool: pool}
}

func (h *K8sClusterHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/clusters", h.List)
	r.POST("/k8s/clusters", h.Create)
	r.PUT("/k8s/clusters/:id", h.Update)
	r.DELETE("/k8s/clusters/:id", h.Delete)
	r.POST("/k8s/clusters/:id/test", h.Test)
	r.POST("/k8s/clusters/discover", h.DiscoverGKE) // 用云账号 SA key 列出某 project 的 GKE 集群
}

// DiscoverGKE 用云账号项目的 SA key 列出该 GCP project 下所有 GKE 集群（供勾选纳管）。
func (h *K8sClusterHandler) DiscoverGKE(c *gin.Context) {
	var in struct {
		CloudAccountID int    `json:"cloud_account_id"`
		ProjectID      string `json:"project_id"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.CloudAccountID == 0 || in.ProjectID == "" {
		httpx.Required(c, "cloud_account_id/project_id")
		return
	}
	var enc sql.NullString
	e := h.DB.QueryRow(`SELECT cred_enc FROM cloud_account_projects WHERE account_id=? AND project_id=?`, in.CloudAccountID, in.ProjectID).Scan(&enc)
	if e != nil || !enc.Valid || enc.String == "" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "该云账号项目未配 SA key（去 系统管理→云账号 配）", "error_key": "error.noSaKey"})
		return
	}
	saJSON, e := h.Cipher.Decrypt(enc.String)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "解密 SA key 失败", "error_key": "error.decryptSaKeyFailed"})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	clusters, err := k8ssource.DiscoverGKE(ctx, []byte(saJSON), in.ProjectID)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	logx.J("k8s", "gke_discover", map[string]any{"account": in.CloudAccountID, "project": in.ProjectID, "found": len(clusters)})
	c.JSON(http.StatusOK, gin.H{"ok": true, "clusters": clusters})
}

type k8sClusterOut struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	PromClusterValue string `json:"prom_cluster_value"` // 指标里 cluster 标签的取值（空=用 name）
	NetworkExposure  string `json:"network_exposure"`   // public/private/空=按节点公网IP推断
	AllowSecretInv   bool   `json:"allow_secret_inventory"`
	DisplayName      string `json:"display_name"`
	Environment      string `json:"environment"`
	Provider         string `json:"provider"`
	ProjectID        string `json:"project_id"`
	CloudAccountID   int    `json:"cloud_account_id"` // 引用主机模块的云账号(GCP SA key 复用，不重复存)
	CloudAccount     string `json:"cloud_account"`    // 云账号名(展示)
	Location         string `json:"location"`
	Endpoint         string `json:"endpoint"`
	NodepoolLabel    string `json:"nodepool_label"` // 节点池标签 key（空=按角色/default 兜底）
	CostMode         string `json:"cost_mode"`      // cloud/idc/none，空=按 provider 自动推断
	HasKubeconfig    bool   `json:"has_kubeconfig"` // 不回传明文
	Enabled          int    `json:"enabled"`
}

// clusterGone 校验 cluster_id 指向的集群确实存在；不存在就写好响应并返回 true（调用方直接 return）。
//
// ⚠️ 为什么必须查一次库：原先各接口只校验「cluster_id 必填」，不校验「这个集群存在」，
// 于是对一个已删除/根本不存在的集群，体检返回 200 +「未发现有风险的安全上下文配置」、
// 孤儿资源返回 200 + 空列表、暴露面返回 200 + 空列表——**把"查不到这个集群"说成了"这个集群没问题"**。
// 只有 /k8s/events 一个接口老实报错，口径完全不统一。查不到对象必须是 404，不能是"一切正常"。
func clusterGone(c *gin.Context, db *sql.DB, cid int) bool {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM k8s_clusters WHERE id=?`, cid).Scan(&n); err != nil {
		logx.J("k8s", "cluster_check_err", map[string]any{"cluster_id": cid, "err": err.Error()})
		httpx.Fail(c, httpx.CodeInternal, fmt.Errorf("校验集群是否存在失败: %w", err), nil)
		return true
	}
	if n == 0 {
		httpx.FailKey(c, httpx.CodeNotFound, "error.clusterGone", nil, map[string]any{"id": cid})
		return true
	}
	return false
}

// requireCluster 取必填的 cluster_id 并校验存在性。返回 ok=false 时响应已写好。
func requireCluster(c *gin.Context, db *sql.DB) (int, bool) {
	raw := strings.TrimSpace(c.Query("cluster_id"))
	if raw == "" {
		httpx.Required(c, "cluster_id")
		return 0, false
	}
	cid, err := strconv.Atoi(raw)
	if err != nil || cid <= 0 {
		httpx.Invalid(c, "cluster_id", "number")
		return 0, false
	}
	if clusterGone(c, db, cid) {
		return 0, false
	}
	return cid, true
}

// envRank 生成按环境重要性排序的 SQL 片段（数字越小越靠前）。
//
// ⚠️ 这个排序不是"好看"，它决定了前端 11 个 K8s 页面**默认选中哪个集群**——
// 那些页面统一取集群列表的第一项。原先是 `ORDER BY environment, name`，纯字母序，
// 于是 environment='DEMO' 的演示集群永远排第一，打开任何 K8s 页面默认都是它，
// 全都显示「无数据，先去集群管理点同步」；即便没有 DEMO，DEV 也会排在 PROD 前面。
// 配合 `enabled DESC`（未启用的集群一律沉底），默认选中的才是"真正在跑的生产集群"。
func envRank(col string) string {
	return `CASE UPPER(COALESCE(` + col + `,''))
		WHEN 'PROD' THEN 1 WHEN 'PRD' THEN 1 WHEN 'PRODUCTION' THEN 1
		WHEN 'UAT' THEN 2
		WHEN 'STAGE' THEN 3 WHEN 'STAGING' THEN 3 WHEN 'TEST' THEN 3
		WHEN 'DEV' THEN 4 WHEN 'DEVELOP' THEN 4
		WHEN 'DEMO' THEN 9
		ELSE 5 END`
}

func (h *K8sClusterHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT k.id, k.name, COALESCE(k.prom_cluster_value,''), COALESCE(k.network_exposure,''), COALESCE(k.allow_secret_inventory,0), k.display_name, k.environment, k.provider, k.project_id,
		k.cloud_account_id, COALESCE(a.name,''), k.location, k.endpoint, COALESCE(k.nodepool_label,''), COALESCE(k.cost_mode,''),
		CASE WHEN k.kubeconfig_enc IS NULL OR k.kubeconfig_enc='' THEN 0 ELSE 1 END,
		k.enabled
		FROM k8s_clusters k LEFT JOIN cloud_accounts a ON a.id=k.cloud_account_id
		ORDER BY k.enabled DESC, ` + envRank("k.environment") + `, k.name`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []k8sClusterOut{}
	for rows.Next() {
		var r k8sClusterOut
		var hasKC int
		var allowSec int
		if err := rows.Scan(&r.ID, &r.Name, &r.PromClusterValue, &r.NetworkExposure, &allowSec, &r.DisplayName, &r.Environment, &r.Provider,
			&r.ProjectID, &r.CloudAccountID, &r.CloudAccount, &r.Location, &r.Endpoint, &r.NodepoolLabel, &r.CostMode, &hasKC, &r.Enabled); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		r.HasKubeconfig = hasKC == 1
		r.AllowSecretInv = allowSec == 1
		out = append(out, r)
	}
	c.JSON(http.StatusOK, out)
}

// k8sClusterPatch 部分更新用。全指针 —— nil = 没传（不动它）。
//
// 🔴 这个处理器此前用 `IF(?=”, 旧值, 新值)` 打过一次补丁，只护住了 GKE
//
//	连接四件套；`name` / `prom_cluster_value` / `allow_secret_inventory` 等
//	仍是无条件覆盖。其中两个的后果特别隐蔽：
//	  · prom_cluster_value 被清空 → 所有带集群隔离的查询**静默返回空**（生产踩过）
//	  · allow_secret_inventory 是**安全开关**，被静默翻动不该发生在任何方向
//
// ⚠️ 换成指针后 SQL 里的 `IF(?=”, …)` 就不需要了 —— "没传就别动"
//
//	本来就该由类型表达，而不是由 SQL 表达式模拟。
type k8sClusterPatch struct {
	Name             *string `json:"name"`
	PromClusterValue *string `json:"prom_cluster_value"`
	NetworkExposure  *string `json:"network_exposure"`
	AllowSecretInv   *bool   `json:"allow_secret_inventory"`
	DisplayName      *string `json:"display_name"`
	Environment      *string `json:"environment"`
	Provider         *string `json:"provider"`
	ProjectID        *string `json:"project_id"`
	CloudAccountID   *int    `json:"cloud_account_id"`
	Location         *string `json:"location"`
	Endpoint         *string `json:"endpoint"`
	CaData           *string `json:"ca_data"`
	NodepoolLabel    *string `json:"nodepool_label"`
	CostMode         *string `json:"cost_mode"`
	Kubeconfig       string  `json:"kubeconfig"` // 空=保留原值（本来就是二态）
	Enabled          *int    `json:"enabled"`
}

type k8sClusterIn struct {
	Name string `json:"name"`
	// 该集群在 Prometheus 指标里 cluster 标签的取值。与 name 不一致时必须填，
	// 否则所有带集群隔离的查询都会静默返回空（生产踩过：指标标签值 ≠ 系统里登记的集群名）。
	PromClusterValue string `json:"prom_cluster_value"`
	NetworkExposure  string `json:"network_exposure"`
	AllowSecretInv   bool   `json:"allow_secret_inventory"`
	DisplayName      string `json:"display_name"`
	Environment      string `json:"environment"`
	Provider         string `json:"provider"`
	ProjectID        string `json:"project_id"`
	CloudAccountID   int    `json:"cloud_account_id"` // 引用主机模块云账号(GKE 用；GCP SA key 不在此重复配)
	Location         string `json:"location"`
	Endpoint         string `json:"endpoint"`
	CaData           string `json:"ca_data"` // GKE 自动发现导入时的集群 CA(base64)
	NodepoolLabel    string `json:"nodepool_label"`
	CostMode         string `json:"cost_mode"`
	Kubeconfig       string `json:"kubeconfig"` // 空=保留原值(更新)/未配(创建)
	Enabled          *int   `json:"enabled"`
}

func (h *K8sClusterHandler) Create(c *gin.Context) {
	var in k8sClusterIn
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	if in.Name == "" {
		httpx.Required(c, "name")
		return
	}
	if in.Environment == "" {
		in.Environment = "DEV"
	}
	if in.Provider == "" {
		in.Provider = "gke"
	}
	kcEnc, err := h.encOrEmpty(in.Kubeconfig)
	if err != nil {
		httpx.FailKey(c, httpx.CodeInternal, "error.encryptFailed", err, nil)
		return
	}
	enabled := 1
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	res, err := h.DB.Exec(`INSERT INTO k8s_clusters
		(name, prom_cluster_value, network_exposure, allow_secret_inventory, display_name, environment, provider, project_id, cloud_account_id, location, endpoint, ca_data, nodepool_label, cost_mode, kubeconfig_enc, enabled)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.Name, in.PromClusterValue, in.NetworkExposure, b2int(in.AllowSecretInv), in.DisplayName, in.Environment, in.Provider, in.ProjectID, in.CloudAccountID, in.Location, in.Endpoint, in.CaData, in.NodepoolLabel, in.CostMode, kcEnc, enabled)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	AuditCreated(c, "k8s_clusters", id)
	SetAuditTarget(c, in.Name)
	logx.J("k8s", "cluster_create", map[string]any{"id": id, "name": in.Name, "env": in.Environment, "provider": in.Provider})
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (h *K8sClusterHandler) Update(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	var in k8sClusterPatch
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	if requireNonBlank(c, "name", in.Name) {
		return
	}
	// 凭据留空=保留原值；填了=加密覆盖
	if in.Kubeconfig != "" {
		enc, err := h.Cipher.Encrypt(in.Kubeconfig)
		if err != nil {
			httpx.FailKey(c, httpx.CodeInternal, "error.encryptFailed", err, nil)
			return
		}
		if _, err := sc.Exec(`UPDATE k8s_clusters SET kubeconfig_enc=? WHERE tenant_id = ? AND id=?`, enc, id); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	p := &patchSet{}
	p.Add("name", in.Name)
	p.Add("prom_cluster_value", in.PromClusterValue)
	p.Add("network_exposure", in.NetworkExposure)
	if in.AllowSecretInv != nil {
		p.Add("allow_secret_inventory", ptrInt(b2int(*in.AllowSecretInv)))
	}
	p.Add("display_name", in.DisplayName)
	p.Add("environment", in.Environment)
	p.Add("provider", in.Provider)
	p.Add("project_id", in.ProjectID)
	p.Add("cloud_account_id", in.CloudAccountID)
	p.Add("location", in.Location)
	p.Add("endpoint", in.Endpoint)
	p.Add("ca_data", in.CaData)
	p.Add("nodepool_label", in.NodepoolLabel)
	p.Add("cost_mode", in.CostMode)
	p.Add("enabled", in.Enabled)
	if p.Empty() {
		if in.Kubeconfig != "" {
			// 只换了凭据也算改动
			h.Pool.Invalidate(id)
			SetAuditTarget(c, strconv.Itoa(id))
			c.JSON(200, gin.H{"ok": true})
			return
		}
		httpx.Invalid(c, "body", "至少要传一个要改的字段")
		return
	}
	res, err := sc.Exec(`UPDATE k8s_clusters SET `+p.SQL()+` WHERE tenant_id = ? AND id=?`,
		append(p.Args(), id)...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.NotFound(c, "cluster")
		return
	}
	h.Pool.Invalidate(id) // 凭据/状态可能变，清连接缓存
	// 没传 name 时回落到 id：审计目标空着等于没记
	if n := derefStr(in.Name); n != "" {
		SetAuditTarget(c, n)
	} else {
		SetAuditTarget(c, strconv.Itoa(id))
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sClusterHandler) Delete(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	res, err := sc.Exec(`DELETE FROM k8s_clusters WHERE tenant_id = ? AND id=?`, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.NotFound(c, "cluster")
		return
	}
	// ⚠️ 必须连带清理**采集来的资源**，否则它们变成孤儿：
	// 集群名 JOIN 不到（显示空白）、心跳停在删除那一刻（永远"失联"），
	// 而重新纳管同一个集群会拿到新的 cluster_id —— 于是每个节点出现两行，
	// 一行 Ready 一行失联。生产上实测 32 个节点里 16 个是这种重影（OPSCMDB-022）。
	//
	// 只删**采集数据**（删了能重采）。以下四类是人工产物，删了找不回来，故保留：
	//   harbor_registries / obs_endpoints —— 接入配置，本来就独立于集群生命周期
	//   k8s_ns_project                    —— 人工填的命名空间归属
	//   gke_upgrade_baselines             —— 刻意存下的升级前基线，是历史证据
	// 删掉再重新纳管同一个集群时，这几样还在，正是我们想要的。
	for _, tbl := range []string{
		"k8s_nodes", "k8s_pods", "k8s_workloads", "k8s_namespaces", "k8s_services",
		"k8s_pvcs", "k8s_events", "k8s_ingresses", "k8s_gateways", "k8s_httproutes",
		"k8s_virtualservices", "k8s_hpas", "k8s_pdbs", "k8s_configmaps", "k8s_endpoints",
		"k8s_secrets", "k8s_pod_config_refs", "k8s_pod_security", "k8s_pod_volumes",
		"k8s_node_pools", "k8s_changes", "k8s_node_version_events", "k8s_node_alert_state",
		"k8s_sync_state", "gke_cluster_upgrade", "gke_node_pools", "gke_repair_history",
		"gke_upgrade_history",
	} {
		if _, err := sc.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ? AND cluster_id=?", id); err != nil {
			// 单表失败不该让整个删除半途而废——但要留痕，否则又是一批查不出来的孤儿
			logx.J("k8s", "cluster_delete_cleanup_failed", map[string]any{
				"cluster_id": id, "table": tbl, "err": err.Error(),
			})
		}
	}
	h.Pool.Invalidate(id)
	SetAuditTarget(c, c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Test 测连通：只 ServerVersion + 列 1 个节点，验证只读凭据可达 apiserver。
func (h *K8sClusterHandler) Test(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	h.Pool.Invalidate(id) // 用最新凭据
	cs, err := h.Pool.ClientFor(id)
	if err != nil {
		logx.J("k8s", "cluster_test_fail", map[string]any{"id": id, "err": err.Error()})
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ver, err := cs.Discovery().ServerVersion()
	if err != nil {
		logx.J("k8s", "cluster_test_fail", map[string]any{"id": id, "err": err.Error()})
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	nodes, err := cs.CoreV1().Nodes().List(ctx, metav1.ListOptions{Limit: 500})
	if err != nil {
		logx.J("k8s", "cluster_test_fail", map[string]any{"id": id, "version": ver.GitVersion, "err": err.Error()})
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(), "version": ver.GitVersion})
		return
	}
	missing := checkReadPerms(ctx, cs)
	logx.J("k8s", "cluster_test_ok", map[string]any{"id": id, "version": ver.GitVersion,
		"nodes": len(nodes.Items), "missing_perms": missing})
	out := gin.H{"ok": true, "version": ver.GitVersion, "nodes": len(nodes.Items)}
	if len(missing) > 0 {
		out["missing_perms"] = missing
		out["perm_warn"] = "连接正常，但以下只读权限缺失，对应功能会在使用时报 403：" + strings.Join(missing, "；")
	}
	c.JSON(http.StatusOK, out)
}

// readPermChecks CMDB 只读功能实际依赖的权限。
//
// 为什么要主动查而不是等报错：权限缺一项不影响连接测试通过（列节点照样成功），
// 要等到有人真去排障时才撞上 403——生产上就是这样，Pod 日志一直读不了，
// 直到查 Kafka 时才发现。测试连接时顺手核一遍，缺什么当场说清楚。
var readPermChecks = []struct{ group, resource, subresource, verb, why string }{
	{"", "pods", "", "list", "Pod 清单"},
	{"", "pods", "log", "get", "Pod 日志，诊断/排障用；缺了只能退回 Loki 查历史"},
	{"", "events", "", "list", "事件（诊断根因）"},
	{"", "nodes", "", "list", "节点"},
	{"", "persistentvolumeclaims", "", "list", "存储卷"},
	{"apps", "deployments", "", "list", "工作负载"},
}

// checkReadPerms 用 SelfSubjectAccessReview 自查当前凭据缺哪些只读权限，返回人话描述。
// SSAR 本身不可用时返回空——不能因为自检做不了就报告「权限有问题」。
func checkReadPerms(ctx context.Context, cs *kubernetes.Clientset) []string {
	var missing []string
	for _, chk := range readPermChecks {
		r := &authzv1.SelfSubjectAccessReview{
			Spec: authzv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authzv1.ResourceAttributes{
					Group: chk.group, Resource: chk.resource, Subresource: chk.subresource, Verb: chk.verb,
				},
			},
		}
		res, err := cs.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, r, metav1.CreateOptions{})
		if err != nil {
			return nil // 集群不让做 SSAR，放弃自检而不是误报
		}
		if !res.Status.Allowed {
			res := chk.resource
			if chk.subresource != "" {
				res += "/" + chk.subresource
			}
			missing = append(missing, fmt.Sprintf("%s %s —— %s", chk.verb, res, chk.why))
		}
	}
	return missing
}

func (h *K8sClusterHandler) encOrEmpty(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	return h.Cipher.Encrypt(s)
}

// b2int bool → 0/1，写 TINYINT 列用。
func b2int(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ptrInt 取地址的小工具（patchSet.Add 只收指针）。
func ptrInt(v int) *int { return &v }

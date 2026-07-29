package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	authzv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/k8ssource"
	"opsplatform-cmdb-backend/logx"
)

// K8sClusterHandler 管理多集群纳管（只读）。凭据 AES 加密存，测连通只 list nodes。
type K8sClusterHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
	Pool   *k8ssource.Pool
}

func NewK8sClusterHandler(db *sql.DB, cipher *crypto.Cipher, pool *k8ssource.Pool) *K8sClusterHandler {
	return &K8sClusterHandler{DB: db, Cipher: cipher, Pool: pool}
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
		c.JSON(400, gin.H{"error": "cloud_account_id/project_id 必填"})
		return
	}
	var enc sql.NullString
	e := h.DB.QueryRow(`SELECT cred_enc FROM cloud_account_projects WHERE account_id=? AND project_id=?`, in.CloudAccountID, in.ProjectID).Scan(&enc)
	if e != nil || !enc.Valid || enc.String == "" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "该云账号项目未配 SA key（去 系统管理→云账号 配）"})
		return
	}
	saJSON, e := h.Cipher.Decrypt(enc.String)
	if e != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "解密 SA key 失败"})
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

func (h *K8sClusterHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT k.id, k.name, COALESCE(k.prom_cluster_value,''), COALESCE(k.network_exposure,''), COALESCE(k.allow_secret_inventory,0), k.display_name, k.environment, k.provider, k.project_id,
		k.cloud_account_id, COALESCE(a.name,''), k.location, k.endpoint, COALESCE(k.nodepool_label,''), COALESCE(k.cost_mode,''),
		CASE WHEN k.kubeconfig_enc IS NULL OR k.kubeconfig_enc='' THEN 0 ELSE 1 END,
		k.enabled
		FROM k8s_clusters k LEFT JOIN cloud_accounts a ON a.id=k.cloud_account_id
		ORDER BY k.environment, k.name`)
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

type k8sClusterIn struct {
	Name string `json:"name"`
	// 该集群在 Prometheus 指标里 cluster 标签的取值。与 name 不一致时必须填，
	// 否则所有带集群隔离的查询都会静默返回空（g32 生产踩过：prod-k8s-cluster-01 vs g32-prod-cluster）。
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
		c.JSON(400, gin.H{"error": "请求体解析失败"})
		return
	}
	if in.Name == "" {
		c.JSON(400, gin.H{"error": "集群名必填"})
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
		c.JSON(500, gin.H{"error": "加密 kubeconfig 失败"})
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
	WriteAudit(h.DB, c, "create_k8s_cluster", in.Name)
	logx.J("k8s", "cluster_create", map[string]any{"id": id, "name": in.Name, "env": in.Environment, "provider": in.Provider})
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (h *K8sClusterHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in k8sClusterIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "请求体解析失败"})
		return
	}
	enabled := 1
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	// 凭据留空=保留原值；填了=加密覆盖
	if in.Kubeconfig != "" {
		enc, err := h.Cipher.Encrypt(in.Kubeconfig)
		if err != nil {
			c.JSON(500, gin.H{"error": "加密 kubeconfig 失败"})
			return
		}
		if _, err := h.DB.Exec(`UPDATE k8s_clusters SET kubeconfig_enc=? WHERE id=?`, enc, id); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	_, err := h.DB.Exec(`UPDATE k8s_clusters SET name=?, prom_cluster_value=?, network_exposure=?, allow_secret_inventory=?,
		display_name=?, environment=?, provider=?,
		project_id=?, cloud_account_id=?, location=?, endpoint=?, nodepool_label=?, cost_mode=?, enabled=? WHERE id=?`,
		in.Name, in.PromClusterValue, in.NetworkExposure, b2int(in.AllowSecretInv), in.DisplayName, in.Environment, in.Provider,
		in.ProjectID, in.CloudAccountID, in.Location, in.Endpoint, in.NodepoolLabel, in.CostMode, enabled, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	h.Pool.Invalidate(id) // 凭据/状态可能变，清连接缓存
	WriteAudit(h.DB, c, "update_k8s_cluster", in.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *K8sClusterHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, err := h.DB.Exec(`DELETE FROM k8s_clusters WHERE id=?`, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	_, _ = h.DB.Exec(`DELETE FROM k8s_sync_state WHERE cluster_id=?`, id)
	h.Pool.Invalidate(id)
	WriteAudit(h.DB, c, "delete_k8s_cluster", c.Param("id"))
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
// 要等到有人真去排障时才撞上 403——g32 生产就是这样，Pod 日志一直读不了，
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

package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/logx"
)

// ObsHandler 管理外部数据源接入（Prometheus/Loki/KubeSphere），只读查询用。
type ObsHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewObsHandler(db *sql.DB, cipher *crypto.Cipher) *ObsHandler {
	return &ObsHandler{DB: db, Cipher: cipher}
}

func (h *ObsHandler) Register(r *gin.RouterGroup) {
	r.GET("/obs-endpoints", h.List)
	r.POST("/obs-endpoints", h.Create)
	r.PUT("/obs-endpoints/:id", h.Update)
	r.DELETE("/obs-endpoints/:id", h.Delete)
	r.POST("/obs-endpoints/:id/test", h.Test)
}

type obsOut struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	URL          string `json:"url"`
	Env          string `json:"env"`
	ClusterID    int    `json:"cluster_id"`
	ClusterLabel string `json:"cluster_label"` // 多集群共享源的隔离标签名，空=单集群源
	HasToken     bool   `json:"has_token"`
	Enabled      int    `json:"enabled"`
}

func (h *ObsHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id,name,type,url,env,cluster_id,cluster_label,
		CASE WHEN token_enc IS NULL OR token_enc='' THEN 0 ELSE 1 END, enabled FROM obs_endpoints ORDER BY type,env,name`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []obsOut{}
	for rows.Next() {
		var o obsOut
		var hasTok int
		if rows.Scan(&o.ID, &o.Name, &o.Type, &o.URL, &o.Env, &o.ClusterID, &o.ClusterLabel, &hasTok, &o.Enabled) != nil {
			continue
		}
		o.HasToken = hasTok == 1
		out = append(out, o)
	}
	c.JSON(http.StatusOK, out)
}

type obsIn struct {
	Name         string `json:"name"`
	Type         string `json:"type"`
	URL          string `json:"url"`
	Env          string `json:"env"`
	ClusterID    int    `json:"cluster_id"`
	ClusterLabel string `json:"cluster_label"` // 通用源填 cluster；空=该源只有一个集群的数据
	Token        string `json:"token"`         // 空=保留(更新)/不配(创建)
	Enabled      *int   `json:"enabled"`
}

// obsTypes 认得的数据源类型。
//
//	以前不校验，什么字符串都收。打错一个字母（n9E / nightingle）就会静默建出
//	一条永远不工作的记录：查询时按 type 找不到源，界面上却显示"已启用"。
//	这类错误没有任何报错，只有"功能怎么没数据"。
var obsTypes = map[string]string{
	"prometheus": "Prometheus / VictoriaMetrics",
	"loki":       "Loki",
	"kubesphere": "KubeSphere",
	"n9e":        "夜莺 Nightingale",
}

func normalizeObsType(t string) (string, bool) {
	k := strings.ToLower(strings.TrimSpace(t))
	// 几个常见别名归一，免得同一种源存成两个 type 值互相看不见
	switch k {
	case "vm", "victoriametrics", "prometheus/vm":
		k = "prometheus"
	case "nightingale":
		k = "n9e"
	}
	_, ok := obsTypes[k]
	return k, ok
}

func (h *ObsHandler) Create(c *gin.Context) {
	var in obsIn
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" || in.Type == "" || in.URL == "" {
		c.JSON(400, gin.H{"error": "name/type/url 必填"})
		return
	}
	typ, ok := normalizeObsType(in.Type)
	if !ok {
		names := make([]string, 0, len(obsTypes))
		for k := range obsTypes {
			names = append(names, k)
		}
		sort.Strings(names)
		c.JSON(400, gin.H{"error": "不认识的数据源类型 " + in.Type + "，支持：" + strings.Join(names, " / ")})
		return
	}
	in.Type = typ
	tokEnc := ""
	if in.Token != "" {
		e, err := h.Cipher.Encrypt(in.Token)
		if err != nil {
			c.JSON(500, gin.H{"error": "加密失败"})
			return
		}
		tokEnc = e
	}
	enabled := 1
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	res, err := h.DB.Exec(`INSERT INTO obs_endpoints (name,type,url,env,cluster_id,cluster_label,token_enc,enabled) VALUES (?,?,?,?,?,?,?,?)`,
		in.Name, in.Type, in.URL, in.Env, in.ClusterID, strings.TrimSpace(in.ClusterLabel), tokEnc, enabled)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	AuditCreated(c, "obs_endpoints", id)
	SetAuditTarget(c, in.Name)
	c.JSON(http.StatusOK, gin.H{"id": id})
}

func (h *ObsHandler) Update(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	var in obsIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": "请求体解析失败"})
		return
	}
	enabled := 1
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if in.Token != "" {
		e, err := h.Cipher.Encrypt(in.Token)
		if err != nil {
			c.JSON(500, gin.H{"error": "加密失败"})
			return
		}
		if _, err := h.DB.Exec(`UPDATE obs_endpoints SET token_enc=? WHERE id=?`, e, id); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	if _, err := h.DB.Exec(`UPDATE obs_endpoints SET name=?,type=?,url=?,env=?,cluster_id=?,cluster_label=?,enabled=? WHERE id=?`,
		in.Name, in.Type, in.URL, in.Env, in.ClusterID, strings.TrimSpace(in.ClusterLabel), enabled, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	SetAuditTarget(c, in.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ObsHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, err := h.DB.Exec(`DELETE FROM obs_endpoints WHERE id=?`, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	SetAuditTarget(c, c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// Test 测连通：按类型依次探几个"确定存在"的路径，任一返回 2xx 即算通。
//
// 为什么要多个候选：这些服务的根路径基本都不是 200（KubeSphere 的 ks-apiserver 根路径直接 404），
// 而不同版本/不同部署方式暴露的健康路径又不一样——写死单条路径，换个版本就又是一次误报"连不通"。
// 全部探完都不通才判失败，并把每条路径的实际状态码都带回去，让人一眼看出是地址错了、
// 要认证、还是服务真的没起来。
func (h *ObsHandler) Test(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	base, token, err := resolveEndpointByID(h.DB, h.Cipher, id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	var typ string
	_ = h.DB.QueryRow(`SELECT type FROM obs_endpoints WHERE id=?`, id).Scan(&typ)

	c.JSON(http.StatusOK, probeEndpoint(base, token, probePaths(typ), typ))
}

// probeEndpoint 依次探候选路径，任一 2xx 即算通；全不通则按失败形态给出可行动的原因。
func probeEndpoint(base, token string, paths []string, typ string) gin.H {
	root := strings.TrimRight(base, "/")
	tried := []gin.H{}
	var lastErr string
	for _, p := range paths {
		code, body, e := obsGetAs(root+p, token, typ, 10*time.Second)
		if e != nil {
			lastErr = e.Error()
			tried = append(tried, gin.H{"path": orRoot(p), "error": truncate(e.Error(), 120)})
			continue
		}
		if code >= 200 && code < 300 {
			return gin.H{"ok": true, "status": code, "path": orRoot(p),
				"body": truncate(body, 200), "tried": tried}
		}
		tried = append(tried, gin.H{"path": orRoot(p), "status": code})
	}

	out := gin.H{"ok": false, "tried": tried}
	switch {
	case hasStatus(tried, 401) || hasStatus(tried, 403):
		out["error"] = "地址通了但没有权限（401/403）：token 没配或已失效"
	case lastErr != "":
		out["error"] = "连不上：" + lastErr
	default:
		out["error"] = "地址通了但探测路径都不是 2xx —— 多半是地址填错了（少了/多了路径前缀），或这不是该类型的服务"
	}
	return out
}

func orRoot(p string) string {
	if p == "" {
		return "/"
	}
	return p
}

func hasStatus(tried []gin.H, code int) bool {
	for _, t := range tried {
		if s, ok := t["status"].(int); ok && s == code {
			return true
		}
	}
	return false
}

// probePaths 按数据源类型返回若干"确定存在"的探活路径，按可信度排序，依次探到 2xx 为止。
// 都是只读且无副作用的接口。
func probePaths(typ string) []string {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "loki":
		// 根路径 404；labels 是最轻的只读接口，ready 在部分部署里没暴露。
		return []string{"/loki/api/v1/labels", "/ready", "/metrics"}
	case "prometheus", "vm", "victoriametrics", "prometheus/vm":
		return []string{"/api/v1/query?query=up", "/-/healthy", "/health"}
	case "kubesphere":
		// ks-apiserver 根路径和 /kapis 都是 404。实测这几条返回 200：
		// /kapis/version 还能顺带确认"这确实是个 KubeSphere"，所以排在健康检查前面。
		return []string{"/kapis/version", "/healthz", "/version", "/apis"}
	case "n9e", "nightingale":
		// 夜莺没有无鉴权的健康检查路径，根路径是前端页面（200 但说明不了什么）。
		// 直接探真正要用的那个接口，limit=1 开销最小——
		// 它同时验证了"地址对"和"token 有效"，这才是用户点「测试」想知道的。
		return []string{"/api/n9e/alert-cur-events/list?limit=1&p=1"}
	default:
		return []string{"", "/healthz"}
	}
}

// ---- 供 usage/loki/kubesphere 复用 ----

// resolveEndpoint 按 type + env + cluster 选最匹配的启用端点（集群>环境>通用）。
func resolveEndpoint(db *sql.DB, cipher *crypto.Cipher, typ, env string, clusterID int) (url, token string, err error) {
	url, token, _, err = resolveEndpointFull(db, cipher, typ, env, clusterID)
	return url, token, err
}

// resolveEndpointFull 同 resolveEndpoint，额外返回该端点的 cluster_label（多集群共享源的隔离标签名）。
func resolveEndpointFull(db *sql.DB, cipher *crypto.Cipher, typ, env string, clusterID int) (url, token, clusterLabel string, err error) {
	rows, e := db.Query(`SELECT url, COALESCE(token_enc,''), env, cluster_id, COALESCE(cluster_label,'') FROM obs_endpoints WHERE type=? AND enabled=1`, typ)
	if e != nil {
		return "", "", "", e
	}
	defer rows.Close()
	bestScore := -1
	var bestURL, bestEnc, bestLabel string
	for rows.Next() {
		var u, enc, e2, lbl string
		var cid int
		if rows.Scan(&u, &enc, &e2, &cid, &lbl) != nil {
			continue
		}
		score := 0
		if cid != 0 {
			if cid == clusterID {
				score += 2
			} else {
				continue // 指定了别的集群，不匹配
			}
		}
		if e2 != "" {
			if e2 == env {
				score += 1
			} else {
				continue
			}
		}
		if score > bestScore {
			bestScore, bestURL, bestEnc, bestLabel = score, u, enc, lbl
		}
	}
	if bestURL == "" {
		return "", "", "", errNoEndpoint(typ)
	}
	if bestEnc != "" {
		token, _ = cipher.Decrypt(bestEnc)
	}
	return bestURL, token, bestLabel, nil
}

// clusterSelector 生成把查询限定到本集群的 PromQL 标签选择器（如 cluster="uat-k8s-cluster-01"）。
//
// 只有多集群共享的数据源才需要——它同时采多个集群，不加这个条件会把别的集群的数据一起捞回来，
// 而 UAT 和 PROD 存在大量同名 namespace，sum by(namespace,pod) 会把同名 Pod 直接加在一起。
// 返回空串表示不需要注入（单集群源），此时所有查询与改造前完全一致。
func clusterSelector(db *sql.DB, clusterLabel string, clusterID int) string {
	label, value := clusterSelectorParts(db, clusterLabel, clusterID)
	if label == "" {
		return ""
	}
	return fmt.Sprintf("%s=%q", label, value)
}

// clusterSelectorParts 同 clusterSelector，但把标签名和取值分开返回——
// 调用方需要它们去做「配的值在数据源里到底存不存在」的自检（见 verifyClusterValue）。
//
// 取值优先用 k8s_clusters.prom_cluster_value，为空才回落到 name。
// 早期版本只用 name，隐含假设「CMDB 集群名 == 指标里的 cluster 标签值」；UAT 恰好成立，
// g32 生产不成立（g32-prod-cluster vs prod-k8s-cluster-01），于是所有隔离查询静默返回空。
// 台账命名和对方集群的采集配置本就是两回事，不该绑死。
func clusterSelectorParts(db *sql.DB, clusterLabel string, clusterID int) (label, value string) {
	if clusterLabel == "" || clusterID <= 0 {
		return "", ""
	}
	var name, promValue string
	if db.QueryRow(`SELECT name, COALESCE(prom_cluster_value,'') FROM k8s_clusters WHERE id=?`,
		clusterID).Scan(&name, &promValue) != nil || name == "" {
		logx.J("obs", "cluster_selector_miss", map[string]any{
			"cluster_id": clusterID, "cluster_label": clusterLabel,
			"warn": "数据源配了 cluster_label 但集群没有 name，本次查询不做集群隔离，结果可能混入其它集群",
		})
		return "", ""
	}
	if promValue != "" {
		return clusterLabel, promValue
	}
	return clusterLabel, name
}

// verifyClusterValue 检查隔离用的标签值在数据源里是否真的存在。
//
// 只在「查询做了集群隔离、但结果为空」时调用——这正是最危险的一种空：
// 标签值配错时 Prometheus 不会报错，只会返回 0 条，读起来和「组件正常、无异常数据」一模一样。
// g32 的 Kafka 就是这么被查成「无数据」的。
//
// 返回 nil 表示值存在（空结果另有原因，比如 exporter 没部署）；否则返回可直接给人看的诊断。
func verifyClusterValue(base, token, label, value string) map[string]any {
	code, body, err := obsGet(base+"/api/v1/label/"+url.PathEscape(label)+"/values", token, 15*time.Second)
	if err != nil || code != 200 {
		return nil // 自检本身失败就不猜了，别拿一个不确定的结论去覆盖真实的空结果
	}
	var r struct {
		Data []string `json:"data"`
	}
	if json.Unmarshal([]byte(body), &r) != nil || len(r.Data) == 0 {
		return nil
	}
	for _, v := range r.Data {
		if v == value {
			return nil
		}
	}
	sort.Strings(r.Data)
	logx.J("obs", "cluster_value_mismatch", map[string]any{
		"label": label, "configured": value, "available": r.Data,
		"warn": "集群隔离标签值在数据源里不存在，所有隔离查询都会返回空结果",
	})
	return map[string]any{
		"label": label, "configured_value": value, "available_values": r.Data,
		"error": fmt.Sprintf("集群隔离标签值 %s=%q 在该数据源里不存在——所有带集群条件的查询都会返回空，"+
			"这个空不代表组件正常。数据源里可选的值：%s。"+
			"请在「接入管理 → 集群」里把该集群的「指标集群标签值」改成正确的那个。",
			label, value, strings.Join(r.Data, ", ")),
	}
}

// promLabels 把集群选择器与查询级条件拼成 PromQL 的 {...} 片段；全空时返回空串。
func promLabels(selector string, conds ...string) string {
	parts := make([]string, 0, len(conds)+1)
	if selector != "" {
		parts = append(parts, selector)
	}
	for _, c := range conds {
		if c != "" {
			parts = append(parts, c)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func resolveEndpointByID(db *sql.DB, cipher *crypto.Cipher, id int) (url, token string, err error) {
	var enc sql.NullString
	e := db.QueryRow(`SELECT url, token_enc FROM obs_endpoints WHERE id=?`, id).Scan(&url, &enc)
	if e == sql.ErrNoRows {
		return "", "", errNoEndpoint("id")
	}
	if e != nil {
		return "", "", e
	}
	if enc.Valid && enc.String != "" {
		token, _ = cipher.Decrypt(enc.String)
	}
	return url, token, nil
}

type obsErr string

func (e obsErr) Error() string { return string(e) }
func errNoEndpoint(t string) error {
	return obsErr("未配置可用的 " + t + " 数据源（去 系统管理→数据源接入 配置）")
}

// obsGet 只读 GET（带可选 Bearer token）。
func obsGet(url, token string, timeout time.Duration) (int, string, error) {
	return obsGetAs(url, token, "", timeout)
}

// obsGetAs 按数据源类型选鉴权头。
//
//	⚠️ 夜莺**只认 X-User-Token**，不认 Authorization: Bearer。
//	统一发 Bearer 的后果是：token 明明是好的，「测试连通」永远报
//	"401 token 没配或已失效"——而告警功能本身却是正常工作的。
//	这种"功能好的、测试说坏的"最误导人，会让人去改一个根本没问题的配置。
func obsGetAs(url, token, typ string, timeout time.Duration) (int, string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, "", err
	}
	if token != "" {
		switch strings.ToLower(strings.TrimSpace(typ)) {
		case "n9e", "nightingale":
			req.Header.Set("X-User-Token", token)
		default:
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	cli := &http.Client{Timeout: timeout}
	resp, err := cli.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(b), nil
}

// ── 容器级指标去重（PROD-014）────────────────────────────────────────
//
//	g32 生产上同一个容器的 container_* 指标存在 **3 条独立 series**
//	（kube-prometheus-stack 和 victoria-metrics 两套栈重复采 kubelet，
//	外加 /metrics/resource 与 /metrics/cadvisor 两条路径）。标签不同、值相同，
//	Prometheus 不会自动去重，于是任何 `sum(...) by (pod)` 都把同一份用量加了三遍。
//
//	后果不是"数字不好看"，是**基于它的结论全错**：resource_waste 报某服务
//	内存用了 211%（超 request 一倍多），HPA 的 status.currentMetrics 实测只有 70%，
//	比值精确 3.00。照着这种数去做扩缩容决策，方向都是反的。
//
//	修法用 avg 而不是锁定某个 metrics_path/service 标签：
//	`sum by (pod) (avg by (pod,container) (metric))` —— 先把同一容器的多份取平均
//	（它们值本来就相同，avg 即原值），再按 pod 求和。这样不依赖任何一套采集器的
//	标签约定，将来换监控栈、或者重复采集被治好，这个表达式都照样正确。

// dedupContainerSum 生成去重的容器级聚合表达式。
//
//	by:    最终聚合维度，如 "namespace,pod"；空串表示全局 sum
//	inner: 被聚合的指标表达式（含标签选择器），如 container_memory_working_set_bytes{...}
func dedupContainerSum(by, inner string) string {
	if by == "" {
		return fmt.Sprintf("sum(avg by (namespace,pod,container) (%s))", inner)
	}
	return fmt.Sprintf("sum by (%s) (avg by (%s,container) (%s))", by, by, inner)
}

// warnIfDuplicateSeries 采一次样，若同一 (pod,container) 有多条 series 就打 WARN。
//
//	去重之后数字是对的，但重复采集本身还在浪费监控存储、也说明集群侧配置有问题
//	（PROD-014 第 4 步「根治」）。这条日志是让它别再无声无息。
func warnIfDuplicateSeries(base, token, selector string) {
	res, err := promInstant(base, token,
		`count by (namespace,pod,container) (container_memory_working_set_bytes`+selector+`) > 1`)
	if err != nil || len(res) == 0 {
		return
	}
	dup := res[0]
	logx.Line("obs", fmt.Sprintf(
		"WARN 容器级指标存在重复采集：%s/%s 容器 %s 有 %.0f 条 series（共 %d 个容器中招）。"+
			"查询已做去重所以数值正确，但监控侧仍在重复存储，参见 PROD-014",
		dup.Metric["namespace"], dup.Metric["pod"], dup.Metric["container"], dup.Value, len(res)))
}

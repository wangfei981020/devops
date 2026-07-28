package handlers

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
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

func (h *ObsHandler) Create(c *gin.Context) {
	var in obsIn
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" || in.Type == "" || in.URL == "" {
		c.JSON(400, gin.H{"error": "name/type/url 必填"})
		return
	}
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
	WriteAudit(h.DB, c, "create_obs_endpoint", in.Name)
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
	WriteAudit(h.DB, c, "update_obs_endpoint", in.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *ObsHandler) Delete(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	if _, err := h.DB.Exec(`DELETE FROM obs_endpoints WHERE id=?`, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "delete_obs_endpoint", c.Param("id"))
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

	c.JSON(http.StatusOK, probeEndpoint(base, token, probePaths(typ)))
}

// probeEndpoint 依次探候选路径，任一 2xx 即算通；全不通则按失败形态给出可行动的原因。
func probeEndpoint(base, token string, paths []string) gin.H {
	root := strings.TrimRight(base, "/")
	tried := []gin.H{}
	var lastErr string
	for _, p := range paths {
		code, body, e := obsGet(root+p, token, 10*time.Second)
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
//
// 标签值取 k8s_clusters.name，与通用源里 cluster 标签的取值一致（已逐节点比对验证）。
func clusterSelector(db *sql.DB, clusterLabel string, clusterID int) string {
	if clusterLabel == "" || clusterID <= 0 {
		return ""
	}
	var name string
	if db.QueryRow(`SELECT name FROM k8s_clusters WHERE id=?`, clusterID).Scan(&name) != nil || name == "" {
		logx.J("obs", "cluster_selector_miss", map[string]any{
			"cluster_id": clusterID, "cluster_label": clusterLabel,
			"warn": "数据源配了 cluster_label 但集群没有 name，本次查询不做集群隔离，结果可能混入其它集群",
		})
		return ""
	}
	return fmt.Sprintf("%s=%q", clusterLabel, name)
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
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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

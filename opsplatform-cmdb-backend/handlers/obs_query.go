package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/crypto"
)

// rawJSON 把外部返回的 JSON 文本解析成对象嵌入响应（解析失败则原样返回字符串）。
func rawJSON(s string) any {
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	return s
}

// ObsQueryHandler 查外部数据源：资源使用率(Prometheus/VM)、Loki 日志、KubeSphere 流水线。
// 本地不存历史，实时打这些源；地址来自 obs_endpoints（按 env/cluster 解析）。
type ObsQueryHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewObsQueryHandler(db *sql.DB, cipher *crypto.Cipher) *ObsQueryHandler {
	return &ObsQueryHandler{DB: db, Cipher: cipher}
}

func (h *ObsQueryHandler) Register(r *gin.RouterGroup) {
	r.GET("/obs/usage", h.Usage)          // 资源使用率(Prometheus): cluster_id,env?,target,namespace?,name,metric,minutes,query?
	r.GET("/obs/loki", h.Loki)            // Loki 日志: env?/cluster_id?,query(LogQL),minutes
	r.GET("/obs/kubesphere", h.KubeSphere) // KubeSphere 透传: env?/cluster_id?,path(kapis 路径)
}

// clusterEnv 由 cluster_id 取环境（用于按环境解析数据源）。
func (h *ObsQueryHandler) clusterEnv(cid int) string {
	var env string
	_ = h.DB.QueryRow(`SELECT environment FROM k8s_clusters WHERE id=?`, cid).Scan(&env)
	return env
}

// Usage 资源使用率：构造 PromQL → query_range → 返回 Prometheus 原始结果（AI/前端自解析）。
func (h *ObsQueryHandler) Usage(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	env := c.Query("env")
	if env == "" && cid > 0 {
		env = h.clusterEnv(cid)
	}
	base, token, err := resolveEndpoint(h.DB, h.Cipher, "prometheus", env, cid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	promql := c.Query("query")
	if promql == "" {
		promql = buildPromQL(c.Query("target"), c.Query("namespace"), c.Query("name"), c.Query("metric"))
	}
	if promql == "" {
		c.JSON(400, gin.H{"error": "缺 query 或 target/name/metric"})
		return
	}
	minutes := int64(60)
	if m, e := strconv.ParseInt(c.Query("minutes"), 10, 64); e == nil && m > 0 && m <= 43200 {
		minutes = m
	}
	end := time.Now()
	start := end.Add(-time.Duration(minutes) * time.Minute)
	step := minutes / 60
	if step < 1 {
		step = 1
	}
	u := fmt.Sprintf("%s/api/v1/query_range?query=%s&start=%d&end=%d&step=%dm",
		base, url.QueryEscape(promql), start.Unix(), end.Unix(), step)
	code, body, err := obsGet(u, token, 20*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": code == 200, "status": code, "query": promql, "data": rawJSON(body)})
}

func buildPromQL(target, ns, name, metric string) string {
	if name == "" {
		return ""
	}
	switch target {
	case "pod":
		if metric == "mem" {
			return fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s",pod="%s",container!=""})`, ns, name)
		}
		return fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod="%s",container!=""}[5m]))`, ns, name)
	case "workload":
		if metric == "mem" {
			return fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s",pod=~"%s-.*",container!=""})`, ns, name)
		}
		return fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s-.*",container!=""}[5m]))`, ns, name)
	case "node":
		if metric == "mem" {
			return fmt.Sprintf(`sum(container_memory_working_set_bytes{node="%s",container!=""})`, name)
		}
		return fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{node="%s",container!=""}[5m]))`, name)
	}
	return ""
}

// Loki 日志检索（LogQL），透传 range 查询结果。
func (h *ObsQueryHandler) Loki(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	env := c.Query("env")
	if env == "" && cid > 0 {
		env = h.clusterEnv(cid)
	}
	base, token, err := resolveEndpoint(h.DB, h.Cipher, "loki", env, cid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	q := c.Query("query")
	if q == "" {
		c.JSON(400, gin.H{"error": "query(LogQL) 必填"})
		return
	}
	minutes := int64(60)
	if m, e := strconv.ParseInt(c.Query("minutes"), 10, 64); e == nil && m > 0 {
		minutes = m
	}
	end := time.Now()
	start := end.Add(-time.Duration(minutes) * time.Minute)
	u := fmt.Sprintf("%s/loki/api/v1/query_range?query=%s&start=%d&end=%d&limit=500",
		base, url.QueryEscape(q), start.UnixNano(), end.UnixNano())
	code, body, err := obsGet(u, token, 20*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": code == 200, "status": code, "data": rawJSON(body)})
}

// KubeSphere 透传：拉指定 kapis 路径（如流水线运行状态/日志），交给 AI 诊断。
func (h *ObsQueryHandler) KubeSphere(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	env := c.Query("env")
	if env == "" && cid > 0 {
		env = h.clusterEnv(cid)
	}
	base, token, err := resolveEndpoint(h.DB, h.Cipher, "kubesphere", env, cid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	path := c.Query("path")
	if path == "" {
		c.JSON(400, gin.H{"error": "path 必填(如 /kapis/devops.kubesphere.io/v1alpha3/...)"})
		return
	}
	code, body, err := obsGet(base+path, token, 20*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": code >= 200 && code < 300, "status": code, "data": rawJSON(body)})
}

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
	r.GET("/k8s/pod-usage", h.PodUsage)   // 全 Pod 实时用量(cpu_m/mem_mi) map，供 Pod 页列展示
	r.GET("/k8s/node-usage", h.NodeUsage) // 全节点实时用量(cpu%/mem%) map，供节点页列展示
	r.GET("/k8s/pvc-usage", h.PVCUsage)   // 全 PVC 使用率(used/cap/pct) map，供存储页列展示
}

// PVCUsage 返回 {"ns/pvc": {used_gi, cap_gi, pct}}（kubelet volume 指标）。
func (h *ObsQueryHandler) PVCUsage(c *gin.Context) {
	base, token, ok := h.prom(c)
	if !ok {
		return
	}
	usage := map[string]map[string]float64{}
	get := func(k string) map[string]float64 {
		if usage[k] == nil {
			usage[k] = map[string]float64{}
		}
		return usage[k]
	}
	if used, err := promInstant(base, token, `kubelet_volume_stats_used_bytes`); err == nil {
		for _, s := range used {
			get(s.Metric["namespace"]+"/"+s.Metric["persistentvolumeclaim"])["used_gi"] = s.Value / 1073741824
		}
	}
	if cap, err := promInstant(base, token, `kubelet_volume_stats_capacity_bytes`); err == nil {
		for _, s := range cap {
			m := get(s.Metric["namespace"] + "/" + s.Metric["persistentvolumeclaim"])
			m["cap_gi"] = s.Value / 1073741824
			if m["cap_gi"] > 0 {
				m["pct"] = m["used_gi"] / m["cap_gi"] * 100
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "usage": usage})
}

type promSample struct {
	Metric map[string]string
	Value  float64
}

// promInstant 打一次 /api/v1/query，返回样本列表。
func promInstant(base, token, query string) ([]promSample, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", base, url.QueryEscape(query))
	code, body, err := obsGet(u, token, 15*time.Second)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("prometheus HTTP %d", code)
	}
	var r struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return nil, err
	}
	out := make([]promSample, 0, len(r.Data.Result))
	for _, res := range r.Data.Result {
		val := 0.0
		if len(res.Value) == 2 {
			if s, ok := res.Value[1].(string); ok {
				val, _ = strconv.ParseFloat(s, 64)
			}
		}
		out = append(out, promSample{Metric: res.Metric, Value: val})
	}
	return out, nil
}

func (h *ObsQueryHandler) prom(c *gin.Context) (string, string, bool) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	env := c.Query("env")
	if env == "" && cid > 0 {
		env = h.clusterEnv(cid)
	}
	base, token, err := resolveEndpoint(h.DB, h.Cipher, "prometheus", env, cid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return "", "", false
	}
	return base, token, true
}

// PodUsage 返回 {"ns/pod": {cpu_m, mem_mi}}，一次拉全集群 Pod 实时用量。
func (h *ObsQueryHandler) PodUsage(c *gin.Context) {
	base, token, ok := h.prom(c)
	if !ok {
		return
	}
	usage := map[string]map[string]float64{}
	get := func(k string) map[string]float64 {
		if usage[k] == nil {
			usage[k] = map[string]float64{}
		}
		return usage[k]
	}
	if cpu, err := promInstant(base, token, `sum by(namespace,pod)(rate(container_cpu_usage_seconds_total{container!=""}[5m]))`); err == nil {
		for _, s := range cpu {
			get(s.Metric["namespace"]+"/"+s.Metric["pod"])["cpu_m"] = s.Value * 1000
		}
	}
	if mem, err := promInstant(base, token, `sum by(namespace,pod)(container_memory_working_set_bytes{container!=""})`); err == nil {
		for _, s := range mem {
			get(s.Metric["namespace"]+"/"+s.Metric["pod"])["mem_mi"] = s.Value / 1048576
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "usage": usage})
}

// NodeUsage 返回 {"node": {cpu_pct, mem_pct}}（来自 node-exporter）。
func (h *ObsQueryHandler) NodeUsage(c *gin.Context) {
	base, token, ok := h.prom(c)
	if !ok {
		return
	}
	usage := map[string]map[string]float64{}
	get := func(k string) map[string]float64 {
		if usage[k] == nil {
			usage[k] = map[string]float64{}
		}
		return usage[k]
	}
	if cpu, err := promInstant(base, token, `(1 - avg by(node)(rate(node_cpu_seconds_total{mode="idle"}[5m]))) * 100`); err == nil {
		for _, s := range cpu {
			get(s.Metric["node"])["cpu_pct"] = s.Value
		}
	}
	if mem, err := promInstant(base, token, `(1 - sum by(node)(node_memory_MemAvailable_bytes) / sum by(node)(node_memory_MemTotal_bytes)) * 100`); err == nil {
		for _, s := range mem {
			get(s.Metric["node"])["mem_pct"] = s.Value
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "usage": usage})
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
	// 自定义起止时间(unix 秒)优先于 minutes
	if s, e1 := strconv.ParseInt(c.Query("start"), 10, 64); e1 == nil && s > 0 {
		if en, e2 := strconv.ParseInt(c.Query("end"), 10, 64); e2 == nil && en > s {
			start = time.Unix(s, 0)
			end = time.Unix(en, 0)
			minutes = (en - s) / 60
		}
	}
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
		// 按 Pod 分组 → 图上一个服务的每个 Pod 各一条线
		if metric == "mem" {
			return fmt.Sprintf(`sum by(pod)(container_memory_working_set_bytes{namespace="%s",pod=~"%s-.*",container!=""})`, ns, name)
		}
		return fmt.Sprintf(`sum by(pod)(rate(container_cpu_usage_seconds_total{namespace="%s",pod=~"%s-.*",container!=""}[5m]))`, ns, name)
	case "node":
		// 节点：用 node-exporter 绝对用量(核/字节)，与 Pod 单位一致；按 node 标签匹配。
		if metric == "mem" {
			return fmt.Sprintf(`sum(node_memory_MemTotal_bytes{node="%s"}) - sum(node_memory_MemAvailable_bytes{node="%s"})`, name, name)
		}
		return fmt.Sprintf(`sum(rate(node_cpu_seconds_total{mode!="idle",node="%s"}[5m]))`, name)
	case "host":
		// 传统主机：node-exporter，按 instance 匹配(通常 <ip>:9100)，传入主机内网IP。
		if metric == "mem" {
			return fmt.Sprintf(`sum(node_memory_MemTotal_bytes{instance=~"%s.*"}) - sum(node_memory_MemAvailable_bytes{instance=~"%s.*"})`, name, name)
		}
		return fmt.Sprintf(`sum(rate(node_cpu_seconds_total{mode!="idle",instance=~"%s.*"}[5m]))`, name)
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

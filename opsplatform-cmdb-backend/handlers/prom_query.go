package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 通用 Prometheus 查询。
//
// 在此之前，Prometheus 只能通过 node_usage/pod_usage/pvc_usage 等几个写死的封装访问，
// 而 Loki 早就有 query_loki 可以自由查。结果是：中间件的一切指标——Kafka 消费积压、
// nacos 注册实例数、etcd 写延迟、Harbor 存储配额——CMDB 全都查不到，
// 尽管这些指标就在已经接好的 Prometheus 里躺着。
//
// 与其为每个中间件写一套专用接口（写不完，且每加一个组件就要改代码发版），
// 不如把查询能力本身开出来，再补一个指标发现工具解决「不知道指标名」的问题。
//
// 只读：Prometheus 的 query API 本身不具备写能力，符合 CMDB 的只读约束。

// 单次查询返回的序列上限。PromQL 一个手滑（比如查 `up` 不加过滤）能返回上万条，
// 塞进 AI 上下文既无用又昂贵，超出就截断并明确告知。
const promMaxSeries = 300

// clusterPlaceholder 调用方在 PromQL 里写 $CLUSTER，由后端替换成真实的集群标签选择器。
//
// 为什么不自动注入：任意 PromQL 里注入标签需要解析 AST（聚合、子查询、函数嵌套都要处理），
// 做不对就是静默给错数据。用占位符把这件事显式化——写了就隔离，没写就警告，两种情况都不会骗人。
const clusterPlaceholder = "$CLUSTER"

type promQueryHandler struct{ *ObsQueryHandler }

// RegisterPromQuery 挂通用查询路由。挂在 ObsQueryHandler 上以复用数据源解析与凭证解密。
func (h *ObsQueryHandler) RegisterPromQuery(r *gin.RouterGroup) {
	q := promQueryHandler{h}
	r.GET("/obs/prom-query", q.Query)     // cluster_id/env, query, minutes?, step?
	r.GET("/obs/prom-metrics", q.Metrics) // cluster_id/env, keyword?, limit?
	r.GET("/obs/prom-labels", q.Labels)   // cluster_id/env, label, metric?
}

// applyCluster 替换 $CLUSTER 占位符，并判断这次查询是否真的做了集群隔离。
func applyCluster(query, selector string) (out string, isolated bool, note string) {
	has := strings.Contains(query, clusterPlaceholder)
	switch {
	case selector == "":
		// 数据源只有单集群数据，无需隔离；占位符替换成空标签。
		out = strings.ReplaceAll(query, "{"+clusterPlaceholder+"}", "")
		out = strings.ReplaceAll(out, clusterPlaceholder, "")
		return out, true, "该数据源只有本集群数据，无需集群隔离"
	case has:
		return strings.ReplaceAll(query, clusterPlaceholder, selector), true,
			"已按 " + selector + " 隔离到本集群"
	default:
		// 关键分支：多集群共享数据源 + 查询没做隔离 = 结果可能混入别的集群。
		// 这种情况必须说出来，否则会拿着掺了其它集群的数字下结论。
		return query, false,
			"⚠️ 该 Prometheus 由多个集群共享，但本次查询未做集群隔离，结果可能混入其它集群的数据。" +
				"请在指标的标签选择器里加入 " + clusterPlaceholder + "，例如 up{" + clusterPlaceholder + "}（将替换为 " + selector + "）"
	}
}

type promSeries struct {
	Metric map[string]string `json:"metric"`
	Value  *float64          `json:"value,omitempty"`  // instant
	Values [][]any           `json:"values,omitempty"` // range: [[ts, val], ...]
}

// Query 执行 PromQL。不传 minutes 走瞬时查询，传了走区间查询（看趋势）。
func (h promQueryHandler) Query(c *gin.Context) {
	query := strings.TrimSpace(c.Query("query"))
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query 必填（PromQL）"})
		return
	}
	base, token, sel, ok := h.prom(c)
	if !ok {
		return
	}
	final, isolated, note := applyCluster(query, sel)

	minutes, _ := strconv.Atoi(c.Query("minutes"))
	var u string
	rangeMode := minutes > 0
	if rangeMode {
		step := c.Query("step")
		if step == "" {
			step = autoStep(minutes)
		}
		end := time.Now()
		u = fmt.Sprintf("%s/api/v1/query_range?query=%s&start=%d&end=%d&step=%s",
			base, url.QueryEscape(final), end.Add(-time.Duration(minutes)*time.Minute).Unix(), end.Unix(), url.QueryEscape(step))
	} else {
		u = fmt.Sprintf("%s/api/v1/query?query=%s", base, url.QueryEscape(final))
	}

	code, body, err := obsGet(u, token, 30*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "连接 Prometheus 失败: " + err.Error()})
		return
	}
	if code != 200 {
		// Prometheus 把 PromQL 语法错误也放在响应体里，原样带出去比只说 HTTP 400 有用得多。
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("Prometheus HTTP %d", code),
			"detail": truncate(body, 400), "query_sent": final})
		return
	}

	var r struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
				Values [][]any           `json:"values"`
			} `json:"result"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "解析响应失败: " + err.Error()})
		return
	}
	if r.Status != "success" {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": r.Error, "query_sent": final})
		return
	}

	series := make([]promSeries, 0, len(r.Data.Result))
	for i, res := range r.Data.Result {
		if i >= promMaxSeries {
			break
		}
		s := promSeries{Metric: res.Metric}
		if rangeMode {
			s.Values = res.Values
		} else if len(res.Value) == 2 {
			if str, ok := res.Value[1].(string); ok {
				if f, err := strconv.ParseFloat(str, 64); err == nil {
					s.Value = &f
				}
			}
		}
		series = append(series, s)
	}

	out := gin.H{
		"ok": true, "query_sent": final, "result_type": r.Data.ResultType,
		"series_count": len(r.Data.Result), "series": series,
		"cluster_isolated": isolated, "note": note,
	}
	if len(r.Data.Result) > promMaxSeries {
		out["truncated"] = fmt.Sprintf("共 %d 条序列，只返回前 %d 条——请用标签选择器缩小范围，或用 topk()/聚合",
			len(r.Data.Result), promMaxSeries)
	}
	if len(r.Data.Result) == 0 {
		// 空结果的两种含义必须分开：指标不存在 vs 指标存在但当前无数据。
		out["empty_hint"] = "无数据。可能是：指标名不存在（用 prom_metrics 确认）、" +
			"标签选择器过严、或该 exporter 未部署/已挂。空结果不等于「该组件正常」"
	}
	c.JSON(http.StatusOK, out)
}

// autoStep 按时间窗自动选步长，控制在 ~200 个点以内——再密对判断趋势没有帮助，只是撑大响应。
func autoStep(minutes int) string {
	switch {
	case minutes <= 30:
		return "15s"
	case minutes <= 180:
		return "1m"
	case minutes <= 720:
		return "5m"
	case minutes <= 1440:
		return "10m"
	default:
		return "1h"
	}
}

// Metrics 指标发现：不知道指标名就没法查，这是通用查询能用起来的前提。
func (h promQueryHandler) Metrics(c *gin.Context) {
	base, token, _, ok := h.prom(c)
	if !ok {
		return
	}
	code, body, err := obsGet(base+"/api/v1/label/__name__/values", token, 30*time.Second)
	if err != nil || code != 200 {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("取指标列表失败 (HTTP %d): %v", code, err)})
		return
	}
	var r struct {
		Data []string `json:"data"`
	}
	if json.Unmarshal([]byte(body), &r) != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "解析指标列表失败"})
		return
	}

	kw := strings.ToLower(strings.TrimSpace(c.Query("keyword")))
	limit := 200
	if n, _ := strconv.Atoi(c.Query("limit")); n > 0 && n <= 2000 {
		limit = n
	}
	matched := make([]string, 0, 64)
	for _, m := range r.Data {
		if kw == "" || strings.Contains(strings.ToLower(m), kw) {
			matched = append(matched, m)
		}
	}
	sort.Strings(matched)
	total := len(matched)
	if len(matched) > limit {
		matched = matched[:limit]
	}
	out := gin.H{"ok": true, "total_in_prometheus": len(r.Data), "matched": total, "metrics": matched}
	if total > limit {
		out["truncated"] = fmt.Sprintf("匹配 %d 个，只返回前 %d 个；请用更具体的 keyword", total, limit)
	}
	if total == 0 && kw != "" {
		// 同样是「查不到 ≠ 不存在」：exporter 没部署时指标根本不会出现在这里。
		out["empty_hint"] = "没有匹配的指标。可能是该组件的 exporter 未部署、或指标名用词不同" +
			"（如 Kafka 可能是 kafka_ 或 kminion_，nacos 可能经 JMX exporter 暴露为 jvm_ 前缀）"
	}
	c.JSON(http.StatusOK, out)
}

// Labels 列某个标签的可选值，用来搞清楚「这个指标能按什么维度筛」。
func (h promQueryHandler) Labels(c *gin.Context) {
	label := strings.TrimSpace(c.Query("label"))
	if label == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "label 必填，如 namespace / job / instance"})
		return
	}
	base, token, sel, ok := h.prom(c)
	if !ok {
		return
	}
	u := base + "/api/v1/label/" + url.PathEscape(label) + "/values"
	if m := strings.TrimSpace(c.Query("metric")); m != "" {
		match := m
		if sel != "" {
			match = fmt.Sprintf("%s{%s}", m, sel)
		}
		u += "?match[]=" + url.QueryEscape(match)
	}
	code, body, err := obsGet(u, token, 30*time.Second)
	if err != nil || code != 200 {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": fmt.Sprintf("取标签值失败 (HTTP %d): %v", code, err)})
		return
	}
	var r struct {
		Data []string `json:"data"`
	}
	if json.Unmarshal([]byte(body), &r) != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "解析标签值失败"})
		return
	}
	sort.Strings(r.Data)
	if len(r.Data) > 500 {
		r.Data = r.Data[:500]
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "label": label, "values": r.Data, "count": len(r.Data)})
}

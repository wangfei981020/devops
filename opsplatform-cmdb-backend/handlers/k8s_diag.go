package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/diag"
	"opsplatform-cmdb-backend/k8ssource"
	"opsplatform-cmdb-backend/logx"
)

// K8sDiagHandler 合并自 k8sinsight：实时 Pod 日志/事件 + 规则诊断（只读）。
// 这些也是阶段5B MCP 的诊断工具后端。
type K8sDiagHandler struct {
	DB     *sql.DB
	Pool   *k8ssource.Pool
	Cipher *crypto.Cipher // 解 Loki endpoint 的 token：kubelet 取不到日志时退到 Loki
}

func NewK8sDiagHandler(db *sql.DB, pool *k8ssource.Pool, cipher *crypto.Cipher) *K8sDiagHandler {
	return &K8sDiagHandler{DB: db, Pool: pool, Cipher: cipher}
}

func (h *K8sDiagHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/pod-logs", h.Logs)     // cluster_id, namespace, pod, container, tail, previous
	r.GET("/k8s/pod-events", h.Events) // cluster_id, namespace, pod
	// cluster_id, namespace?, kind?, type?, reason?, sort?(count), min_count?, hours?, limit?, exclude_reason?, include_noise?
	r.GET("/k8s/events", h.ClusterEvents)
	r.GET("/k8s/diagnose", h.Diagnose) // cluster_id, namespace, pod → 规则诊断
	// cluster_id, namespace?, include_unused? → 配置引用完整性（缺哪个 ConfigMap/Secret）
	r.GET("/k8s/config-audit", h.ConfigAudit)
	// cluster_id, kind, name, namespace?, api_group? → 单对象完整 YAML（脱敏），看探针/env/亲和性等 spec
	r.GET("/k8s/manifest", h.Manifest)
}

// eventNoiseReasons 是实测确认会把事件视图整个淹掉的高频控制器噪声。
//
// 起因：UAT 上取 500 条事件回来，argocd 的 StatusRefreshed 占了三分之二，
// 而真问题（一个 Pod BackOff 82 万次、3 个孤儿 HPA 的 FailedGetScale 20 万次）被挤到看不见。
// 这些 reason 只代表「控制器又对账了一次」，排障用不上，默认剔掉；
// 显式传 reason= 点名查某一种、或 include_noise=1 时不剔。
var eventNoiseReasons = map[string]bool{
	"StatusRefreshed":    true, // argocd application-controller，每分钟数条
	"ResourceUpdated":    true,
	"OperationStarted":   true,
	"OperationCompleted": true,
}

// ClusterEvents 统一事件视图（全集群/命名空间，可按 involvedObject.kind(含 Node) / 类型 / reason 筛，实时）。
//
// 三个坑都是实测踩出来的：
//  1. kind/type/reason 必须下推成 apiserver 的 FieldSelector。原来是先无条件取 500 条、再在内存里筛，
//     等于「500 条里的过滤」——集群 Warning 上千条时，传 type=Warning 只能看到最新那批里的零头，
//     漏掉的恰恰是 count 几十万的重灾区。
//  2. 默认剔掉 eventNoiseReasons，否则真问题永远排不进前几十条。
//  3. 默认按时间倒序（前端事件流是这个语义，15 秒刷一次）；排障要找「反复发生」的传 sort=count。
func (h *K8sDiagHandler) ClusterEvents(c *gin.Context) {
	// 先判「集群存不存在」(404)，再判「连不连得上」(502)：
	// 这两件事的处置完全不同——前者是集群已被删除/传错了 id，后者是凭据或网络问题。
	// 原先都走 ClientFor 的 502，一律报"集群 N 不存在"，把连不上也说成了不存在。
	cid, ok := requireCluster(c, h.DB)
	if !ok {
		return
	}
	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	kind, typ, reason := c.Query("kind"), c.Query("type"), c.Query("reason")
	ns := c.Query("namespace") // 空=全部命名空间

	var sel []string
	if typ != "" {
		sel = append(sel, "type="+typ)
	}
	if reason != "" {
		sel = append(sel, "reason="+reason)
	}
	if kind != "" {
		sel = append(sel, "involvedObject.kind="+kind)
	}
	limit := int64(1000)
	if v, e := strconv.ParseInt(c.Query("limit"), 10, 64); e == nil && v > 0 && v <= 5000 {
		limit = v
	}
	opts := metav1.ListOptions{Limit: limit}
	if len(sel) > 0 {
		opts.FieldSelector = strings.Join(sel, ",")
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	list, err := cs.CoreV1().Events(ns).List(ctx, opts)
	if err != nil {
		logx.J("k8s_diag", "cluster_events_failed", map[string]any{
			"cluster_id": cid, "namespace": ns, "field_selector": opts.FieldSelector, "err": err.Error(),
		})
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	minCount := 0
	if v, e := strconv.Atoi(c.Query("min_count")); e == nil && v > 0 {
		minCount = v
	}
	var since time.Time
	if v, e := strconv.Atoi(c.Query("hours")); e == nil && v > 0 {
		since = time.Now().Add(-time.Duration(v) * time.Hour)
	}
	dropNoise := c.Query("include_noise") != "1" && reason == ""
	excluded := map[string]bool{}
	for _, r := range strings.Split(c.Query("exclude_reason"), ",") {
		if r = strings.TrimSpace(r); r != "" {
			excluded[r] = true
		}
	}

	out := []gin.H{}
	noiseDropped := 0
	for i := range list.Items {
		e := &list.Items[i]
		if dropNoise && eventNoiseReasons[e.Reason] {
			noiseDropped++
			continue
		}
		if excluded[e.Reason] || int(e.Count) < minCount {
			continue
		}
		ts := e.LastTimestamp.Time
		if ts.IsZero() {
			ts = e.EventTime.Time
		}
		if !since.IsZero() && ts.Before(since) {
			continue
		}
		out = append(out, gin.H{
			"type": e.Type, "reason": e.Reason, "message": e.Message, "count": e.Count,
			"kind": e.InvolvedObject.Kind, "object": e.InvolvedObject.Name, "namespace": e.Namespace,
			"last_seen": ts.Format("2006-01-02 15:04:05"),
		})
	}
	if c.Query("sort") == "count" {
		sort.SliceStable(out, func(i, j int) bool { return out[i]["count"].(int32) > out[j]["count"].(int32) })
	} else {
		sort.SliceStable(out, func(i, j int) bool { return out[i]["last_seen"].(string) > out[j]["last_seen"].(string) })
	}

	// 事件是 TTL 只有 1 小时的易失数据，取回多少 / 筛掉多少 / 有没有被 limit 截断都必须留痕，
	// 否则「查不到」分不清是真没发生、还是被剔了或截断了。
	truncated := int64(len(list.Items)) >= limit
	if truncated {
		logx.J("k8s_diag", "cluster_events_truncated", map[string]any{
			"cluster_id": cid, "namespace": ns, "limit": limit,
			"hint": "已达 limit，结果可能不完整，请收窄 namespace/kind/type 或提高 limit",
		})
	}
	logx.J("k8s_diag", "cluster_events", map[string]any{
		"cluster_id": cid, "namespace": ns, "field_selector": opts.FieldSelector, "sort": c.Query("sort"),
		"limit": limit, "fetched": len(list.Items), "returned": len(out),
		"noise_dropped": noiseDropped, "truncated": truncated,
	})
	c.JSON(http.StatusOK, out)
}

// Logs 取 Pod 日志（非流式，尾部 N 行）。
func (h *K8sDiagHandler) Logs(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	ns, pod := c.Query("namespace"), c.Query("pod")
	if cid == 0 || ns == "" || pod == "" {
		c.JSON(400, gin.H{"error": "cluster_id/namespace/pod 必填"})
		return
	}
	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	tail := int64(200)
	if t, e := strconv.ParseInt(c.Query("tail"), 10, 64); e == nil && t > 0 && t <= 5000 {
		tail = t
	}
	opts := &corev1.PodLogOptions{Container: c.Query("container"), TailLines: &tail}
	if c.Query("previous") == "1" {
		opts.Previous = true
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	raw, err := cs.CoreV1().Pods(ns).GetLogs(pod, opts).DoRaw(ctx)
	if err != nil {
		// 与 diagnose_pod 同一套处理：翻译报错 + 退到 Loki。
		// 否则调用方在这里只会拿到 "unknown (get pods xxx)"，既不知道原因也没有退路。
		explained := diag.ExplainLogError(err)
		if lines := h.lokiTail(cid, ns, pod, int(tail)); lines != "" {
			c.Data(http.StatusOK, "text/plain; charset=utf-8",
				[]byte("# kubelet 通道取日志失败，以下为 Loki 历史日志\n# 失败原因: "+explained+"\n\n"+lines))
			SetAuditTarget(c, ns+"/"+pod)
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": explained})
		return
	}
	SetAuditTarget(c, ns+"/"+pod)
	c.Data(http.StatusOK, "text/plain; charset=utf-8", raw)
}

// lokiTail 从 Loki 取某 Pod 的历史日志尾部；没配 Loki 或查不到时返回空串。
func (h *K8sDiagHandler) lokiTail(cid int, ns, pod string, tail int) string {
	var env string
	_ = h.DB.QueryRow(`SELECT COALESCE(environment,'') FROM k8s_clusters WHERE id=?`, cid).Scan(&env)
	base, token, err := resolveEndpoint(h.DB, h.Cipher, "loki", env, cid)
	if err != nil {
		return ""
	}
	if tail <= 0 || tail > 1000 {
		tail = 200
	}
	q := fmt.Sprintf(`{namespace=%q,pod=%q}`, ns, pod)
	u := fmt.Sprintf("%s/loki/api/v1/query_range?query=%s&limit=%d&direction=backward&start=%d&end=%d",
		base, url.QueryEscape(q), tail, time.Now().Add(-6*time.Hour).UnixNano(), time.Now().UnixNano())
	code, body, err := obsGet(u, token, 15*time.Second)
	if err != nil || code != 200 {
		return ""
	}
	return extractLokiLines(body, tail)
}

// Events 取 Pod 相关事件（Warning 优先、按时间倒序）。
func (h *K8sDiagHandler) Events(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	ns, pod := c.Query("namespace"), c.Query("pod")
	if cid == 0 || ns == "" || pod == "" {
		c.JSON(400, gin.H{"error": "cluster_id/namespace/pod 必填"})
		return
	}
	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	list, err := cs.CoreV1().Events(ns).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.name=" + pod})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	out := []gin.H{}
	for i := range list.Items {
		e := &list.Items[i]
		ts := e.LastTimestamp.Time
		if ts.IsZero() {
			ts = e.EventTime.Time
		}
		out = append(out, gin.H{"type": e.Type, "reason": e.Reason, "message": e.Message,
			"count": e.Count, "last_seen": ts.Format("2006-01-02 15:04:05")})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i]["last_seen"].(string) > out[j]["last_seen"].(string)
	})
	c.JSON(http.StatusOK, out)
}

// Diagnose 规则诊断：采集 Pod 上下文 → RuleProvider → 根因 + 建议（只给方案，人工执行）。
func (h *K8sDiagHandler) Diagnose(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	ns, pod := c.Query("namespace"), c.Query("pod")
	if cid == 0 || ns == "" || pod == "" {
		c.JSON(400, gin.H{"error": "cluster_id/namespace/pod 必填"})
		return
	}
	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	var clusterName string
	_ = h.DB.QueryRow(`SELECT COALESCE(display_name,name) FROM k8s_clusters WHERE id=?`, cid).Scan(&clusterName)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
	defer cancel()
	dc, err := diag.Collect(ctx, cs, clusterName, ns, pod)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	// kubelet 通道拿不到日志时退到 Loki。必须在诊断前补，规则要靠日志内容判根因。
	h.fillLogsFromLoki(ctx, cid, dc)
	res := diag.RuleProvider{}.Diagnose(dc)
	logx.J("k8s", "diagnose", map[string]any{"cluster_id": cid, "ns": ns, "pod": pod, "matched": res.Matched, "root_cause": res.RootCause})
	SetAuditTarget(c, ns+"/"+pod)
	c.JSON(http.StatusOK, gin.H{"result": res, "context": dc})
}

// fillLogsFromLoki 对 kubelet 取不到日志的容器，改用 Loki 补历史日志。
//
// 现实原因：取日志要经 APIServer 代理到节点 kubelet(10250)，节点一旦失联/繁忙就整条断掉，
// 而这类节点上的 Pod 往往正是要诊断的对象——最需要日志的时候恰恰取不到。
// Loki 是旁路采集的，不依赖 kubelet，正好补上这个盲区。
func (h *K8sDiagHandler) fillLogsFromLoki(ctx context.Context, cid int, dc *diag.DiagnosisContext) {
	if len(dc.LogErrors) == 0 {
		return
	}
	var env string
	_ = h.DB.QueryRow(`SELECT COALESCE(environment,'') FROM k8s_clusters WHERE id=?`, cid).Scan(&env)
	base, token, err := resolveEndpoint(h.DB, h.Cipher, "loki", env, cid)
	if err != nil {
		return // 该集群没配 Loki，保留原始报错即可
	}
	for container := range dc.LogErrors {
		q := fmt.Sprintf(`{namespace=%q,pod=%q}`, dc.Namespace, dc.PodName)
		u := fmt.Sprintf("%s/loki/api/v1/query_range?query=%s&limit=30&direction=backward&start=%d&end=%d",
			base, url.QueryEscape(q), time.Now().Add(-6*time.Hour).UnixNano(), time.Now().UnixNano())
		code, body, err := obsGet(u, token, 10*time.Second)
		if err != nil || code != 200 {
			continue
		}
		if lines := extractLokiLines(body, 30); lines != "" {
			dc.LogTails[container] = lines
			dc.LogSource[container] = "loki"
			dc.LogErrors[container] += "（已改用 Loki 历史日志，见 log_tails）"
		}
	}
}

// extractLokiLines 从 Loki query_range 响应里抽出日志正文，按时间正序拼成文本。
func extractLokiLines(body string, max int) string {
	var resp struct {
		Data struct {
			Result []struct {
				Values [][]string `json:"values"` // [[ns时间戳, 日志行], ...]
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(body), &resp) != nil {
		return ""
	}
	type entry struct{ ts, line string }
	all := []entry{}
	for _, r := range resp.Data.Result {
		for _, v := range r.Values {
			if len(v) == 2 {
				all = append(all, entry{v[0], v[1]})
			}
		}
	}
	if len(all) == 0 {
		return ""
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ts < all[j].ts })
	if len(all) > max {
		all = all[len(all)-max:]
	}
	out := make([]string, 0, len(all))
	for _, e := range all {
		out = append(out, strings.TrimRight(e.line, "\n"))
	}
	return strings.Join(out, "\n")
}

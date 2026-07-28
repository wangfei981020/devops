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
	r.GET("/k8s/pod-logs", h.Logs)        // cluster_id, namespace, pod, container, tail, previous
	r.GET("/k8s/pod-events", h.Events)    // cluster_id, namespace, pod
	r.GET("/k8s/events", h.ClusterEvents) // cluster_id, namespace?, kind?(Node/Pod/..), type?(Warning/Normal) 统一事件
	r.GET("/k8s/diagnose", h.Diagnose)    // cluster_id, namespace, pod → 规则诊断
}

// ClusterEvents 统一事件视图（全集群/命名空间，可按 involvedObject.kind(含 Node) / 类型 筛，实时）。
func (h *K8sDiagHandler) ClusterEvents(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	if cid == 0 {
		c.JSON(400, gin.H{"error": "cluster_id 必填"})
		return
	}
	cs, err := h.Pool.ClientFor(cid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	kind, typ := c.Query("kind"), c.Query("type")
	ns := c.Query("namespace") // 空=全部命名空间
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	limit := int64(500)
	list, err := cs.CoreV1().Events(ns).List(ctx, metav1.ListOptions{Limit: limit})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	out := []gin.H{}
	for i := range list.Items {
		e := &list.Items[i]
		if kind != "" && e.InvolvedObject.Kind != kind {
			continue
		}
		if typ != "" && e.Type != typ {
			continue
		}
		ts := e.LastTimestamp.Time
		if ts.IsZero() {
			ts = e.EventTime.Time
		}
		out = append(out, gin.H{
			"type": e.Type, "reason": e.Reason, "message": e.Message, "count": e.Count,
			"kind": e.InvolvedObject.Kind, "object": e.InvolvedObject.Name, "namespace": e.Namespace,
			"last_seen": ts.Format("2006-01-02 15:04:05"),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i]["last_seen"].(string) > out[j]["last_seen"].(string) })
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
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "view_pod_log", ns+"/"+pod)
	c.Data(http.StatusOK, "text/plain; charset=utf-8", raw)
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
	WriteAudit(h.DB, c, "diagnose_pod", ns+"/"+pod)
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

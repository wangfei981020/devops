package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"opsplatform-cmdb-backend/diag"
	"opsplatform-cmdb-backend/k8ssource"
	"opsplatform-cmdb-backend/logx"
)

// K8sDiagHandler 合并自 k8sinsight：实时 Pod 日志/事件 + 规则诊断（只读）。
// 这些也是阶段5B MCP 的诊断工具后端。
type K8sDiagHandler struct {
	DB   *sql.DB
	Pool *k8ssource.Pool
}

func NewK8sDiagHandler(db *sql.DB, pool *k8ssource.Pool) *K8sDiagHandler {
	return &K8sDiagHandler{DB: db, Pool: pool}
}

func (h *K8sDiagHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/pod-logs", h.Logs)      // cluster_id, namespace, pod, container, tail, previous
	r.GET("/k8s/pod-events", h.Events)  // cluster_id, namespace, pod
	r.GET("/k8s/events", h.ClusterEvents) // cluster_id, namespace?, kind?(Node/Pod/..), type?(Warning/Normal) 统一事件
	r.GET("/k8s/diagnose", h.Diagnose)  // cluster_id, namespace, pod → 规则诊断
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
	res := diag.RuleProvider{}.Diagnose(dc)
	logx.J("k8s", "diagnose", map[string]any{"cluster_id": cid, "ns": ns, "pod": pod, "matched": res.Matched, "root_cause": res.RootCause})
	WriteAudit(h.DB, c, "diagnose_pod", ns+"/"+pod)
	c.JSON(http.StatusOK, gin.H{"result": res, "context": dc})
}

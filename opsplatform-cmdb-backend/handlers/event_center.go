package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"opsplatform-cmdb-backend/k8ssource"
)

// EventCenterHandler 事件中心：聚合平台各处事件成统一时间线(到期/变更/同步失败/K8s Warning)。
// AI 排障入口:一次拉到"最近平台出了什么事"，再钻具体诊断。
type EventCenterHandler struct {
	DB   *sql.DB
	Pool *k8ssource.Pool
}

func NewEventCenterHandler(db *sql.DB, pool *k8ssource.Pool) *EventCenterHandler {
	return &EventCenterHandler{DB: db, Pool: pool}
}

func (h *EventCenterHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/event-center", h.List) // days,level(critical/warning/info),source(expiry/change/sync/k8s)
}

type evt struct {
	Time    string `json:"time"`
	Source  string `json:"source"` // expiry/change/sync/k8s
	Level   string `json:"level"`  // critical/warning/info
	Object  string `json:"object"`
	Title   string `json:"title"`
	Message string `json:"message"`
	Cluster string `json:"cluster,omitempty"`
	sortTs  time.Time
}

// List 聚合事件。days 默认30(到期/变更窗口);K8s Warning 取各启用集群实时(有界)。
func (h *EventCenterHandler) List(c *gin.Context) {
	days := 30
	if d, e := strconv.Atoi(c.Query("days")); e == nil && d > 0 && d <= 365 {
		days = d
	}
	srcFilter := c.Query("source")
	lvlFilter := c.Query("level")
	want := func(src, lvl string) bool {
		return (srcFilter == "" || srcFilter == src) && (lvlFilter == "" || lvlFilter == lvl)
	}
	events := []evt{}
	add := func(e evt) {
		if want(e.Source, e.Level) {
			e.Time = e.sortTs.Format("2006-01-02 15:04:05")
			events = append(events, e)
		}
	}

	// 1. 到期:证书(线上探测) + 域名注册
	if rows, _ := h.DB.Query(`SELECT r.host, r.cert_expiry_at FROM domain_records r
		WHERE r.cert_expiry_at IS NOT NULL AND r.cert_ignored=0 AND r.cert_expiry_at <= NOW()+INTERVAL ? DAY
		ORDER BY r.cert_expiry_at`, days); rows != nil {
		for rows.Next() {
			var host string
			var exp sql.NullTime
			if rows.Scan(&host, &exp) == nil && exp.Valid {
				add(evt{Source: "expiry", Level: expiryLevel(exp.Time), Object: host,
					Title: "证书到期", Message: "证书 " + exp.Time.Format("2006-01-02") + " 到期", sortTs: exp.Time})
			}
		}
		rows.Close()
	}
	if rows, _ := h.DB.Query(`SELECT c.name, d.expiry_at FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.type='domain' AND d.stale=0 AND d.ignored=0 AND d.expiry_at IS NOT NULL AND d.expiry_at <= NOW()+INTERVAL ? DAY
		ORDER BY d.expiry_at`, days); rows != nil {
		for rows.Next() {
			var name string
			var exp sql.NullTime
			if rows.Scan(&name, &exp) == nil && exp.Valid {
				add(evt{Source: "expiry", Level: expiryLevel(exp.Time), Object: name,
					Title: "域名到期", Message: "域名注册 " + exp.Time.Format("2006-01-02") + " 到期", sortTs: exp.Time})
			}
		}
		rows.Close()
	}

	// 2. 工作负载变更
	if rows, _ := h.DB.Query(`SELECT namespace,kind,name,field,old_value,new_value,changed_at FROM k8s_changes
		WHERE changed_at >= NOW()-INTERVAL ? DAY ORDER BY changed_at DESC LIMIT 300`, days); rows != nil {
		for rows.Next() {
			var ns, kind, name, field, ov, nv string
			var ts time.Time
			if rows.Scan(&ns, &kind, &name, &field, &ov, &nv, &ts) == nil {
				fn := map[string]string{"image": "镜像", "replicas": "副本"}[field]
				if fn == "" {
					fn = field
				}
				add(evt{Source: "change", Level: "info", Object: ns + "/" + name,
					Title: kind + " " + fn + "变更", Message: ov + " → " + nv, sortTs: ts})
			}
		}
		rows.Close()
	}

	// 3. 同步失败:K8s 采集失败 + 云项目同步失败
	if rows, _ := h.DB.Query(`SELECT s.resource, s.err, s.last_sync, COALESCE(k.display_name,k.name)
		FROM k8s_sync_state s JOIN k8s_clusters k ON k.id=s.cluster_id WHERE s.ok=0 AND s.err<>''`); rows != nil {
		for rows.Next() {
			var res, errMsg, cl string
			var ts sql.NullTime
			if rows.Scan(&res, &errMsg, &ts, &cl) == nil {
				t := time.Now()
				if ts.Valid {
					t = ts.Time
				}
				add(evt{Source: "sync", Level: "warning", Object: cl + "/" + res,
					Title: "K8s 采集失败", Message: truncStr(errMsg, 200), Cluster: cl, sortTs: t})
			}
		}
		rows.Close()
	}
	if rows, _ := h.DB.Query(`SELECT project_id, last_result, last_sync_at FROM cloud_account_projects
		WHERE last_result<>'' AND (last_result LIKE '%error%' OR last_result LIKE '%失败%' OR last_result LIKE '%fail%' OR last_result LIKE '%denied%')`); rows != nil {
		for rows.Next() {
			var pid, lr string
			var ts sql.NullTime
			if rows.Scan(&pid, &lr, &ts) == nil {
				t := time.Now()
				if ts.Valid {
					t = ts.Time
				}
				add(evt{Source: "sync", Level: "warning", Object: pid,
					Title: "云项目同步失败", Message: truncStr(lr, 200), sortTs: t})
			}
		}
		rows.Close()
	}

	// 4. K8s Warning 事件(各启用集群实时,有界)
	if srcFilter == "" || srcFilter == "k8s" {
		if lvlFilter == "" || lvlFilter == "warning" {
			h.collectK8sWarnings(c.Request.Context(), days, &events, add)
		}
	}

	sort.Slice(events, func(i, j int) bool { return events[i].sortTs.After(events[j].sortTs) })
	if len(events) > 500 {
		events = events[:500]
	}
	c.JSON(http.StatusOK, gin.H{"events": events, "count": len(events)})
}

func (h *EventCenterHandler) collectK8sWarnings(ctx context.Context, days int, _ *[]evt, add func(evt)) {
	rows, err := h.DB.Query(`SELECT id, COALESCE(display_name,name) FROM k8s_clusters WHERE enabled=1`)
	if err != nil {
		return
	}
	type cl struct {
		id   int
		name string
	}
	cls := []cl{}
	for rows.Next() {
		var x cl
		if rows.Scan(&x.id, &x.name) == nil {
			cls = append(cls, x)
		}
	}
	rows.Close()
	cutoff := time.Now().AddDate(0, 0, -days)
	for _, x := range cls {
		cs, err := h.Pool.ClientFor(x.id)
		if err != nil {
			continue // 集群不可达:跳过,不影响其它源
		}
		cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		list, err := cs.CoreV1().Events("").List(cctx, metav1.ListOptions{Limit: 300})
		cancel()
		if err != nil {
			continue
		}
		for i := range list.Items {
			e := &list.Items[i]
			if e.Type != "Warning" {
				continue
			}
			ts := e.LastTimestamp.Time
			if ts.IsZero() {
				ts = e.EventTime.Time
			}
			if ts.Before(cutoff) {
				continue
			}
			obj := e.InvolvedObject.Kind + " " + e.InvolvedObject.Name
			if e.InvolvedObject.Namespace != "" {
				obj = e.InvolvedObject.Namespace + "/" + e.InvolvedObject.Name
			}
			add(evt{Source: "k8s", Level: "warning", Object: obj,
				Title: e.Reason, Message: truncStr(e.Message, 240), Cluster: x.name, sortTs: ts})
		}
	}
}

func expiryLevel(t time.Time) string {
	if time.Until(t) <= 7*24*time.Hour {
		return "critical"
	}
	return "warning"
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

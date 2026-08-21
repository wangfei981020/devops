package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
)

// 事件列表。
//
// ⚠️ 只有 Warning。界面上必须写清楚这一点 —— 否则"事件"这个名字会让人
// 以为这里是全量事件流，从而得出"最近什么都没发生"的错误结论。
//
// ⚠️ 分页在 SQL 里做：事件是持续累积的，几周下来轻松上百万行。

type EventListHandler struct{ DB *sql.DB }

func NewEventListHandler(db *sql.DB) *EventListHandler { return &EventListHandler{DB: db} }

func (h *EventListHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/event-list", h.List)
}

type eventOut struct {
	ClusterID   int    `json:"cluster_id"`
	ClusterName string `json:"cluster_name"`
	// ClusterDisplay 别名；没设时等于技术名。
	//
	// 🔴 cluster_name 装的必须是**技术名**（kubectl / PromQL 标签里的那个）。
	//	原来这里装的是 COALESCE(display_name, name) —— 一个字段两种含义，
	//	前端拿到「开发环境集群」就再也拿不到 dev-k8s-cluster-01，
	//	想按全站统一的 clusterLabel 渲染也做不到（OPSCMDB-078）。
	ClusterDisplay string `json:"cluster_display_name"`
	Namespace      string `json:"namespace"`
	Kind           string `json:"kind"`
	ObjName        string `json:"obj_name"`
	// Reason / Message 原样透传：FailedScheduling、BackOff 是运维
	// 直接拿去 kubectl 和搜索引擎里查的词
	Reason  string `json:"reason"`
	Message string `json:"message"`
	// Count k8s 自己聚合的重复次数。1 次和 300 次是完全不同的严重度
	Count   int    `json:"count"`
	FirstAt string `json:"first_at,omitempty"`
	LastAt  string `json:"last_at,omitempty"`
}

// List GET /api/k8s/event-list
//
//	@Summary		事件（仅 Warning）
//	@Description	只采集并保留 Warning 事件；etcd 只留 1 小时，这里是落库后的可回看副本。
//	@Tags			k8s
//	@Produce		json
//	@Param			page		query		int		false	"页码，从 1 开始"
//	@Param			size		query		int		false	"每页条数"
//	@Param			q			query		string	false	"按对象名/原因/消息搜索"
//	@Param			cluster		query		string	false	"集群名，all 或集群名"
//	@Param			namespace	query		string	false	"命名空间，all 或具体值"
//	@Param			reason		query		string	false	"原因，all 或具体值"
//	@Success		200			{object}	httpx.ListResponse[handlers.eventOut]
//	@Router			/k8s/event-list [get]
func (h *EventListHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "cluster", "namespace", "reason")
	clusterExpr := "COALESCE(cl.name, ''), COALESCE(cl.display_name, cl.name, '')"
	isSet := func(v string) bool { return v != "" && v != "all" }

	base := func() *httpx.WhereBuilder {
		w := &httpx.WhereBuilder{}
		if kw := q.Keyword; kw != "" {
			like := httpx.EscapeLike(kw)
			w.Add("(e.obj_name LIKE ? OR e.reason LIKE ? OR e.message LIKE ?)", like, like, like)
		}
		return w
	}
	where := base().
		AddIf(isSet(q.Filters["cluster"]), clusterExpr+" = ?", q.Filters["cluster"]).
		AddIf(isSet(q.Filters["namespace"]), "e.namespace = ?", q.Filters["namespace"]).
		AddIf(isSet(q.Filters["reason"]), "e.reason = ?", q.Filters["reason"])

	var total int64
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_events e
		LEFT JOIN k8s_clusters cl ON cl.id = e.cluster_id `+where.SQL(), where.Args()...).
		Scan(&total); err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}

	// 默认按最后发生时间倒序：事件页人是来看"刚刚发生了什么"的
	orderBy := q.OrderBy(map[string]string{
		"last":  "e.last_at",
		"count": "e.count",
	}, "e.last_at DESC")

	limit, offset := q.LimitClause()
	rows, err := h.DB.Query(`SELECT e.cluster_id, `+clusterExpr+`, e.namespace, e.kind,
		e.obj_name, e.reason, e.message, e.count, e.first_at, e.last_at
		FROM k8s_events e LEFT JOIN k8s_clusters cl ON cl.id = e.cluster_id
		`+where.SQL()+` ORDER BY `+orderBy+` LIMIT ? OFFSET ?`,
		append(where.Args(), limit, offset)...)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	items := []eventOut{}
	for rows.Next() {
		var o eventOut
		var first, last sql.NullTime
		if rows.Scan(&o.ClusterID, &o.ClusterName, &o.ClusterDisplay, &o.Namespace, &o.Kind, &o.ObjName,
			&o.Reason, &o.Message, &o.Count, &first, &last) != nil {
			continue
		}
		if first.Valid {
			o.FirstAt = first.Time.Format(time.RFC3339)
		}
		if last.Valid {
			o.LastAt = last.Time.Format(time.RFC3339)
		}
		items = append(items, o)
	}

	// 分面：每个维度排除自己那条筛选（理由见 Pod 列表）
	facets := map[string]map[string]int64{}
	count := func(dim, expr string, w *httpx.WhereBuilder) {
		rows, err := h.DB.Query(`SELECT `+expr+` AS k, COUNT(*) FROM k8s_events e
			LEFT JOIN k8s_clusters cl ON cl.id = e.cluster_id `+w.SQL()+` GROUP BY k
			ORDER BY COUNT(*) DESC LIMIT 30`, w.Args()...)
		if err != nil {
			return
		}
		defer rows.Close()
		m := map[string]int64{}
		var all int64
		for rows.Next() {
			var k sql.NullString
			var n int64
			if rows.Scan(&k, &n) == nil {
				m[k.String] = n
				all += n
			}
		}
		m["all"] = all
		facets[dim] = m
	}
	count("cluster", clusterExpr, base().
		AddIf(isSet(q.Filters["namespace"]), "e.namespace = ?", q.Filters["namespace"]).
		AddIf(isSet(q.Filters["reason"]), "e.reason = ?", q.Filters["reason"]))
	count("namespace", "e.namespace", base().
		AddIf(isSet(q.Filters["cluster"]), clusterExpr+" = ?", q.Filters["cluster"]).
		AddIf(isSet(q.Filters["reason"]), "e.reason = ?", q.Filters["reason"]))
	count("reason", "e.reason", base().
		AddIf(isSet(q.Filters["cluster"]), clusterExpr+" = ?", q.Filters["cluster"]).
		AddIf(isSet(q.Filters["namespace"]), "e.namespace = ?", q.Filters["namespace"]))

	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(facets))
}

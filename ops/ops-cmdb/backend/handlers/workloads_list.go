package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
)

// 工作负载列表。
//
// 分页在 SQL 里做，理由同 Pod：工作负载虽比 Pod 少一个量级，
// 但大集群里也是万级，而且这一页天然会被"看全部命名空间"地用。

type WorkloadListHandler struct{ DB *sql.DB }

func NewWorkloadListHandler(db *sql.DB) *WorkloadListHandler {
	return &WorkloadListHandler{DB: db}
}

func (h *WorkloadListHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/workload-list", h.List)
}

type workloadOut struct {
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
	Name           string `json:"name"`

	// ReplicasDesired / ReplicasReady 是这一页的**核心信号**。
	//
	// ⚠️ 三种情况在界面上必须分开，它们看起来都"不是满副本"：
	//	3/3   正常
	//	1/3   降级中 —— 有实例在跑，但不够
	//	0/3   全挂 —— 一个都没起来
	//	0/0   **被刻意缩到 0**（停服、定时任务模板、金丝雀留空）
	//	      这是正常状态，标红会在每套环境里制造一批假故障
	ReplicasDesired int `json:"replicas_desired"`
	ReplicasReady   int `json:"replicas_ready"`

	// Image / ImageTag 分开存：排障时问的是"跑的是哪个 tag"，
	// 而完整镜像串里仓库地址往往长到把这一列挤没
	Image    string `json:"image"`
	ImageTag string `json:"image_tag"`

	Status   string `json:"status"`
	SyncedAt string `json:"synced_at,omitempty"`
}

// workloadHealthCase 健康度归档，用 SQL 表达是因为分页在 SQL 里做。
//
//	scaled_zero  期望副本为 0 —— **正常状态**，单独一档
//	down         期望 > 0 但一个都没就绪
//	degraded     起来了但不够
//	ok           满副本
const workloadHealthCase = `CASE
	WHEN w.replicas_desired = 0 THEN 'scaled_zero'
	WHEN w.replicas_ready = 0 THEN 'down'
	WHEN w.replicas_ready < w.replicas_desired THEN 'degraded'
	ELSE 'ok' END`

// List GET /api/k8s/workload-list
//
//	@Summary		工作负载列表
//	@Description	副本就绪情况、镜像 tag 与健康度。期望副本为 0 单独成一档（那是正常状态，不是故障）。
//	@Tags			k8s
//	@Produce		json
//	@Param			page		query		int		false	"页码，从 1 开始"
//	@Param			size		query		int		false	"每页条数"
//	@Param			q			query		string	false	"按名称/镜像搜索"
//	@Param			cluster		query		string	false	"集群名，all 或集群名"
//	@Param			namespace	query		string	false	"命名空间，all 或具体值"
//	@Param			health		query		string	false	"健康度"	Enums(all, down, degraded, scaled_zero, ok)
//	@Param			sort		query		string	false	"排序字段，前缀 - 为降序"	Enums(name, namespace, kind, ready)
//	@Success		200			{object}	httpx.ListResponse[handlers.workloadOut]
//	@Router			/k8s/workload-list [get]
func (h *WorkloadListHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "cluster", "namespace", "health")

	base := func() *httpx.WhereBuilder {
		w := &httpx.WhereBuilder{}
		if kw := q.Keyword; kw != "" {
			like := httpx.EscapeLike(kw)
			w.Add("(w.name LIKE ? OR w.image LIKE ?)", like, like)
		}
		return w
	}
	cluster, ns, health := q.Filters["cluster"], q.Filters["namespace"], q.Filters["health"]
	isSet := func(v string) bool { return v != "" && v != "all" }
	clusterExpr := "COALESCE(cl.name, ''), COALESCE(cl.display_name, cl.name, '')"

	where := base().
		AddIf(isSet(cluster), clusterExpr+" = ?", cluster).
		AddIf(isSet(ns), "w.namespace = ?", ns).
		AddIf(isSet(health), workloadHealthCase+" = ?", health)

	var total int64
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_workloads w
		LEFT JOIN k8s_clusters cl ON cl.id = w.cluster_id `+where.SQL(), where.Args()...).
		Scan(&total); err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}

	// 默认按严重度：全挂 → 降级 → 正常 → 缩到 0。
	// scaled_zero 排最后而不是最前 —— 它是正常状态，放前面等于天天提醒一件不用管的事
	orderBy := q.OrderBy(map[string]string{
		"name":      "w.name",
		"namespace": "w.namespace",
		"kind":      "w.kind",
		"ready":     "w.replicas_ready",
	}, `FIELD(`+workloadHealthCase+`, 'down','degraded','ok','scaled_zero'), w.namespace, w.name`)

	limit, offset := q.LimitClause()
	rows, err := h.DB.Query(`SELECT w.cluster_id, `+clusterExpr+`, w.namespace, w.kind, w.name,
		w.replicas_desired, w.replicas_ready, COALESCE(w.image,''), COALESCE(w.image_tag,''),
		COALESCE(w.status,''), w.synced_at
		FROM k8s_workloads w
		LEFT JOIN k8s_clusters cl ON cl.id = w.cluster_id
		`+where.SQL()+` ORDER BY `+orderBy+` LIMIT ? OFFSET ?`,
		append(where.Args(), limit, offset)...)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	items := []workloadOut{}
	for rows.Next() {
		var o workloadOut
		var synced sql.NullTime
		if err := rows.Scan(&o.ClusterID, &o.ClusterName, &o.ClusterDisplay, &o.Namespace, &o.Kind, &o.Name,
			&o.ReplicasDesired, &o.ReplicasReady, &o.Image, &o.ImageTag, &o.Status, &synced); err != nil {
			continue
		}
		if synced.Valid {
			o.SyncedAt = synced.Time.Format(time.RFC3339)
		}
		items = append(items, o)
	}

	c.JSON(http.StatusOK, httpx.NewList(items, q, total).
		WithFacets(h.facets(q, base, clusterExpr)))
}

// facets 分面。每个维度排除它自己那条筛选，且全量统计（理由见 Pod 列表）。
func (h *WorkloadListHandler) facets(
	q httpx.PageQuery, base func() *httpx.WhereBuilder, clusterExpr string,
) map[string]map[string]int64 {
	cluster, ns, health := q.Filters["cluster"], q.Filters["namespace"], q.Filters["health"]
	isSet := func(v string) bool { return v != "" && v != "all" }
	out := map[string]map[string]int64{}

	count := func(dim, expr string, w *httpx.WhereBuilder) {
		rows, err := h.DB.Query(`SELECT `+expr+` AS k, COUNT(*) FROM k8s_workloads w
			LEFT JOIN k8s_clusters cl ON cl.id = w.cluster_id `+w.SQL()+` GROUP BY k`, w.Args()...)
		if err != nil {
			// 查不出来时留成缺失，不要返回空 map —— 空 map 会让下拉里
			// 所有计数变成 0，看起来像"确实一条都没有"
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
		out[dim] = m
	}

	count("cluster", clusterExpr, base().
		AddIf(isSet(ns), "w.namespace = ?", ns).
		AddIf(isSet(health), workloadHealthCase+" = ?", health))
	count("namespace", "w.namespace", base().
		AddIf(isSet(cluster), clusterExpr+" = ?", cluster).
		AddIf(isSet(health), workloadHealthCase+" = ?", health))
	count("health", workloadHealthCase, base().
		AddIf(isSet(cluster), clusterExpr+" = ?", cluster).
		AddIf(isSet(ns), "w.namespace = ?", ns))

	return out
}

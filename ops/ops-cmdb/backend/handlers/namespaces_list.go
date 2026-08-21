package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
)

// 命名空间列表。
//
// 命名空间是百量级（大集群几百个），用内存分页 —— 与集群/节点同档。
// ⚠️ Pod 和工作负载不能照抄这份（万级以上必须在 SQL 里分页），见 CONVENTIONS §3.1。

type NamespaceListHandler struct{ DB *sql.DB }

func NewNamespaceListHandler(db *sql.DB) *NamespaceListHandler {
	return &NamespaceListHandler{DB: db}
}

func (h *NamespaceListHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/namespace-list", h.List)
}

type namespaceOut struct {
	ClusterID   int    `json:"cluster_id"`
	ClusterName string `json:"cluster_name"`
	// ClusterDisplay 别名；没设时等于技术名。
	//
	// 🔴 cluster_name 装的必须是**技术名**（kubectl / PromQL 标签里的那个）。
	//	原来这里装的是 COALESCE(display_name, name) —— 一个字段两种含义，
	//	前端拿到「开发环境集群」就再也拿不到 dev-k8s-cluster-01，
	//	想按全站统一的 clusterLabel 渲染也做不到（OPSCMDB-078）。
	ClusterDisplay string `json:"cluster_display_name"`
	Name           string `json:"name"`

	// Phase 原样透传：Active / Terminating。
	//
	// ⚠️ Terminating 不是"正在正常删除"就完事了 —— 卡在 Terminating 的命名空间
	// 是最常见的一类僵局（finalizer 没被清），它会一直占着名字，
	// 让同名的重建一直失败。所以它要作为一个**显眼的状态**出现，
	// 而不是和 Active 一样淡。
	Phase string `json:"phase"`

	// Project 归属项目（KubeSphere 的 project 之类）。空 = 没有归属登记，
	// 不是"没有项目"——界面上要能看出是没登记，而不是留白
	Project string `json:"project"`

	// 计数。null = 该集群还没采过对应资源，与 0（确实是空的）区分。
	// 判据抬到集群层，与节点列表、主机抽屉保持一致。
	Workloads *int64 `json:"workloads"`
	Pods      *int64 `json:"pods"`
	PodsBad   *int64 `json:"pods_bad"`

	SyncedAt string `json:"synced_at,omitempty"`
}

// List GET /api/k8s/namespace-list
//
//	@Summary		命名空间列表
//	@Description	含工作负载/Pod 计数与归属项目。未采集的计数为 null 而非 0。
//	@Tags			k8s
//	@Produce		json
//	@Param			page	query		int		false	"页码，从 1 开始"
//	@Param			size	query		int		false	"每页条数"
//	@Param			q		query		string	false	"按命名空间名或项目搜索"
//	@Param			cluster	query		string	false	"集群名，all 或集群名"
//	@Param			phase	query		string	false	"状态筛选，all 或 Active / Terminating"
//	@Param			sort	query		string	false	"排序字段，前缀 - 为降序"	Enums(name, cluster, workloads, pods)
//	@Success		200		{object}	httpx.ListResponse[handlers.namespaceOut]
//	@Router			/k8s/namespace-list [get]
func (h *NamespaceListHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "cluster", "phase")

	rows, err := h.DB.Query(`SELECT n.cluster_id, COALESCE(cl.name, ''), COALESCE(cl.display_name, cl.name, ''),
		n.name, COALESCE(n.phase,''), COALESCE(pr.project,''), n.synced_at
		FROM k8s_namespaces n
		LEFT JOIN k8s_clusters cl ON cl.id = n.cluster_id
		LEFT JOIN k8s_ns_project pr ON pr.cluster_id = n.cluster_id AND pr.namespace = n.name
		ORDER BY n.cluster_id, n.name`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	all := []namespaceOut{}
	for rows.Next() {
		var o namespaceOut
		var synced sql.NullTime
		if err := rows.Scan(&o.ClusterID, &o.ClusterName, &o.ClusterDisplay, &o.Name, &o.Phase, &o.Project, &synced); err != nil {
			continue
		}
		if synced.Valid {
			o.SyncedAt = synced.Time.Format(time.RFC3339)
		}
		all = append(all, o)
	}

	h.fillCounts(all)
	items, total, facets := namespacePage(all, q)
	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(facets))
}

// fillCounts 补工作负载与 Pod 计数。
//
// 同节点列表：判据抬到**集群**层。集群里有任意一行，说明那类资源采过了，
// 于是某个命名空间没有行就是真的空；集群一行都没有，才是"没采过"。
// 不这么分的话，同一个空命名空间在这一页显示"未接入"、在别处显示 0。
func (h *NamespaceListHandler) fillCounts(all []namespaceOut) {
	if len(all) == 0 {
		return
	}
	type key struct {
		cid int
		ns  string
	}
	wl := map[key]int64{}
	pods := map[key]int64{}
	bad := map[key]int64{}
	wlClusters := map[int]bool{}
	podClusters := map[int]bool{}

	if rows, err := h.DB.Query(
		`SELECT cluster_id, namespace, COUNT(*) FROM k8s_workloads GROUP BY cluster_id, namespace`); err == nil {
		for rows.Next() {
			var cid int
			var ns string
			var n int64
			if rows.Scan(&cid, &ns, &n) == nil {
				wl[key{cid, ns}] = n
				wlClusters[cid] = true
			}
		}
		rows.Close()
	}
	// ⚠️ 与 Pod 列表的 health=bad 一字不差：只算起不来的，**不含重启过的**
	if rows, err := h.DB.Query(`SELECT cluster_id, namespace, COUNT(*),
		SUM(phase NOT IN ('Running','Succeeded'))
		FROM k8s_pods GROUP BY cluster_id, namespace`); err == nil {
		for rows.Next() {
			var cid int
			var ns string
			var n, b sql.NullInt64
			if rows.Scan(&cid, &ns, &n, &b) == nil {
				pods[key{cid, ns}] = n.Int64
				bad[key{cid, ns}] = b.Int64
				podClusters[cid] = true
			}
		}
		rows.Close()
	}

	set := func(m map[key]int64, seen map[int]bool, cid int, ns string) *int64 {
		if v, ok := m[key{cid, ns}]; ok {
			return &v
		}
		if seen[cid] {
			zero := int64(0)
			return &zero
		}
		return nil // 该集群没采过这类资源
	}
	for i := range all {
		cid, ns := all[i].ClusterID, all[i].Name
		all[i].Workloads = set(wl, wlClusters, cid, ns)
		all[i].Pods = set(pods, podClusters, cid, ns)
		all[i].PodsBad = set(bad, podClusters, cid, ns)
	}
}

// namespacePage 纯函数：筛选 + 分面 + 排序 + 分页（百量级，内存里够用）。
func namespacePage(all []namespaceOut, q httpx.PageQuery) ([]namespaceOut, int64, map[string]map[string]int64) {
	facets := map[string]map[string]int64{"cluster": {}, "phase": {}}
	cluster, phase := q.Filters["cluster"], q.Filters["phase"]
	match := func(n namespaceOut) bool {
		if q.Keyword == "" {
			return true
		}
		kw := strings.ToLower(q.Keyword)
		return strings.Contains(strings.ToLower(n.Name), kw) ||
			strings.Contains(strings.ToLower(n.Project), kw)
	}

	for _, n := range all {
		if !match(n) {
			continue
		}
		// 分面排除自己那一维，否则切过去的条数全是 0
		if cluster == "" || cluster == "all" || n.ClusterName == cluster {
			facets["phase"][n.Phase]++
			facets["phase"]["all"]++
		}
		if phase == "" || phase == "all" || n.Phase == phase {
			facets["cluster"][n.ClusterName]++
			facets["cluster"]["all"]++
		}
	}

	filtered := make([]namespaceOut, 0, len(all))
	for _, n := range all {
		if !match(n) {
			continue
		}
		if cluster != "" && cluster != "all" && n.ClusterName != cluster {
			continue
		}
		if phase != "" && phase != "all" && n.Phase != phase {
			continue
		}
		filtered = append(filtered, n)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		var less bool
		switch q.SortBy {
		case "cluster":
			less = a.ClusterName < b.ClusterName
		case "workloads":
			less = derefI64(a.Workloads) < derefI64(b.Workloads)
		case "pods":
			less = derefI64(a.Pods) < derefI64(b.Pods)
		default:
			// 默认把 Terminating 顶到最前：卡在这个状态的命名空间会一直占着名字，
			// 让同名重建持续失败，而按字母序它可能在第 200 行
			if (a.Phase == "Terminating") != (b.Phase == "Terminating") {
				return a.Phase == "Terminating"
			}
			less = a.Name < b.Name
		}
		if q.SortDesc {
			return !less
		}
		return less
	})

	total := int64(len(filtered))
	lo := min(q.Offset(), len(filtered))
	hi := min(lo+q.Size, len(filtered))
	return filtered[lo:hi], total, facets
}

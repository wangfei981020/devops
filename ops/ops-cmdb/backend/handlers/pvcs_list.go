package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
)

// 存储卷（PVC）。

// PVCListHandler 存储卷列表。
//
// ⚠️ 需要 Store 而不只是 DB：卷成本要按**租户**的费率表算
// （不同客户和云厂商谈的折扣不一样），而费率读取强制走 Scoped。
type PVCListHandler struct {
	DB    *sql.DB
	Store *store.Store
}

func NewPVCListHandler(db *sql.DB, st *store.Store) *PVCListHandler {
	return &PVCListHandler{DB: db, Store: st}
}

// clusterLocation 集群所在区域。费率按区域分档，同一个卷在不同区域价格不同。
func (h *PVCListHandler) clusterLocation(cid int) string {
	var loc string
	_ = h.DB.QueryRow(`SELECT COALESCE(location,'') FROM k8s_clusters WHERE id=?`, cid).Scan(&loc)
	return loc
}

func (h *PVCListHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/pvc-list", h.List)
}

type pvcOut struct {
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
	Name           string `json:"name"`
	// Status 原样透传：Bound / Pending / Lost
	Status       string `json:"status"`
	Capacity     string `json:"capacity"`
	StorageClass string `json:"storage_class"`
	VolumeName   string `json:"volume_name"`

	// Orphan 没有任何 Pod 在用它。
	//
	// ⚠️ 这**不等于可以删**：定时任务的卷、刚 drain 完的有状态服务，
	// 都会短暂没有使用者。所以只标注"当前没有使用者"，不写"可回收" ——
	// 后者会让人放心地删掉别人的数据。
	Orphan bool `json:"orphan"`
	// MonthlyUSD 这个卷每月多少钱。**只有能算出来的才有**（0 = 算不出来，不是免费）。
	//
	//	⚠️ 后端一直在别处算这个数（orphans.go 里逐条算好了），
	//	而这一页一个字都没显示 —— 于是「1Ti 当前无使用者」只是个中性事实，
	//	紧跟一个「$102.4/月」才产生行动力。
	//	实测 DEV 集群 21 个无使用者的卷，合计 **$866/月**（OPSCMDB-031 P1-23）。
	MonthlyUSD float64 `json:"monthly_usd,omitempty"`
	SyncedAt   string  `json:"synced_at,omitempty"`
}

// pvcHealth 归档。
//
//	lost      卷丢了 —— 数据可能已经没了，最该看
//	pending   一直没绑上 —— 这是 Pod 起不来的最常见原因之一
//	orphan    绑着但没人用（可能是遗留，也可能是正常的）
//	ok
func pvcHealth(p pvcOut) string {
	switch p.Status {
	case "Lost":
		return "lost"
	case "Pending":
		return "pending"
	}
	if p.Orphan {
		return "orphan"
	}
	return "ok"
}

// List GET /api/k8s/pvc-list
//
//	@Summary		存储卷列表
//	@Description	含"当前没有使用者"标注（注意：那不等于可以删）。
//	@Tags			k8s
//	@Produce		json
//	@Param			page		query		int		false	"页码，从 1 开始"
//	@Param			size		query		int		false	"每页条数"
//	@Param			q			query		string	false	"按名称/存储类搜索"
//	@Param			cluster		query		string	false	"集群名，all 或集群名"
//	@Param			health		query		string	false	"健康度"	Enums(all, lost, pending, orphan, ok)
//	@Success		200			{object}	httpx.ListResponse[handlers.pvcOut]
//	@Router			/k8s/pvc-list [get]
func (h *PVCListHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "cluster", "health")

	rows, err := h.DB.Query(`SELECT p.cluster_id, COALESCE(cl.name, ''), COALESCE(cl.display_name, cl.name, ''),
		p.namespace, p.name, COALESCE(p.status,''), COALESCE(p.capacity,''),
		COALESCE(p.storage_class,''), COALESCE(p.volume_name,''), p.synced_at
		FROM k8s_pvcs p
		LEFT JOIN k8s_clusters cl ON cl.id = p.cluster_id
		ORDER BY p.cluster_id, p.namespace, p.name`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	all := []pvcOut{}
	for rows.Next() {
		var o pvcOut
		var synced sql.NullTime
		if err := rows.Scan(&o.ClusterID, &o.ClusterName, &o.ClusterDisplay, &o.Namespace, &o.Name, &o.Status,
			&o.Capacity, &o.StorageClass, &o.VolumeName, &synced); err != nil {
			continue
		}
		if synced.Valid {
			o.SyncedAt = synced.Time.Format(time.RFC3339)
		}
		all = append(all, o)
	}

	h.markOrphans(all)
	h.fillMonthlyCost(all)
	items, total, facets := pvcPage(all, q)
	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(facets))
}

// fillMonthlyCost 给每个卷算月成本。
//
//	⚠️ 用的是和 orphans.go **同一套**算法（容量 GB × 该区域该 storageClass 的单价）。
//	两处各算一份的话，同一个卷在「存储卷」页和「孤儿资源」里会显示不同的钱数 ——
//	而钱是最不能出现两个说法的东西。
//
//	算不出来（费率表里没有对应档、或容量解析不了）时留 0 并**不显示**，
//	而不是显示 $0 —— 后者会被读成"这个卷不要钱"。
func (h *PVCListHandler) fillMonthlyCost(all []pvcOut) {
	if len(all) == 0 {
		return
	}
	sc, err := h.Store.Tenant(context.Background())
	if err != nil {
		logx.J("pvcs", "cost_skip", map[string]any{
			"err": err.Error(), "note": "取不到租户上下文，本次不算卷成本（界面上不显示金额，不是显示 0）",
		})
		return
	}
	rc := newRateCache(sc)
	// 集群 → 区域。费率按区域分档，同一个卷在不同区域价格不同
	loc := map[int]string{}
	for i := range all {
		cid := all[i].ClusterID
		if _, ok := loc[cid]; !ok {
			loc[cid] = h.clusterLocation(cid)
		}
		gb := capToGB(all[i].Capacity)
		rate := rc.diskRate(loc[cid], all[i].StorageClass)
		if gb > 0 && rate > 0 {
			all[i].MonthlyUSD = round2(float64(gb) * rate)
		}
	}
}

// markOrphans 标注"当前没有使用者"。
//
// ⚠️ 只在**该集群采过卷挂载关系**时才标：一行都没采过的话，
// 所有卷都会被标成孤儿 —— 那正是"没采到被渲染成确实没有"的老毛病。
func (h *PVCListHandler) markOrphans(all []pvcOut) {
	if len(all) == 0 {
		return
	}
	type key struct {
		cid int
		ns  string
		pvc string
	}
	used := map[key]bool{}
	seen := map[int]bool{}
	if rows, err := h.DB.Query(
		`SELECT cluster_id, namespace, pvc_name FROM k8s_pod_volumes WHERE pvc_name <> ''`); err == nil {
		for rows.Next() {
			var cid int
			var ns, claim string
			if rows.Scan(&cid, &ns, &claim) == nil {
				used[key{cid, ns, claim}] = true
				seen[cid] = true
			}
		}
		rows.Close()
	}
	for i := range all {
		if !seen[all[i].ClusterID] {
			continue // 该集群没采过挂载关系，不下结论
		}
		all[i].Orphan = !used[key{all[i].ClusterID, all[i].Namespace, all[i].Name}]
	}
}

func pvcPage(all []pvcOut, q httpx.PageQuery) ([]pvcOut, int64, map[string]map[string]int64) {
	match := func(p pvcOut) bool {
		if q.Keyword == "" {
			return true
		}
		kw := strings.ToLower(q.Keyword)
		return strings.Contains(strings.ToLower(p.Name), kw) ||
			strings.Contains(strings.ToLower(p.StorageClass), kw)
	}
	cluster, health := q.Filters["cluster"], q.Filters["health"]
	facets := map[string]map[string]int64{"cluster": {}, "health": {}}
	for _, p := range all {
		if !match(p) {
			continue
		}
		if cluster == "" || cluster == "all" || p.ClusterName == cluster {
			facets["health"][pvcHealth(p)]++
			facets["health"]["all"]++
		}
		if health == "" || health == "all" || pvcHealth(p) == health {
			facets["cluster"][p.ClusterName]++
			facets["cluster"]["all"]++
		}
	}

	filtered := make([]pvcOut, 0, len(all))
	for _, p := range all {
		if !match(p) {
			continue
		}
		if cluster != "" && cluster != "all" && p.ClusterName != cluster {
			continue
		}
		if health != "" && health != "all" && pvcHealth(p) != health {
			continue
		}
		filtered = append(filtered, p)
	}

	sev := func(p pvcOut) int {
		switch pvcHealth(p) {
		case "lost":
			return 0
		case "pending":
			return 1
		case "orphan":
			return 2
		default:
			return 3
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		var less bool
		switch q.SortBy {
		case "namespace":
			less = a.Namespace < b.Namespace
		default:
			if s1, s2 := sev(a), sev(b); s1 != s2 {
				return s1 < s2
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

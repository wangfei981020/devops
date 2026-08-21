package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
)

// Pod 列表。
//
// ⚠️ 与集群/节点列表**不同**：这里的分页、筛选、分面全部在 SQL 里做。
//
// clusterPage / nodePage 那种"全取到内存再切片"的写法只对十几到几百条成立。
// Pod 是十万级：一次全取会把内存打满，而且慢的时候看起来只是"页面转圈"，
// 没人会立刻联想到是分页方式的问题。样板可以照抄，**这一条不能照抄**。

type PodListHandler struct{ DB *sql.DB }

func NewPodListHandler(db *sql.DB) *PodListHandler { return &PodListHandler{DB: db} }

func (h *PodListHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/pod-list", h.List)
}

type podOut struct {
	ClusterID   int    `json:"cluster_id"`
	ClusterName string `json:"cluster_name"`
	// ClusterDisplay 别名；没设时等于技术名。
	//
	// 🔴 cluster_name 装的必须是**技术名**（kubectl / PromQL 标签里的那个）。
	//	原来这里装的是 COALESCE(display_name, name) —— 一个字段两种含义，
	//	前端拿到「开发环境集群」就再也拿不到 dev-k8s-cluster-01（OPSCMDB-078）。
	//
	// ⚠️ 筛选与分面也一并改成按技术名：分面值会被前端原样回传当筛选条件，
	//	用别名当键的话，改一次别名所有已保存的筛选就全失效了。
	ClusterDisplay string `json:"cluster_display_name"`
	Namespace      string `json:"namespace"`
	Name           string `json:"name"`
	NodeName       string `json:"node_name"`
	Workload       string `json:"workload"`

	// Phase / Reason 都**原样透传**：运维是拿 CrashLoopBackOff、
	// ImagePullBackOff 这些词直接去 kubectl 和搜索引擎里查的，
	// 翻译成中文就对不上了。
	Phase  string `json:"phase"`
	Reason string `json:"reason"`

	Restarts int    `json:"restarts"`
	PodIP    string `json:"pod_ip"`

	// CPUReqM / MemReqMi / CPULimM / MemLimMi —— **配置**不是用量。
	//
	//	「这个 Pod 申请了多少」是判断"是不是 request 写太大导致装不下"的唯一依据，
	//	用量页有实际用量，但那回答不了这个问题。
	//
	//	⚠️ 用指针是为了把 SQL NULL 原样透出去，**但当前采集写的是 0 不是 NULL**
	//	（实测 kube-scheduler：cpu_req_m=100，其余三项都是 0）。
	//	也就是说这一层现在区分不出「没配」和「配了 0」——
	//	好在 k8s 里不存在 request=0 的有效配置，所以前端把 0 当"没配"是安全的。
	//	留着指针是为了采集哪天改成写 NULL 时这里不用再动；
	//	🔴 但别据此以为已经能区分两者了 —— 真要区分得先改采集。
	CPUReqM  *int `json:"cpu_req_m,omitempty"`
	MemReqMi *int `json:"mem_req_mi,omitempty"`
	CPULimM  *int `json:"cpu_lim_m,omitempty"`
	MemLimMi *int `json:"mem_lim_mi,omitempty"`

	// StartTime 起来多久了。CrashLoop 的 Pod 会不断重启，
	// 这个值会一直很小 —— 它本身就是一条线索。
	StartTime string `json:"start_time,omitempty"`
	SyncedAt  string `json:"synced_at,omitempty"`
}

// podHealthCase 把 phase + restarts 归成筛选用的三档。
//
// 用 SQL 表达式而不是 Go 判断，是因为分页在 SQL 里做：
// 拿到内存里再判，分面计数和 total 就都只覆盖当前页了。
//
// ⚠️ 判据必须与主机抽屉、集群列表一致（不是 Running/Succeeded，或重启过 = 异常），
// 否则同一个 Pod 在三个页面上有三种说法。
const podHealthCase = `CASE
	WHEN p.phase NOT IN ('Running','Succeeded') THEN 'bad'
	WHEN p.restarts > 0 THEN 'restarted'
	ELSE 'ok' END`

// List GET /api/k8s/pod-list
//
//	@Summary		Pod 列表
//	@Description	分页、筛选、分面全部在 SQL 里做（Pod 是十万级，不能全取到内存）。
//	@Tags			k8s
//	@Produce		json
//	@Param			page		query		int		false	"页码，从 1 开始"
//	@Param			size		query		int		false	"每页条数"
//	@Param			q			query		string	false	"按 Pod 名/工作负载/节点名搜索"
//	@Param			cluster		query		string	false	"集群名，all 或集群名"
//	@Param			namespace	query		string	false	"命名空间，all 或具体值"
//	@Param			health		query		string	false	"健康度"	Enums(all, bad, restarted, ok)
//	@Param			sort		query		string	false	"排序字段，前缀 - 为降序"	Enums(name, namespace, restarts, started)
//	@Success		200			{object}	httpx.ListResponse[handlers.podOut]
//	@Router			/k8s/pod-list [get]
func (h *PodListHandler) List(c *gin.Context) {
	// 🔴 workload / node 是**精确**过滤，不是关键词。
	//
	//	靠关键词搜工作负载名只是**碰巧**能命中（Pod 名 = 工作负载名 + hash）；
	//	Job / CronJob 的 Pod 名与工作负载名对不上时就搜不到，
	//	而"搜不到"会被读成"这个工作负载没有 Pod"（OPSCMDB-028 GAP-11）。
	q := httpx.BindPage(c, "cluster", "namespace", "health", "workload", "node")

	base := func() *httpx.WhereBuilder {
		w := &httpx.WhereBuilder{}
		if kw := q.Keyword; kw != "" {
			like := httpx.EscapeLike(kw)
			w.Add("(p.name LIKE ? OR p.workload LIKE ? OR p.node_name LIKE ?)", like, like, like)
		}
		return w
	}
	cluster := q.Filters["cluster"]
	ns := q.Filters["namespace"]
	health := q.Filters["health"]
	isSet := func(v string) bool { return v != "" && v != "all" }

	where := base().
		AddIf(isSet(cluster), "COALESCE(cl.name, '') = ?", cluster).
		AddIf(isSet(ns), "p.namespace = ?", ns).
		AddIf(isSet(health), podHealthCase+" = ?", health).
		AddIf(isSet(q.Filters["workload"]), "p.workload = ?", q.Filters["workload"]).
		AddIf(isSet(q.Filters["node"]), "p.node_name = ?", q.Filters["node"])

	var total int64
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_pods p
		LEFT JOIN k8s_clusters cl ON cl.id = p.cluster_id `+where.SQL(), where.Args()...).
		Scan(&total); err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}

	// 默认按**严重度**排，不按名字：人是来找"哪个不对劲"的。
	// 与主机抽屉里那份排序同一套顺序（坏的 → 重启过的 → 正常）。
	orderBy := q.OrderBy(map[string]string{
		"name":      "p.name",
		"namespace": "p.namespace",
		"restarts":  "p.restarts",
		"started":   "p.start_time",
	}, `FIELD(`+podHealthCase+`, 'bad','restarted','ok'), p.restarts DESC, p.namespace, p.name`)

	limit, offset := q.LimitClause()
	rows, err := h.DB.Query(`SELECT p.cluster_id, COALESCE(cl.name, ''), COALESCE(cl.display_name, cl.name, ''),
		p.namespace, p.name, p.node_name, COALESCE(p.workload,''), p.phase,
		COALESCE(p.reason,''), p.restarts, COALESCE(p.pod_ip,''), p.start_time, p.synced_at,
		p.cpu_req_m, p.mem_req_mi, p.cpu_lim_m, p.mem_lim_mi
		FROM k8s_pods p
		LEFT JOIN k8s_clusters cl ON cl.id = p.cluster_id
		`+where.SQL()+` ORDER BY `+orderBy+` LIMIT ? OFFSET ?`,
		append(where.Args(), limit, offset)...)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	items := []podOut{}
	for rows.Next() {
		var o podOut
		var start, synced sql.NullTime
		var cpuReq, memReq, cpuLim, memLim sql.NullInt64
		if err := rows.Scan(&o.ClusterID, &o.ClusterName, &o.ClusterDisplay, &o.Namespace, &o.Name, &o.NodeName,
			&o.Workload, &o.Phase, &o.Reason, &o.Restarts, &o.PodIP, &start, &synced,
			&cpuReq, &memReq, &cpuLim, &memLim); err != nil {
			continue
		}
		// NULL → nil（没配），有值 → 指针。⚠️ 不要 .Int64 直接取：
		// 那会把 NULL 变成 0，于是「没配 request」显示成「配了 0」
		setInt := func(n sql.NullInt64, dst **int) {
			if n.Valid {
				v := int(n.Int64)
				*dst = &v
			}
		}
		setInt(cpuReq, &o.CPUReqM)
		setInt(memReq, &o.MemReqMi)
		setInt(cpuLim, &o.CPULimM)
		setInt(memLim, &o.MemLimMi)
		if start.Valid {
			o.StartTime = start.Time.Format(time.RFC3339)
		}
		if synced.Valid {
			o.SyncedAt = synced.Time.Format(time.RFC3339)
		}
		items = append(items, o)
	}

	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(h.facets(q, base)))
}

// facets 分面计数。
//
// ⚠️ 每个维度都要**排除它自己那一条筛选**，否则选中之后其余取值全是 0，
// 用户看不出"切过去还有多少"——而那正是下拉里那个数字存在的意义。
//
// ⚠️ 也必须是全量统计。按当前页统计的话数字是错的，
// 而它看起来完全正常，没人会怀疑。
func (h *PodListHandler) facets(q httpx.PageQuery, base func() *httpx.WhereBuilder) map[string]map[string]int64 {
	cluster := q.Filters["cluster"]
	ns := q.Filters["namespace"]
	health := q.Filters["health"]
	isSet := func(v string) bool { return v != "" && v != "all" }

	out := map[string]map[string]int64{}

	count := func(dim, expr string, w *httpx.WhereBuilder) {
		m := map[string]int64{}
		rows, err := h.DB.Query(`SELECT `+expr+` AS k, COUNT(*) FROM k8s_pods p
			LEFT JOIN k8s_clusters cl ON cl.id = p.cluster_id `+w.SQL()+` GROUP BY k`, w.Args()...)
		if err != nil {
			// 分面查不出来时**不要返回空 map**：空 map 会让下拉里所有计数变成 0，
			// 看起来像"确实一条都没有"。留成缺失，前端据此不显示计数
			return
		}
		defer rows.Close()
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

	count("cluster", "COALESCE(cl.name, '')", base().
		AddIf(isSet(ns), "p.namespace = ?", ns).
		AddIf(isSet(health), podHealthCase+" = ?", health).
		AddIf(isSet(q.Filters["workload"]), "p.workload = ?", q.Filters["workload"]).
		AddIf(isSet(q.Filters["node"]), "p.node_name = ?", q.Filters["node"]))

	count("namespace", "p.namespace", base().
		AddIf(isSet(cluster), "COALESCE(cl.name, '') = ?", cluster).
		AddIf(isSet(health), podHealthCase+" = ?", health).
		AddIf(isSet(q.Filters["workload"]), "p.workload = ?", q.Filters["workload"]).
		AddIf(isSet(q.Filters["node"]), "p.node_name = ?", q.Filters["node"]))

	// ⚠️ workload / node 这两条在**每个**维度上都要加（除了它们自己）。
	//	漏掉的话，从工作负载下钻进来时「健康度」的计数仍是全集群的数字，
	//	而列表只有那个工作负载的几个 Pod —— 两个数字对不上，人会怀疑数据错了。
	//
	//	⚠️ 另：这三处的 AddIf 早就写着 q.Filters["workload"]，
	//	但 BindPage 从来没绑过这个参数，取到的永远是空串 ——
	//	代码看着接好了，实际一直空转。绑定加在 List 开头。
	count("health", podHealthCase, base().
		AddIf(isSet(cluster), "COALESCE(cl.name, '') = ?", cluster).
		AddIf(isSet(ns), "p.namespace = ?", ns).
		AddIf(isSet(q.Filters["workload"]), "p.workload = ?", q.Filters["workload"]).
		AddIf(isSet(q.Filters["node"]), "p.node_name = ?", q.Filters["node"]))

	return out
}

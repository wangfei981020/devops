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

// 集群列表。阶段 3 的第一个页面，按 hosts 的四件套样板做。

type ClusterListHandler struct{ DB *sql.DB }

func NewClusterListHandler(db *sql.DB) *ClusterListHandler { return &ClusterListHandler{DB: db} }

func (h *ClusterListHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/cluster-list", h.List)
}

// clusterOut 集群一行。
//
// 每个数字都要能回答"为什么它是这个值"，所以采集缺口一律用指针表达 null，
// 而不是 0 —— 0 在这里全是**有意义的正常值**（0 个节点 = 集群空着），
// 与"没采到"混在一起就再也分不开了。
type clusterOut struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Environment string `json:"environment"`
	Provider    string `json:"provider"`
	Location    string `json:"location"`
	ProjectID   string `json:"project_id"`
	Enabled     bool   `json:"enabled"`

	// Ingested 集群侧的资源是否采进来了。
	//
	// ⚠️ false 时下面所有计数都是 null 而不是 0。
	// 「纳管了但没采到」和「采到了但里面是空的」在界面上必须长得不一样：
	// 前者是采集缺口（要去查连通性/凭据），后者什么都不用做。
	Ingested bool `json:"ingested"`

	Nodes      *int64 `json:"nodes"`
	NodesReady *int64 `json:"nodes_ready"`
	Pods       *int64 `json:"pods"`

	// PodsBad 起不来的：phase 不是 Running/Succeeded。
	//
	// ⚠️ **不含"重启过"**。判据必须和 Pod 列表的 health=bad 一字不差，
	// 否则同一批 Pod 在两页上有两个说法 —— 实测撞到过：
	// 开发机休眠导致 40 个 Pod 全都 restarts>0，集群页报"32 异常"，
	// 而 Pod 页显示 0 个 bad、32 个 restarted。看的人不知道该信哪个。
	PodsBad *int64 `json:"pods_bad"`

	// PodsRestarted 在跑但重启过。是**较弱的信号**，单列一个数：
	// 三周前重启过一次、之后一直好好的 Pod 不该被叫做"异常"。
	PodsRestarted *int64 `json:"pods_restarted"`

	// KubeletVersions 节点上的 kubelet 版本清单（去重）。
	// 多于一个说明集群正在升级中或升级卡住了 —— 这是升级排期要看的第一眼。
	KubeletVersions []string `json:"kubelet_versions"`

	// SyncedAt 最后一次采到数据的时刻。台账是快照不是实时，
	// 不显示它，人会以为看到的是此刻的集群
	SyncedAt string `json:"synced_at,omitempty"`

	// HasKubeconfig 有没有配连接凭据。没有就只能靠别的途径采，
	// 很多能力（实时日志、诊断）直接不可用
	HasKubeconfig bool `json:"has_kubeconfig"`
}

// List GET /api/k8s/cluster-list
//
//	@Summary		集群列表
//	@Description	含节点/Pod 计数、kubelet 版本分布与采集新鲜度。未采集的集群计数为 null 而非 0。
//	@Tags			k8s
//	@Produce		json
//	@Param			page	query		int		false	"页码，从 1 开始"
//	@Param			size	query		int		false	"每页条数"
//	@Param			q		query		string	false	"按集群名/展示名搜索"
//	@Param			env		query		string	false	"环境筛选，all 或任意环境值"
//
//	环境刻意**不标 Enums**：它在库里是自由字符串，客户完全可能定义
//	PRE / GRAY 之类的值。标成枚举会让生成的前端类型拒绝这些值，
//	而那时错的是我们的注解，不是客户的数据。
//	@Param			sort	query		string	false	"排序字段，前缀 - 为降序"	Enums(name, env, nodes, pods, synced, -name, -env, -nodes, -pods, -synced)
//	@Success		200		{object}	httpx.ListResponse[handlers.clusterOut]
//	@Router			/k8s/cluster-list [get]
func (h *ClusterListHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "env")

	rows, err := h.DB.Query(`SELECT c.id, c.name, COALESCE(c.display_name,''), c.environment,
		c.provider, COALESCE(c.location,''), COALESCE(c.project_id,''), c.enabled,
		(c.kubeconfig_enc IS NOT NULL AND c.kubeconfig_enc <> '') AS has_kc
		FROM k8s_clusters c ORDER BY c.id`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	all := []clusterOut{}
	for rows.Next() {
		var o clusterOut
		var enabled, hasKC int
		if err := rows.Scan(&o.ID, &o.Name, &o.DisplayName, &o.Environment, &o.Provider,
			&o.Location, &o.ProjectID, &enabled, &hasKC); err != nil {
			continue
		}
		o.Enabled = enabled == 1
		o.HasKubeconfig = hasKC == 1
		o.KubeletVersions = []string{}
		all = append(all, o)
	}

	h.fillCounts(all)

	items, total, facets := clusterPage(all, q)
	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(facets))
}

// fillCounts 批量补节点/Pod 计数。
//
// ⚠️ 一次查全部再在内存里归位，不要每个集群查一次：
// 集群数一多就是 N+1，而这个接口是首页级别的，慢下来最先被骂的是它。
func (h *ClusterListHandler) fillCounts(all []clusterOut) {
	if len(all) == 0 {
		return
	}
	type agg struct {
		nodes, ready, pods, bad, restarted int64
		versions                           map[string]bool
		syncedAt                           time.Time
		seen                               bool
	}
	byID := map[int]*agg{}
	for i := range all {
		byID[all[i].ID] = &agg{versions: map[string]bool{}}
	}

	if rows, err := h.DB.Query(`SELECT cluster_id, COUNT(*), SUM(ready_status='Ready'),
		MAX(synced_at), GROUP_CONCAT(DISTINCT kubelet_version) FROM k8s_nodes GROUP BY cluster_id`); err == nil {
		for rows.Next() {
			var cid int
			var n, ready sql.NullInt64
			var syncedAt sql.NullTime
			var vers sql.NullString
			if rows.Scan(&cid, &n, &ready, &syncedAt, &vers) != nil {
				continue
			}
			a, ok := byID[cid]
			if !ok {
				continue
			}
			a.seen = true
			a.nodes = n.Int64
			a.ready = ready.Int64
			if syncedAt.Valid {
				a.syncedAt = syncedAt.Time
			}
			for _, v := range strings.Split(vers.String, ",") {
				if v = strings.TrimSpace(v); v != "" {
					a.versions[v] = true
				}
			}
		}
		rows.Close()
	}

	// ⚠️ 判据必须与 Pod 列表的 podHealthCase **一字不差**：
	//	bad        phase 不是 Running/Succeeded —— 起不来
	//	restarted  在跑但重启过 —— 较弱的信号，单独一个数
	// 合成一个"异常"数的话，一台休眠过的开发机会报出几十条假故障。
	if rows, err := h.DB.Query(`SELECT cluster_id, COUNT(*),
		SUM(phase NOT IN ('Running','Succeeded')),
		SUM(phase IN ('Running','Succeeded') AND restarts > 0)
		FROM k8s_pods GROUP BY cluster_id`); err == nil {
		for rows.Next() {
			var cid int
			var n, bad, restarted sql.NullInt64
			if rows.Scan(&cid, &n, &bad, &restarted) != nil {
				continue
			}
			if a, ok := byID[cid]; ok {
				a.seen = true
				a.pods = n.Int64
				a.bad = bad.Int64
				a.restarted = restarted.Int64
			}
		}
		rows.Close()
	}

	for i := range all {
		a := byID[all[i].ID]
		if a == nil || !a.seen {
			// 采集缺口：计数保持 nil。这里**绝不能填 0** ——
			// 0 会让一个没采到的集群在界面上显示成"空集群"，
			// 而空集群是不需要任何人去处理的
			continue
		}
		all[i].Ingested = true
		nodes, ready, pods, bad, restarted := a.nodes, a.ready, a.pods, a.bad, a.restarted
		all[i].Nodes, all[i].NodesReady, all[i].Pods = &nodes, &ready, &pods
		all[i].PodsBad, all[i].PodsRestarted = &bad, &restarted
		if !a.syncedAt.IsZero() {
			all[i].SyncedAt = a.syncedAt.Format(time.RFC3339)
		}
		vs := make([]string, 0, len(a.versions))
		for v := range a.versions {
			vs = append(vs, v)
		}
		sort.Strings(vs)
		all[i].KubeletVersions = vs
	}
}

// clusterPage 纯函数：筛选 + 分面 + 排序 + 分页。
//
// 抽成纯函数是为了能测。集群数量是十几个量级，内存里处理完全够 ——
// 但**这一点必须写下来**：到几百个集群时要改成 SQL 分页，
// 否则后人照抄这个样板去做 Pod 列表，那可是十万级。
func clusterPage(all []clusterOut, q httpx.PageQuery) ([]clusterOut, int64, map[string]map[string]int64) {
	// 分面按「不含该维度自身的筛选」统计：选了 PROD 之后，
	// 下拉里 UAT 仍要显示它的真实条数，否则用户看不出"切过去还有多少"
	facets := map[string]map[string]int64{"env": {}}
	for _, cl := range all {
		if kw := q.Keyword; kw != "" && !matchCluster(cl, kw) {
			continue
		}
		facets["env"][cl.Environment]++
		facets["env"]["all"]++
	}

	env := q.Filters["env"]
	filtered := make([]clusterOut, 0, len(all))
	for _, cl := range all {
		if kw := q.Keyword; kw != "" && !matchCluster(cl, kw) {
			continue
		}
		if env != "" && env != "all" && cl.Environment != env {
			continue
		}
		filtered = append(filtered, cl)
	}

	desc, field := q.SortDesc, q.SortBy
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		less := false
		switch field {
		case "env":
			less = a.Environment < b.Environment
		case "nodes":
			less = deref(a.Nodes) < deref(b.Nodes)
		case "pods":
			less = deref(a.Pods) < deref(b.Pods)
		case "synced":
			less = a.SyncedAt < b.SyncedAt
		default: // name
			less = a.Name < b.Name
		}
		if desc {
			return !less
		}
		return less
	})

	total := int64(len(filtered))
	lo := min(q.Offset(), len(filtered))
	hi := min(lo+q.Size, len(filtered))
	return filtered[lo:hi], total, facets
}

// deref nil 视为 -1 而不是 0：按节点数排序时，"没采到"应当排在
// "0 个节点"之外，否则一堆采集失败的集群会混在真正空着的集群里
func deref(p *int64) int64 {
	if p == nil {
		return -1
	}
	return *p
}

func matchCluster(cl clusterOut, kw string) bool {
	kw = strings.ToLower(kw)
	return strings.Contains(strings.ToLower(cl.Name), kw) ||
		strings.Contains(strings.ToLower(cl.DisplayName), kw) ||
		strings.Contains(strings.ToLower(cl.ProjectID), kw)
}

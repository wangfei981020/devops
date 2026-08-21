package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
	"ops-cmdb-backend/logx"
)

// 负载均衡。
//
// ⚠️ 这一页有过一次真实事故：界面上 8 条 LB 显示"后端全空"，
// 而它们其实一直在正常服务 —— 真因是后端那条查询因排序规则冲突
// 从来没成功过，一行都没采到，而"没采到"被渲染成了"确实是 0"。
//
// 所以这里的后端数**必须三态**：
//	null  这个项目的 LB 后端压根没采过 → 我们不知道
//	0     采过了，确实一个后端都没有 → 这条 LB 打不通，是真问题
//	n     正常

type LBListHandler struct{ DB *sql.DB }

func NewLBListHandler(db *sql.DB) *LBListHandler { return &LBListHandler{DB: db} }

func (h *LBListHandler) Register(r *gin.RouterGroup) {
	r.GET("/cloud-lb-list", h.List)
}

type lbOut struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Project  string `json:"project"`
	Region   string `json:"region"`
	Scheme   string `json:"scheme"`
	VIP      string `json:"vip"`
	Ports    string `json:"port_range"`
	Protocol string `json:"protocol"`
	Target   string `json:"target"`
	Provider string `json:"provider"`

	// Backends 见文件头：null / 0 / n 是三件不同的事
	Backends *int64 `json:"backends"`
	// BackendState 云上自报的健康状态，原样透传
	BackendState string `json:"backend_state"`
	// K8sService 命中的 K8s Service（"集群 · 命名空间/服务名"），空 = 没对上。
	//
	//	⚠️ 这个字段原来只有 /api/cloud-lb（network_resources.go）有，
	//	这个列表接口没有 —— 于是**同一批 LB 在两个接口下判定能力不一样**：
	//	那边能认出「后端是 K8s Pod/NEG」，这边只能报「0 个后端」。
	//	而前端 LB 列表页用的正是这个接口。
	K8sService string `json:"k8s_service,omitempty"`

	Stale    bool   `json:"stale"`
	SyncedAt string `json:"synced_at,omitempty"`
}

// lbHealth 归档。
//
//	stale      云上已删
//	unknown    后端没采过、或上游某一跳拉失败了 —— **不是**"没有后端"
//	viaTarget  流量由 target instance / target proxy 承载（FortiGate 转发规则、Gateway API）
//	k8s        后端是 K8s Pod/NEG（GKE 的 Service type=LoadBalancer），实例组里看不到
//	lost       采集时追溯到了、读出来没有 → 数据丢了，这个结论不可信
//	empty      target 为空、确认一个后端都没有：这条 LB 打不通
//	ok
//
// # 🔴 必须读 BackendState，不能只看条数
//
//	原来的判据只有「Backends == 0 → empty」。生产 49 条里有 37 条命中
//	（33 条 fgt-* 的 FortiGate 转发规则 + 4 条 gkegw1-* 的 Gateway API），
//	界面因此红字报「37 个确认无后端」，而**真正打不通的是 0 条**
//	（OPSCMDB-031 P0-8）。
//
//	⚠️ 判据线索：**集中的异常分布先怀疑判据**。
//	37 条"故障"里 33 条同名前缀、指向同一个 target，这种分布不可能是真实故障。
//
// ⚠️ 前端 `routes/lbs/queries.ts` 里有一份**同样的**判定（用于渲染每一行）。
//
//	两处必须给出一致的结论。这里是权威：facets 计数和 health 筛选都走它，
//	前端那份只是为了不用为每一行多发一次请求。改判据必须同时改两处 ——
//	分叉的话，筛选选「无后端」筛出 37 条、而每行显示的却是「由 target 承载」。
func lbHealth(l lbOut) string {
	if l.Stale {
		return "stale"
	}
	// 状态优先于条数：条数为 0 有好几种完全不同的原因，
	// 而它们的下一步动作从"什么都不用做"到"这条 LB 打不通"横跨整个范围
	//
	// ⚠️ 这一列有**两套取值**，实测才发现：
	//
	//	采集侧（cloudsource/gcp_lb_backends.go）写的是小写的**追溯结果**：
	//	  ok / none / unsupported / unresolved
	//	读取侧（network_resources.go）还会加工出 lost / k8s。
	//
	//	而库里也存在大写的**云原生健康度**：HEALTHY / UNHEALTHY
	//	—— 那回答的是"后端健不健康"，不是"有没有追溯到后端"，是另一个维度。
	//
	// 两套混在一列里，所以必须显式分开处理，绝不能让不认识的值
	// 静默落到"只看条数"那条路上 —— 那正是 P0-8 的老路。
	switch l.BackendState {
	case "unresolved":
		return "unknown"
	case "unsupported":
		return "viaTarget"
	case "k8s":
		return "k8s"
	case "lost":
		return "lost"
	case "ok", "none", "":
		// 追溯结果里的正常态：继续按条数判断（下面）
	default:
		// 大写的云原生健康度，或上游新增的取值。
		//
		// ⚠️ 云说 HEALTHY 而我们数到 0 个后端时，**两个结论互相矛盾**，
		// 这时唯一诚实的答案是"不知道"，不能说"打不通" ——
		// 后者会让人去查一个云上认为健康的 LB。
		if strings.EqualFold(l.BackendState, "healthy") && (l.Backends == nil || *l.Backends == 0) {
			return "unknown"
		}
		// UNHEALTHY 等其它取值：落到条数判断，条数是我们自己数的，更可信
	}
	if l.Backends == nil {
		return "unknown"
	}
	if *l.Backends == 0 {
		return "empty"
	}
	return "ok"
}

// knownBackendStates 追溯结果的封闭取值集，用于识别"上游新增了取值"。
//
// ⚠️ 单独列出来是为了能在启动时自检（见 checkBackendStates）：
// 一个不认识的状态值会让 lbHealth 退回只看条数，
// 而那正是 P0-8 的失效模式 —— 必须让它**吵**出来，不能静默。
var knownBackendStates = map[string]bool{
	"": true, "ok": true, "none": true, "unsupported": true, "unresolved": true,
	"lost": true, "k8s": true,
	// 云原生健康度（大写），已在 lbHealth 里显式处理
	"HEALTHY": true, "UNHEALTHY": true, "DRAINING": true,
}

// CheckBackendStates 扫库里实际出现过的 backend_state，遇到不认识的记 WARN。
//
//	未识别的枚举值必须 WARN —— 否则判定悄悄退化，而界面看着一切正常
//	（本项目的约定：不让人查库猜）。
func (h *LBListHandler) CheckBackendStates() {
	rows, err := h.DB.Query(`SELECT DISTINCT COALESCE(backend_state,'') FROM cloud_loadbalancers`)
	if err != nil {
		return
	}
	defer rows.Close()
	unknown := []string{}
	for rows.Next() {
		var st string
		if rows.Scan(&st) == nil && !knownBackendStates[st] {
			unknown = append(unknown, st)
		}
	}
	if len(unknown) > 0 {
		logx.J("lbs", "unknown_backend_state", map[string]any{
			"values": unknown,
			"note": "这些 backend_state 取值 lbHealth 不认识，会退回只看后端条数判断 —— " +
				"而那正是「37 条正常 LB 被报成无后端」(OPSCMDB-031 P0-8) 的失效模式。请补进 lbHealth 的 switch",
		})
	}
}

// List GET /api/cloud-lb-list
//
//	@Summary		负载均衡列表
//	@Description	后端数分三态：null=没采过、0=看 backend_state 判断原因（可能由 target/K8s 承载，也可能真的没有）、n=正常。
//	@Tags			cloud
//	@Produce		json
//	@Param			page	query		int		false	"页码，从 1 开始"
//	@Param			size	query		int		false	"每页条数"
//	@Param			q		query		string	false	"按名称/VIP 搜索"
//	@Param			scheme	query		string	false	"EXTERNAL / INTERNAL，all 或具体值"
//	@Param			health	query		string	false	"健康度"	Enums(all, stale, unknown, viaTarget, k8s, lost, empty, ok)
//	@Success		200		{object}	httpx.ListResponse[handlers.lbOut]
//	@Router			/cloud-lb-list [get]
func (h *LBListHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "scheme", "health")

	rows, err := h.DB.Query(`SELECT l.id, l.name, l.project, COALESCE(l.region,''),
		COALESCE(l.scheme,''), COALESCE(l.vip,''), COALESCE(l.port_range,''),
		COALESCE(l.protocol,''), COALESCE(l.target,''), l.provider,
		COALESCE(l.backend_state,''), l.stale, l.synced_at
		FROM cloud_loadbalancers l ORDER BY l.project, l.name`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	all := []lbOut{}
	for rows.Next() {
		var o lbOut
		var stale int
		var synced sql.NullTime
		if err := rows.Scan(&o.ID, &o.Name, &o.Project, &o.Region, &o.Scheme, &o.VIP,
			&o.Ports, &o.Protocol, &o.Target, &o.Provider, &o.BackendState, &stale, &synced); err != nil {
			continue
		}
		o.Stale = stale == 1
		if synced.Valid {
			o.SyncedAt = synced.Time.Format(time.RFC3339)
		}
		all = append(all, o)
	}

	h.fillBackends(all)
	// ⚠️ 判 empty 之前先拿 VIP 对一次 K8s Service。
	//
	//	GKE 的 Service type=LoadBalancer 后端是 Pod（NEG），不在实例组里，
	//	实例组这条追溯路径天然看不到 —— 但它**有后端而且在服务**。
	//	实测生产 8 条命中 8 条，全是 UAT 在跑的 Kafka / ZK / RocketMQ / Istio 内网入口。
	h.fillK8sService(all)
	items, total, facets := lbPage(all, q)
	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(facets))
}

// fillBackends 补后端数。
//
// ⚠️ 判据抬到**项目**层：这个项目只要采到过任意一条后端记录，
// 就说明那条查询是通的，于是某条 LB 没有记录就是真的没有后端。
// 整个项目一条都没有 —— 那更可能是采集本身坏了（历史上就是这么坏的），
// 这时必须显示"未知"，而不是给 8 条正在服务的 LB 打上"无后端"。
func (h *LBListHandler) fillBackends(all []lbOut) {
	if len(all) == 0 {
		return
	}
	type key struct{ project, lb string }
	counts := map[key]int64{}
	seenProjects := map[string]bool{}
	if rows, err := h.DB.Query(
		`SELECT project, lb_name, COUNT(*) FROM cloud_lb_backends GROUP BY project, lb_name`); err == nil {
		for rows.Next() {
			var p, lb string
			var n int64
			if rows.Scan(&p, &lb, &n) == nil {
				counts[key{p, lb}] = n
				seenProjects[p] = true
			}
		}
		rows.Close()
	}
	for i := range all {
		if n, ok := counts[key{all[i].Project, all[i].Name}]; ok {
			v := n
			all[i].Backends = &v
			continue
		}
		if seenProjects[all[i].Project] {
			zero := int64(0)
			all[i].Backends = &zero
		}
		// 该项目一条后端都没采过 → 保持 nil（未知）
	}
}

// fillK8sService 用 VIP 反查 K8s Service，命中的把 BackendState 标成 k8s。
//
//	复用 NetworkHandler.lbVIPToK8sService —— **不另写一份查询**：
//	两处各查一次的话，两个接口对同一条 LB 会给出不同结论，
//	而那种不一致最难查（一个页面说有后端、另一个说没有）。
func (h *LBListHandler) fillK8sService(all []lbOut) {
	svcOfVIP := (&NetworkHandler{DB: h.DB}).lbVIPToK8sService()
	if len(svcOfVIP) == 0 {
		return
	}
	for i := range all {
		svc := svcOfVIP[all[i].VIP]
		if svc == "" {
			continue
		}
		all[i].K8sService = svc
		// 只在"看起来没后端"时才改状态：已经追溯到实例后端的不动它
		if all[i].BackendState == "none" || all[i].BackendState == "unsupported" || all[i].BackendState == "" {
			if all[i].Backends == nil || *all[i].Backends == 0 {
				all[i].BackendState = "k8s"
			}
		}
	}
}

func lbPage(all []lbOut, q httpx.PageQuery) ([]lbOut, int64, map[string]map[string]int64) {
	match := func(l lbOut) bool {
		if q.Keyword == "" {
			return true
		}
		kw := strings.ToLower(q.Keyword)
		return strings.Contains(strings.ToLower(l.Name), kw) ||
			strings.Contains(strings.ToLower(l.VIP), kw)
	}
	scheme, health := q.Filters["scheme"], q.Filters["health"]
	facets := map[string]map[string]int64{"scheme": {}, "health": {}}
	for _, l := range all {
		if !match(l) {
			continue
		}
		if scheme == "" || scheme == "all" || l.Scheme == scheme {
			facets["health"][lbHealth(l)]++
			facets["health"]["all"]++
		}
		if health == "" || health == "all" || lbHealth(l) == health {
			facets["scheme"][l.Scheme]++
			facets["scheme"]["all"]++
		}
	}

	filtered := make([]lbOut, 0, len(all))
	for _, l := range all {
		if !match(l) {
			continue
		}
		if scheme != "" && scheme != "all" && l.Scheme != scheme {
			continue
		}
		if health != "" && health != "all" && lbHealth(l) != health {
			continue
		}
		filtered = append(filtered, l)
	}

	// 排序按"该不该现在看"给权重。
	// ⚠️ viaTarget / k8s 是**正常形态**，必须和 ok 同级排在最后 ——
	// 把它们排到前面，等于把 37 条正常 LB 顶到运维眼前，真问题反而被挤下去
	sev := func(l lbOut) int {
		switch lbHealth(l) {
		case "empty":
			return 0 // target 为空、确认打不通，最该看
		case "lost":
			return 1 // 数据丢了，这条的结论不可信
		case "unknown":
			return 2 // 我们不知道
		case "stale":
			return 3
		default:
			return 4 // ok / viaTarget / k8s —— 都是正常
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		var less bool
		switch q.SortBy {
		case "name":
			less = a.Name < b.Name
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

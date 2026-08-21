package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
	"ops-cmdb-backend/internal/store"
)

// 成本总览。
//
// ⚠️ 这一页显示的全是**估算**，不是账单。
// 单价来自我们维护的费率表，机型/磁盘对不上时会回退到 default ——
// 所以每一个数字旁边都必须写明"估算"，并给出**有多少资源没能匹配到费率**。
// 不写的话，客户会拿它和云账单对，对不上就来问，而我们答不上来差在哪。

type CostOverviewHandler struct {
	// Store 走租户隔离：费率表是有租户语义的
	Store *store.Store
	DB    *sql.DB
}

func NewCostOverviewHandler(st *store.Store, db *sql.DB) *CostOverviewHandler {
	return &CostOverviewHandler{Store: st, DB: db}
}

func (h *CostOverviewHandler) Register(r *gin.RouterGroup) {
	r.GET("/cost/overview", h.Overview)
}

type costRow struct {
	Key     string  `json:"key"`
	Monthly float64 `json:"monthly"`
	Count   int64   `json:"count"`
}

type costOverviewOut struct {
	// TotalMonthly 估算月成本
	TotalMonthly float64 `json:"total_monthly"`
	// Estimated 恒为 true：这一页永远是估算，字段留着是为了将来接账单后能翻成 false
	Estimated bool `json:"estimated"`

	ByProject []costRow `json:"by_project"`
	ByEnv     []costRow `json:"by_env"`

	// Unpriced 没能匹配到费率、按 0 计的资源数。
	//
	// ⚠️ 必须显式给出。它们不是"免费的"，而是我们**算不出来**的 ——
	// 混进总数里会让总额偏低，而偏低的成本报表没人会去质疑。
	UnpricedHosts int64 `json:"unpriced_hosts"`

	// FallbackPricedHosts 按**默认档**估价的机器数。
	//
	//	⚠️ 这是这一页真正的静默降级，比 UnpricedHosts 隐蔽得多：
	//	算不出来（0 元）很醒目，而回退默认档会算出一个看起来正常的数。
	//	原来它被记成"已定价"，界面上没有任何痕迹（OPSCMDB-031 P1-47）。
	FallbackPricedHosts int64 `json:"fallback_priced_hosts"`
	// FallbackRegions 缺费率的区域（最多 8 个）。
	// 光说"有 3 台按默认档估"没法行动，要知道去给哪个区域补费率
	FallbackRegions []string `json:"fallback_regions"`

	// DestroyedExcluded 已销毁但仍在台账里的机器数（不计入成本）。
	// 说明它们为什么不在总额里，否则会被当成漏算
	DestroyedExcluded int64 `json:"destroyed_excluded"`
}

// Overview GET /api/cost/overview
//
//	@Summary		成本总览
//	@Description	**估算**月成本，按项目/环境拆分。明确给出没匹配到费率的资源数。
//	@Tags			cost
//	@Produce		json
//	@Success		200	{object}	handlers.costOverviewOut
//	@Router			/cost/overview [get]
func (h *CostOverviewHandler) Overview(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	out := costOverviewOut{Estimated: true, ByProject: []costRow{}, ByEnv: []costRow{}, FallbackRegions: []string{}}

	rc := newRateCache(sc)

	rows, err := h.DB.Query(`SELECT c.project, COALESCE(c.env,''), h.region, h.machine_type,
		h.vcpu, h.mem_mb, h.status, h.stale, h.ci_id
		FROM hosts h JOIN cis c ON c.id = h.ci_id WHERE c.type='host'`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	// 磁盘一次查完再归位，避免 N+1
	disks := map[int][]diskRow{}
	if drows, e := h.DB.Query(`SELECT host_ci_id, type, size_gb FROM host_disks`); e == nil {
		for drows.Next() {
			var id int
			var d diskRow
			if drows.Scan(&id, &d.Type, &d.SizeGB) == nil {
				disks[id] = append(disks[id], d)
			}
		}
		drows.Close()
	}

	byProject := map[string]*costRow{}
	byEnv := map[string]*costRow{}
	for rows.Next() {
		var project, env, region, machineType, status string
		var vcpu, memMB, ciID int
		var stale int
		if rows.Scan(&project, &env, &region, &machineType, &vcpu, &memMB, &status, &stale, &ciID) != nil {
			continue
		}
		if stale == 1 {
			// 已销毁的不计入成本 —— 它已经不产生费用了。
			// 但要数出来告诉用户，否则台账里有 65 台机器、成本却只算了 60 台，
			// 看起来像漏算
			out.DestroyedExcluded++
			continue
		}
		family := familyOf(machineType)
		hourly, _, _, matched := rc.hostHourly(region, family, vcpu, memMB, status, disks[ciID])
		switch matched {
		case "无":
			// 费率表里既没有精确匹配也没有 default —— 这台机器我们算不出来。
			// 它按 0 计进了总额，所以必须单独数出来
			out.UnpricedHosts++
		case "default":
			// 🔴 **回退到默认档**才是这里真正的静默降级。
			//
			//	它比"算成 0"隐蔽得多：算成 0 很醒目（一台机器不花钱，一眼能看出来），
			//	而回退会用一个**可能不对的价格**算出一个**看起来完全正常的数**。
			//
			//	实测：`fgt-1-eu-west3` 在 europe-west3，费率表里没有这个区域，
			//	按 default 档估出 141.05/月 —— 而 europe-west3 的真实价格
			//	与默认档差多少，无人知晓。原来 `unpriced_hosts` 是 0，
			//	**回退被记成了"已定价"**（OPSCMDB-031 P1-47）。
			out.FallbackPricedHosts++
			// 把区域列出来：知道"有 3 台按默认档估"还不够，
			// 要知道是哪些区域缺费率才能去补。
			//
			// ⚠️ region 可能是空的（实测就有一台）。空串直接塞进去，
			// 界面上会渲染成「缺费率的区域：」后面什么都没有 —— 一个更费解的提示。
			// 空要显式说成"未记录区域"：那本身就是个要查的问题
			// （主机采集没拿到 region，费率自然永远匹配不上）。
			r := region
			if strings.TrimSpace(r) == "" {
				r = "(未记录区域)"
			}
			if len(out.FallbackRegions) < 8 && !containsStr(out.FallbackRegions, r) {
				out.FallbackRegions = append(out.FallbackRegions, r)
			}
		}
		m := round2(hourly * 730)
		out.TotalMonthly += m
		if byProject[project] == nil {
			byProject[project] = &costRow{Key: project}
		}
		byProject[project].Monthly += m
		byProject[project].Count++
		if byEnv[env] == nil {
			byEnv[env] = &costRow{Key: env}
		}
		byEnv[env].Monthly += m
		byEnv[env].Count++
	}
	out.TotalMonthly = round2(out.TotalMonthly)

	for _, m := range byProject {
		m.Monthly = round2(m.Monthly)
		out.ByProject = append(out.ByProject, *m)
	}
	for _, m := range byEnv {
		m.Monthly = round2(m.Monthly)
		out.ByEnv = append(out.ByEnv, *m)
	}
	sort.Slice(out.ByProject, func(i, j int) bool { return out.ByProject[i].Monthly > out.ByProject[j].Monthly })
	sort.Slice(out.ByEnv, func(i, j int) bool { return out.ByEnv[i].Monthly > out.ByEnv[j].Monthly })

	c.JSON(http.StatusOK, out)
}

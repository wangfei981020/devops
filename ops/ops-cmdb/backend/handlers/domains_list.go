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

// 域名。
//
// 一个域名上有**三个互相独立的到期/健康维度**，最容易被搅在一起：
//	注册到期    域名本身的续费，过了就被别人抢注
//	解析状态    DNS 解析通不通
//	证书到期    挂在这个域名上的证书
// 三者任一出问题都会让站点不可用，但**处理的人和动作完全不同**
// （去注册商续费 / 查 DNS / 重签证书）。所以三列分开，不合成一个"健康"。

type DomainListHandler struct{ DB *sql.DB }

func NewDomainListHandler(db *sql.DB) *DomainListHandler { return &DomainListHandler{DB: db} }

func (h *DomainListHandler) Register(r *gin.RouterGroup) {
	r.GET("/domain-list", h.List)
}

// domainRowOut 与旧 handler 里的 domainRowOut 同名会冲突，
// 而那份是老接口的出参形状 —— 重构期两套并存，各用各的类型，
// 不要为了名字好看去动还在跑的那份。
type domainRowOut struct {
	CIID        int    `json:"ci_id"`
	Name        string `json:"name"`
	Registrar   string `json:"registrar"`
	DNSProvider string `json:"dns_provider"`

	// ⚠️ 两个到期天数都用指针：null = 不知道（没登记 / 没探测到），
	// 不是"还有 0 天"。域名注册到期读不出来时，它可能下周就被释放了。
	DaysLeft     *int   `json:"days_left"`
	ExpiryAt     string `json:"expiry_at,omitempty"`
	CertDaysLeft *int   `json:"cert_days_left"`
	CertExpiryAt string `json:"cert_expiry_at,omitempty"`
	// CertCheckMsg 证书探测的错误原因，原样透传
	CertCheckMsg string `json:"cert_check_msg"`

	// ResolveStatus 解析状态，原样透传（ok / nxdomain / timeout …）
	ResolveStatus string `json:"resolve_status"`

	// Ignored 人为忽略。
	//
	// ⚠️ 忽略的域名**不能当成正常**：它只是"我们决定暂时不管"，
	// 客观状态一点没变。所以它单独一档，并把理由带出来 ——
	// 半年后没人记得当初为什么忽略，而那条理由往往已经不成立了。
	Ignored      bool   `json:"ignored"`
	IgnoreReason string `json:"ignore_reason"`

	// Records 主机头台账（domain_records）的条数 —— 「这个域名下我们登记了几条业务解析」。
	Records int64 `json:"records"`
	// DNSRecords 注册商侧真实解析记录（dns_records）的条数。
	//
	// 🔴 与 Records 是**两张表、两件事**，绝不能互相冒充：
	//	domain_records = 我们自己的台账（带 project/env/module/负责人）
	//	dns_records    = 注册商上此刻真实存在的解析
	//	实测 dev-example.com：台账 2 条、注册商侧 0 条。
	//	「DNS 解析」页按域名视图里那个计数必须是后者 ——
	//	写成前者的话，点开弹窗（管的是注册商解析）会看到"没有解析记录"，
	//	而行上明明写着「2 条记录」。数字和它旁边的按钮说的不是一回事。
	DNSRecords int64  `json:"dns_records"`
	SyncedAt   string `json:"synced_at,omitempty"`
}

const domainExpirySoonDays = 30

// domainHealth 归档。注册到期优先于证书 —— 域名没了，证书再新也没用。
func domainHealth(d domainRowOut) string {
	if d.Ignored {
		return "ignored"
	}
	if d.DaysLeft != nil && *d.DaysLeft < 0 {
		return "expired"
	}
	if d.ResolveStatus != "" && d.ResolveStatus != "ok" {
		return "unresolved"
	}
	if d.DaysLeft == nil {
		return "unknown"
	}
	if *d.DaysLeft <= domainExpirySoonDays {
		return "soon"
	}
	if d.CertDaysLeft != nil && *d.CertDaysLeft <= domainExpirySoonDays {
		return "cert_soon"
	}
	return "ok"
}

// List GET /api/domain-list
//
//	@Summary		域名列表
//	@Description	注册到期、解析状态、证书到期三个维度分开显示（处理动作完全不同）。
//	@Tags			domains
//	@Produce		json
//	@Param			page	query		int		false	"页码，从 1 开始"
//	@Param			size	query		int		false	"每页条数"
//	@Param			q		query		string	false	"按域名搜索"
//	@Param			health	query		string	false	"健康度"	Enums(all, expired, unresolved, unknown, soon, cert_soon, ignored, ok)
//	@Success		200		{object}	httpx.ListResponse[handlers.domainRowOut]
//	@Router			/domain-list [get]
func (h *DomainListHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "health")

	rows, err := h.DB.Query(`SELECT ci.id, ci.name, COALESCE(r.name,''), COALESCE(d.dns_provider,''),
		d.expiry_at, d.cert_expiry_at, COALESCE(d.cert_check_msg,''),
		COALESCE(d.resolve_status,''), d.ignored, COALESCE(d.ignore_reason,''), d.last_synced_at
		FROM domains d JOIN cis ci ON ci.id = d.ci_id
		LEFT JOIN registrars r ON r.id = d.registrar_id
		ORDER BY ci.name`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	now := time.Now()
	all := []domainRowOut{}
	for rows.Next() {
		var o domainRowOut
		var exp, certExp, synced sql.NullTime
		var ignored int
		if err := rows.Scan(&o.CIID, &o.Name, &o.Registrar, &o.DNSProvider, &exp, &certExp,
			&o.CertCheckMsg, &o.ResolveStatus, &ignored, &o.IgnoreReason, &synced); err != nil {
			continue
		}
		o.Ignored = ignored == 1
		if exp.Valid {
			o.ExpiryAt = exp.Time.Format("2006-01-02")
			d := int(exp.Time.Sub(now).Hours() / 24)
			o.DaysLeft = &d
		}
		if certExp.Valid {
			o.CertExpiryAt = certExp.Time.Format("2006-01-02")
			d := int(certExp.Time.Sub(now).Hours() / 24)
			o.CertDaysLeft = &d
		}
		if synced.Valid {
			o.SyncedAt = synced.Time.Format(time.RFC3339)
		}
		all = append(all, o)
	}

	h.fillRecordCounts(all)
	items, total, facets := domainPage(all, q)
	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(facets))
}

func (h *DomainListHandler) fillRecordCounts(all []domainRowOut) {
	if len(all) == 0 {
		return
	}
	// ⚠️ 两张表分别数，不要合并成一个数字。
	//	domain_records = 我们的台账；dns_records = 注册商上真实存在的解析。
	//	合成一个的话，「DNS 解析」页那个计数会和它旁边的弹窗对不上（实测栽过）。
	load := func(table string) map[int]int64 {
		m := map[int]int64{}
		rows, err := h.DB.Query(`SELECT domain_ci_id, COUNT(*) FROM ` + table + ` GROUP BY domain_ci_id`)
		if err != nil {
			return m
		}
		defer rows.Close()
		for rows.Next() {
			var id int
			var n int64
			if rows.Scan(&id, &n) == nil {
				m[id] = n
			}
		}
		return m
	}
	ledger := load("domain_records")
	dns := load("dns_records")
	for i := range all {
		all[i].Records = ledger[all[i].CIID]
		all[i].DNSRecords = dns[all[i].CIID]
	}
}

func domainPage(all []domainRowOut, q httpx.PageQuery) ([]domainRowOut, int64, map[string]map[string]int64) {
	match := func(d domainRowOut) bool {
		return q.Keyword == "" || strings.Contains(strings.ToLower(d.Name), strings.ToLower(q.Keyword))
	}
	health := q.Filters["health"]
	facets := map[string]map[string]int64{"health": {}}
	for _, d := range all {
		if !match(d) {
			continue
		}
		facets["health"][domainHealth(d)]++
		facets["health"]["all"]++
	}

	filtered := make([]domainRowOut, 0, len(all))
	for _, d := range all {
		if !match(d) {
			continue
		}
		if health != "" && health != "all" && domainHealth(d) != health {
			continue
		}
		filtered = append(filtered, d)
	}

	sev := func(d domainRowOut) int {
		switch domainHealth(d) {
		case "expired":
			return 0
		case "unresolved":
			return 1
		case "unknown":
			return 2 // 读不出注册到期日：它可能下周就被释放
		case "soon":
			return 3
		case "cert_soon":
			return 4
		case "ignored":
			return 5 // 排在正常前面：被忽略不等于没问题
		default:
			return 6
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

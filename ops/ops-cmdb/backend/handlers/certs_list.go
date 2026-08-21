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

// 证书列表。
//
// ⚠️⚠️ 这个接口**绝不返回 cert_pem / chain_pem / key_pem_enc**。
//
// 上一代在证书接口上出过 P0：列表把私钥一起发给了所有有读权限的人。
// 私钥泄露不会有任何报错，也不影响任何功能 —— 它只是安静地躺在
// 每个人的浏览器 devtools 里，直到有人注意到。
// 所以这里用**显式字段清单**而不是 `SELECT *`：加字段必须有人手写一行，
// 顺手加进来的字段不会自己跑到响应里。

type CertListHandler struct{ DB *sql.DB }

func NewCertListHandler(db *sql.DB) *CertListHandler { return &CertListHandler{DB: db} }

func (h *CertListHandler) Register(r *gin.RouterGroup) {
	r.GET("/cert-list", h.List)
}

// certExpirySoonDays 多少天内到期算"即将到期"。
//
// 取 30：Let's Encrypt 的续期窗口是到期前 30 天，签发方通常也按这个节奏提醒。
// 比它更短，人还没来得及走采购流程；更长则天天都在提醒，提醒就失效了。
const certExpirySoonDays = 30

type certOut struct {
	CIID   int    `json:"ci_id"`
	CN     string `json:"cn"`
	SANs   string `json:"sans"`
	CA     string `json:"ca"`
	Status string `json:"status"`

	// ExpiryAt / DaysLeft
	//
	// ⚠️ DaysLeft 用指针：null = **采不到到期日**，不是"还有 0 天"。
	// 一张读不出到期日的证书绝不能显示成正常 —— 它可能已经过期了，
	// 只是我们不知道。0 是"今天到期"，是完全不同的事。
	ExpiryAt string `json:"expiry_at,omitempty"`
	DaysLeft *int   `json:"days_left"`

	AutoRenew bool `json:"auto_renew"`

	// LastError 上一次续期的错误。
	//
	// ⚠️ 这一条和 AutoRenew 必须**一起看**。最危险的组合是
	// 「自动续期开着 + 一直在失败」：界面上写着"自动续期"会让人放心，
	// 而它其实已经连续失败几周了，没有任何人在管。
	LastError string `json:"last_error"`

	UpdatedAt string `json:"updated_at,omitempty"`
}

// certHealth 归档，供筛选与排序用。
//
//	expired    已过期
//	failing    续期失败（不论还剩几天 —— 它正在失去自我修复能力）
//	unknown    读不出到期日，**不是正常**
//	soon       30 天内到期
//	ok
func certHealth(c certOut, now time.Time) string {
	if c.DaysLeft == nil {
		return "unknown"
	}
	if *c.DaysLeft < 0 {
		return "expired"
	}
	if c.LastError != "" {
		return "failing"
	}
	if *c.DaysLeft <= certExpirySoonDays {
		return "soon"
	}
	return "ok"
}

// List GET /api/cert-list
//
//	@Summary		证书列表
//	@Description	到期倒计时、自动续期与上次续期错误。**不返回证书内容与私钥。**
//	@Tags			certs
//	@Produce		json
//	@Param			page	query		int		false	"页码，从 1 开始"
//	@Param			size	query		int		false	"每页条数"
//	@Param			q		query		string	false	"按通用名/SAN 搜索"
//	@Param			health	query		string	false	"健康度"	Enums(all, expired, failing, unknown, soon, ok)
//	@Param			sort	query		string	false	"排序字段，前缀 - 为降序"	Enums(cn, expiry)
//	@Success		200		{object}	httpx.ListResponse[handlers.certOut]
//	@Router			/cert-list [get]
func (h *CertListHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "health")

	// 显式列清单 —— 见文件头那段注释，别改成 SELECT *
	rows, err := h.DB.Query(`SELECT ci.id, cr.cn, COALESCE(cr.sans,''), COALESCE(cr.ca,''),
		COALESCE(cr.status,''), cr.expiry_at, cr.auto_renew, COALESCE(cr.last_error,''), cr.updated_at
		FROM certificates cr JOIN cis ci ON ci.id = cr.ci_id
		ORDER BY cr.cn`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	now := time.Now()
	all := []certOut{}
	for rows.Next() {
		var o certOut
		var expiry, updated sql.NullTime
		var auto int
		if err := rows.Scan(&o.CIID, &o.CN, &o.SANs, &o.CA, &o.Status,
			&expiry, &auto, &o.LastError, &updated); err != nil {
			continue
		}
		o.AutoRenew = auto == 1
		if expiry.Valid {
			o.ExpiryAt = expiry.Time.Format("2006-01-02")
			d := int(expiry.Time.Sub(now).Hours() / 24)
			o.DaysLeft = &d
		}
		// expiry 为 NULL 时 DaysLeft 保持 nil —— 绝不能兜底成 0 或一个大数
		if updated.Valid {
			o.UpdatedAt = updated.Time.Format(time.RFC3339)
		}
		all = append(all, o)
	}

	items, total, facets := certPage(all, q, now)

	// 🔴 到期日整片为空时，必须说清为什么。
	//
	//	实测生产：890 张证书里 828 张没有到期日 —— 不是"还没采到"，
	//	是「证书到期检测（443）」这个定时任务**被停用且从没跑过**，
	//	也就是证书临期提醒根本不工作。而列表页上没有任何地方说这件事，
	//	看的人只会以为数据没同步。两张 8/21 到期的生产网关证书就这么活到了剩 28 小时。
	//
	//	⚠️ 判据用**全量**（all）不是当前页：翻到第 3 页恰好都有到期日，
	//	不代表这批数据可信。
	//	⚠️ 与巡检弹窗共用 certProbeVerdict，别在这里另写一套判词 ——
	//	同一件事两处说法不同，人会以为是两个问题。
	noExpiry := 0
	for _, c0 := range all {
		if c0.DaysLeft == nil {
			noExpiry++
		}
	}
	kind, noteKey, noteParams := probeVerdictDB(h.DB, noExpiry, 0)
	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(facets).WithCaveat(kind, noteKey, noteParams))
}

// certPage 纯函数：筛选 + 分面 + 排序 + 分页（证书是百量级，内存里够用）。
func certPage(all []certOut, q httpx.PageQuery, now time.Time) ([]certOut, int64, map[string]map[string]int64) {
	match := func(c certOut) bool {
		if q.Keyword == "" {
			return true
		}
		kw := strings.ToLower(q.Keyword)
		return strings.Contains(strings.ToLower(c.CN), kw) ||
			strings.Contains(strings.ToLower(c.SANs), kw)
	}

	facets := map[string]map[string]int64{"health": {}}
	for _, c := range all {
		if !match(c) {
			continue
		}
		facets["health"][certHealth(c, now)]++
		facets["health"]["all"]++
	}

	health := q.Filters["health"]
	filtered := make([]certOut, 0, len(all))
	for _, c := range all {
		if !match(c) {
			continue
		}
		if health != "" && health != "all" && certHealth(c, now) != health {
			continue
		}
		filtered = append(filtered, c)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		var less bool
		switch q.SortBy {
		case "cn":
			less = a.CN < b.CN
		case "expiry":
			less = certSeverity(a, now) < certSeverity(b, now)
		default:
			// 默认按严重度：已过期 → 续期失败 → 读不出到期日 → 快到期 → 正常。
			// 按通用名排序的话，唯一那张过期的证书会躺在字母表中间
			if s1, s2 := certSeverity(a, now), certSeverity(b, now); s1 != s2 {
				return s1 < s2
			}
			less = a.CN < b.CN
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

// certSeverity 越小越该被先看到。
//
// ⚠️ unknown 排在 soon **前面**：读不出到期日意味着我们对这张证书一无所知，
// 它完全可能已经过期了。把它排在"还有 20 天"后面，等于替一张看不见的证书担保。
func certSeverity(c certOut, now time.Time) int {
	switch certHealth(c, now) {
	case "expired":
		return 0
	case "failing":
		return 1
	case "unknown":
		return 2
	case "soon":
		return 3
	default:
		return 4
	}
}

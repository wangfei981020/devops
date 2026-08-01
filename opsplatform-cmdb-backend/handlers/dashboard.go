package handlers

import (
	"database/sql"
	"net/http"

	"opsplatform-cmdb-backend/logx"

	"github.com/gin-gonic/gin"
)

type DashboardHandler struct {
	DB *sql.DB
}

func NewDashboardHandler(db *sql.DB) *DashboardHandler { return &DashboardHandler{DB: db} }

func (h *DashboardHandler) Register(r *gin.RouterGroup) {
	r.GET("/dashboard", h.Get)
}

type expiringItem struct {
	Type     string `json:"type"` // certificate / domain
	Name     string `json:"name"`
	ExpiryAt string `json:"expiry_at"`
	Days     int    `json:"days"`
}

// Get 展示台聚合：资产计数 + 按环境分布 + 30 天内到期清单。
//
// ⚠️ 这里的每一个数字都会被当成结论使用（"证书没过期"="不用管"）。
// 所以查询失败**必须**返回 500，绝不能把失败降级成 0 —— 2026-07-31 生产 MySQL 盘写满时，
// 本接口对前端报告"一切正常、计数全 0"，把故障说成了没问题（CMDB-013）。
func (h *DashboardHandler) Get(c *gin.Context) {
	var firstErr error
	fail := func(what string, err error) {
		if firstErr == nil {
			firstErr = err
		}
		logx.J("dashboard", "query_fail", map[string]any{"item": what, "err": err.Error()})
	}
	count := func(what, q string, args ...any) int {
		var n int
		if err := h.DB.QueryRow(q, args...).Scan(&n); err != nil {
			fail(what, err)
			return 0
		}
		return n
	}

	resp := gin.H{
		"ci_total": count("ci_total", `SELECT COUNT(*) FROM cis`),
		// ⚠️ domain_total 是**注册域名**（顶级域），不是域名页那 700+ 行——
		// 那些是解析记录(domain_records)，两个是不同层级。展示台只写「域名」会被当成同一个数，
		// 所以这里同时给出 record_total，前端必须分别标注。
		"domain_total": count("domain_total", `SELECT COUNT(*) FROM cis WHERE type='domain'`),
		"record_total": count("record_total", `SELECT COUNT(*) FROM domain_records WHERE ignored=0`),
		"cert_total":   count("cert_total", `SELECT COUNT(*) FROM cis WHERE type='certificate'`),
		"cert_active":  count("cert_active", `SELECT COUNT(*) FROM certificates WHERE status='active'`),
		// cert_expired 只统计**我们自己管理**的证书（certificates 表）。
		// 它常年是 0，但线上实际在用的证书可能早已过期——那个数在 online_cert_expired 里。
		// 展示台若只显示这个 0，会让人直接认定证书没问题，是最危险的误导。
		"cert_expired": count("cert_expired", `SELECT COUNT(*) FROM certificates WHERE status='active' AND expiry_at < NOW()`),
		// 线上检测证书（解析记录），未忽略。
		// 口径统一用 DATEDIFF 天数，且与到期巡检页(CertInspect.vue)逐字对齐（CMDB-015）：
		//   快到期 = 0 <= days <= 30   已过期 = days < 0   两者互斥
		// 原先这里写的是 `>= NOW() AND < NOW()+30天`（秒级），前端写的是 `d < 30`（天级），
		// 边界上的项两边归类不同，于是同一指标两个页面给出不同的数。
		"online_cert_expiring": count("online_cert_expiring", `SELECT COUNT(*) FROM domain_records WHERE ignored=0 AND cert_ignored=0 AND cert_expiry_at IS NOT NULL AND DATEDIFF(cert_expiry_at, NOW()) BETWEEN 0 AND 30`),
		"online_cert_expired":  count("online_cert_expired", `SELECT COUNT(*) FROM domain_records WHERE ignored=0 AND cert_ignored=0 AND cert_expiry_at IS NOT NULL AND DATEDIFF(cert_expiry_at, NOW()) < 0`),
		"online_cert_failed":   count("online_cert_failed", `SELECT COUNT(*) FROM domain_records WHERE ignored=0 AND cert_ignored=0 AND cert_expiry_at IS NULL AND cert_check_msg<>''`),
	}

	// 按环境分布
	byEnv := map[string]int{}
	if rows, err := h.DB.Query(`SELECT COALESCE(NULLIF(env,''),'未分类'), COUNT(*) FROM cis GROUP BY env`); err != nil {
		fail("by_env", err)
	} else {
		for rows.Next() {
			var env string
			var n int
			if rows.Scan(&env, &n) == nil {
				byEnv[env] = n
			}
		}
		rows.Close()
	}
	resp["by_env"] = byEnv

	// 到期清单（证书 + 域名）。一次查出「已过期」和「30 天内到期」两段，在 Go 里按天数分开。
	//
	// ⚠️ CMDB-016：原先只写了上界 `< NOW()+30 天`，没有下界，于是已过期 140 天的
	// g33-video.com 被算进了「30 天内到期」。负数天数必须单独归类——
	// "还有 -140 天到期"这种文案是在说谎，而且会让真正快到期的项被淹没。
	//
	// 过滤条件与到期巡检页（cert_inspect.go）保持一致：证书只看 active，
	// 域名排除已忽略/已过时的，否则两个页面的数字又会对不上（CMDB-015）。
	expiring := []expiringItem{}
	expired := []expiringItem{}
	if rows, err := h.DB.Query(`
		SELECT 'certificate', cn, DATE_FORMAT(expiry_at,'%Y-%m-%d'), DATEDIFF(expiry_at, NOW())
		FROM certificates
		WHERE status='active' AND expiry_at IS NOT NULL AND DATEDIFF(expiry_at, NOW()) <= 30
		UNION ALL
		SELECT 'domain', c.name, DATE_FORMAT(d.expiry_at,'%Y-%m-%d'), DATEDIFF(d.expiry_at, NOW())
		FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.type='domain' AND d.stale=0 AND d.ignored=0
		  AND d.expiry_at IS NOT NULL AND DATEDIFF(d.expiry_at, NOW()) <= 30
		ORDER BY 4 ASC`); err != nil {
		fail("expiring", err)
	} else {
		for rows.Next() {
			var it expiringItem
			if rows.Scan(&it.Type, &it.Name, &it.ExpiryAt, &it.Days) != nil {
				continue
			}
			if it.Days < 0 {
				expired = append(expired, it)
			} else {
				expiring = append(expiring, it)
			}
		}
		rows.Close()
	}
	// expiring 现在严格是 0 <= days <= 30；已过期的在 expired 里，两者互斥不重叠。
	resp["expiring"] = expiring
	resp["expired"] = expired

	// 只要有任何一项没查成功，整个响应就是不可信的——宁可让前端报错，
	// 也不能把"没查到"当成"值是 0"发出去。
	if firstErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "展示台统计查询失败: " + firstErr.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

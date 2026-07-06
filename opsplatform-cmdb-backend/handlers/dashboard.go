package handlers

import (
	"database/sql"
	"net/http"

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
func (h *DashboardHandler) Get(c *gin.Context) {
	count := func(q string, args ...any) int {
		var n int
		_ = h.DB.QueryRow(q, args...).Scan(&n)
		return n
	}

	resp := gin.H{
		"ci_total":     count(`SELECT COUNT(*) FROM cis`),
		"domain_total": count(`SELECT COUNT(*) FROM cis WHERE type='domain'`),
		"cert_total":   count(`SELECT COUNT(*) FROM cis WHERE type='certificate'`),
		"cert_active":  count(`SELECT COUNT(*) FROM certificates WHERE status='active'`),
		"cert_expired": count(`SELECT COUNT(*) FROM certificates WHERE status='active' AND expiry_at < NOW()`),
		// 线上检测证书（解析记录），未忽略
		"online_cert_expiring": count(`SELECT COUNT(*) FROM domain_records WHERE ignored=0 AND cert_ignored=0 AND cert_expiry_at IS NOT NULL AND cert_expiry_at >= NOW() AND cert_expiry_at < DATE_ADD(NOW(), INTERVAL 30 DAY)`),
		"online_cert_expired":  count(`SELECT COUNT(*) FROM domain_records WHERE ignored=0 AND cert_ignored=0 AND cert_expiry_at IS NOT NULL AND cert_expiry_at < NOW()`),
		"online_cert_failed":   count(`SELECT COUNT(*) FROM domain_records WHERE ignored=0 AND cert_ignored=0 AND cert_expiry_at IS NULL AND cert_check_msg<>''`),
	}

	// 按环境分布
	byEnv := map[string]int{}
	if rows, err := h.DB.Query(`SELECT COALESCE(NULLIF(env,''),'未分类'), COUNT(*) FROM cis GROUP BY env`); err == nil {
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

	// 30 天内到期（证书 + 域名）
	expiring := []expiringItem{}
	if rows, err := h.DB.Query(`
		SELECT 'certificate', cn, DATE_FORMAT(expiry_at,'%Y-%m-%d'), DATEDIFF(expiry_at, NOW())
		FROM certificates WHERE expiry_at IS NOT NULL AND expiry_at < DATE_ADD(NOW(), INTERVAL 30 DAY)
		UNION ALL
		SELECT 'domain', c.name, DATE_FORMAT(d.expiry_at,'%Y-%m-%d'), DATEDIFF(d.expiry_at, NOW())
		FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.type='domain' AND d.expiry_at IS NOT NULL AND d.expiry_at < DATE_ADD(NOW(), INTERVAL 30 DAY)
		ORDER BY 4 ASC`); err == nil {
		for rows.Next() {
			var it expiringItem
			if rows.Scan(&it.Type, &it.Name, &it.ExpiryAt, &it.Days) == nil {
				expiring = append(expiring, it)
			}
		}
		rows.Close()
	}
	resp["expiring"] = expiring

	c.JSON(http.StatusOK, resp)
}

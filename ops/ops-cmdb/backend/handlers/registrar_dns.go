package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"strings"

	"github.com/gin-gonic/gin"
)

// RegistrarDNSHandler 注册商（GoDaddy）侧的解析记录。
//
// # 为什么单开一个接口
//
// 这批记录一直在采（`dns_sync` 任务写进 `dns_records`），但**从没有任何页面
// 读过它**：DNS 解析页只有 Cloudflare 和 GCP Cloud DNS 两个来源
// （OPSCMDB-031 P0-10）。
//
// 而 62 个域名全部注册在 GoDaddy，其中相当一部分的 NS 就指向 GoDaddy ——
// 也就是说**真正生效的那份解析，页面上一条都看不到**。
//
// ⚠️ 这不是"能力没做"，是"做了没接"。本轮撞到的第 18 次。
type RegistrarDNSHandler struct{ DB *sql.DB }

func (h *RegistrarDNSHandler) Register(r *gin.RouterGroup) {
	r.GET("/registrar/dns-records", h.List)
}

// List GET /api/registrar/dns-records?domain=&type=&q=
func (h *RegistrarDNSHandler) List(c *gin.Context) {
	q := `SELECT ci.name, r.type, r.name, r.data, r.ttl, r.priority, r.protected, r.synced_at
	        FROM dns_records r
	        JOIN cis ci ON ci.id = r.domain_ci_id AND ci.type='domain'
	       WHERE 1=1`
	args := []any{}
	if d := strings.TrimSpace(c.Query("domain")); d != "" {
		q += " AND ci.name=?"
		args = append(args, d)
	}
	if t := strings.TrimSpace(c.Query("type")); t != "" {
		q += " AND r.type=?"
		args = append(args, t)
	}
	if kw := strings.TrimSpace(c.Query("q")); kw != "" {
		q += " AND (r.name LIKE ? OR r.data LIKE ? OR ci.name LIKE ?)"
		args = append(args, "%"+kw+"%", "%"+kw+"%", "%"+kw+"%")
	}
	q += " ORDER BY ci.name, r.name, r.type"

	rows, err := h.DB.Query(q, args...)
	if err != nil {
		// ⚠️ 查询失败不能退化成空数组。空数组会被读成「GoDaddy 侧没有解析记录」，
		// 而那正好是 P0-10 的原状 —— 一个看起来完全正常的谎
		httpx.Fail(c, httpx.CodeInternal, fmt.Errorf("读取注册商解析记录失败: %w", err), nil)
		return
	}
	defer rows.Close()

	auth := loadDNSAuthority(h.DB)
	out := []gin.H{}
	for rows.Next() {
		var domain, typ, name, data string
		var ttl int
		var prio sql.NullInt64
		var protected int
		var synced sql.NullTime
		if rows.Scan(&domain, &typ, &name, &data, &ttl, &prio, &protected, &synced) != nil {
			continue
		}
		fqdn := recordFQDN(name, domain)
		item := gin.H{
			"domain": domain, "type": typ, "name": name, "fqdn": fqdn,
			"data": data, "ttl": ttl, "protected": protected == 1,
		}
		if prio.Valid {
			item["priority"] = prio.Int64
		}
		if synced.Valid {
			item["synced_at"] = synced.Time
		}
		// 每一条都带上「这一方到底生不生效」。
		//
		//	⚠️ 判不出来时给 unknown 而不是省略字段：省略会让前端的
		//	`?? true` 之类兜底把它变成"生效"，那是最坏的方向
		a := authorityOf(auth, fqdn)
		switch {
		case a == nil || a.Provider == ProviderUnknown:
			item["effective"] = nil
			item["effective_note"] = "没采到该域名的 NS 记录，判不出这份解析生不生效"
		case a.Provider == ProviderGoDaddy:
			item["effective"] = true
		default:
			item["effective"] = false
			item["effective_note"] = "该域名的 NS 指向 " + providerLabel(a.Provider) +
				"，GoDaddy 这边的记录不生效，改了不会有任何效果"
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "total": len(out)})
}

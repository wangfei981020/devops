package handlers

import (
	"database/sql"
	"net/http"

	"github.com/gin-gonic/gin"
)

// CertInspectHandler 证书巡检：跨所有域名把线上检测证书 + ACME 签发证书拉平成一张表。
type CertInspectHandler struct {
	DB *sql.DB
}

func NewCertInspectHandler(db *sql.DB) *CertInspectHandler { return &CertInspectHandler{DB: db} }

func (h *CertInspectHandler) Register(r *gin.RouterGroup) {
	r.GET("/cert-inspect", h.List)
	r.PUT("/records/:id/cert-ignore", h.Ignore)
}

type certInspectItem struct {
	Kind         string `json:"kind"` // online=线上检测 / acme=签发
	RecordID     int64  `json:"record_id"`
	DomainCIID   int64  `json:"domain_ci_id"`
	FQDN         string `json:"fqdn"`
	Domain       string `json:"domain"`
	ExpiryAt     string `json:"expiry_at"`
	CheckMsg     string `json:"check_msg"`
	Ignored      bool   `json:"ignored"`
	IgnoreReason string `json:"ignore_reason"`
}

// List 返回全量巡检项（前端做排序/筛选/分页）。
func (h *CertInspectHandler) List(c *gin.Context) {
	out := []certInspectItem{}

	// 线上检测证书（来自解析记录）
	rows, err := h.DB.Query(`
		SELECT r.id, r.domain_ci_id, r.host, c.name, r.cert_expiry_at, r.cert_check_msg, r.cert_ignored, r.cert_ignore_reason
		FROM domain_records r JOIN cis c ON c.id=r.domain_ci_id
		WHERE r.ignored=0`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	for rows.Next() {
		var it certInspectItem
		var host, domain string
		var exp sql.NullTime
		var ignored int
		if rows.Scan(&it.RecordID, &it.DomainCIID, &host, &domain, &exp, &it.CheckMsg, &ignored, &it.IgnoreReason) != nil {
			continue
		}
		it.Kind = "online"
		it.Domain = domain
		it.FQDN = recordFQDN(host, domain)
		if exp.Valid {
			it.ExpiryAt = exp.Time.Format("2006-01-02")
		}
		it.Ignored = ignored == 1
		out = append(out, it)
	}
	rows.Close()

	// 域名注册到期（WHOIS）
	drows, err := h.DB.Query(`SELECT c.id, c.name, d.expiry_at FROM cis c JOIN domains d ON d.ci_id=c.id WHERE c.type='domain' AND d.stale=0`)
	if err == nil {
		for drows.Next() {
			var it certInspectItem
			var name string
			var exp sql.NullTime
			if drows.Scan(&it.DomainCIID, &name, &exp) != nil {
				continue
			}
			it.Kind = "domain"
			it.FQDN = name
			it.Domain = name
			if exp.Valid {
				it.ExpiryAt = exp.Time.Format("2006-01-02")
			}
			out = append(out, it)
		}
		drows.Close()
	}

	// ACME 签发证书
	arows, err := h.DB.Query(`SELECT cert.ci_id, c.name, cert.cn, cert.expiry_at
		FROM certificates cert JOIN cis c ON c.id=cert.ci_id WHERE cert.status='active'`)
	if err == nil {
		for arows.Next() {
			var it certInspectItem
			var name, cn string
			var exp sql.NullTime
			if arows.Scan(&it.DomainCIID, &name, &cn, &exp) != nil {
				continue
			}
			it.Kind = "acme"
			it.FQDN = cn
			it.Domain = name
			if exp.Valid {
				it.ExpiryAt = exp.Time.Format("2006-01-02")
			}
			out = append(out, it)
		}
		arows.Close()
	}

	c.JSON(http.StatusOK, out)
}

type certIgnoreIn struct {
	Ignored bool   `json:"ignored"`
	Reason  string `json:"reason"`
}

// Ignore 标记/取消某条解析的证书忽略。
func (h *CertInspectHandler) Ignore(c *gin.Context) {
	var in certIgnoreIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	v := 0
	reason := ""
	if in.Ignored {
		v = 1
		reason = in.Reason
	}
	if _, err := h.DB.Exec(`UPDATE domain_records SET cert_ignored=?, cert_ignore_reason=? WHERE id=?`,
		v, reason, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "cert_ignore", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

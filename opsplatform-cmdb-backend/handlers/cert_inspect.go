package handlers

import (
	"database/sql"
	"net/http"

	"opsplatform-cmdb-backend/logx"

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
	DomainStatus string `json:"domain_status"` // 所属主域名生命周期状态（已下线/未使用的证书可停续期）
	OriginIP     string `json:"origin_ip"`     // 解析目标地址，内网判定的依据，界面上也要能看见
	// 失败归类（见 tls_error.go）。check_msg 非空时才有值。
	//
	//	scope=internal 的**不是失败**：内网地址被公网巡检器探测，连不上是必然的。
	//	前端据此把这类单列，不再混进"检测失败 N"里。
	ReasonKey   string `json:"reason_key"`
	ReasonLabel string `json:"reason_label"`
	Scope       string `json:"scope"`
}

// List 返回全量巡检项（前端做排序/筛选/分页）。
//
// ⚠️ 三段来源（线上检测 / 域名注册 / ACME 签发）缺一不可：少了任何一段，
// 前端算出来的"快到期 N / 已过期 N / 检测失败 N"就是偏小的，
// 而偏小的告警数比报错更危险（CMDB-013）。所以任何一段查失败都返回 500。
func (h *CertInspectHandler) List(c *gin.Context) {
	out := []certInspectItem{}
	// 主域名生命周期状态查表：domain ci_id → status
	statusMap := map[int64]string{}
	if srows, e := h.DB.Query(`SELECT ci_id, status FROM domains WHERE status<>''`); e != nil {
		logx.J("cert_inspect", "query_fail", map[string]any{"item": "domain_status", "err": e.Error()})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "巡检查询失败(域名状态): " + e.Error()})
		return
	} else {
		for srows.Next() {
			var id int64
			var st string
			if srows.Scan(&id, &st) == nil {
				statusMap[id] = st
			}
		}
		srows.Close()
	}

	// 线上检测证书（来自解析记录）
	rows, err := h.DB.Query(`
		SELECT r.id, r.domain_ci_id, r.host, c.name, r.cert_expiry_at, r.cert_check_msg, r.cert_ignored, r.cert_ignore_reason,
		       COALESCE(r.origin_ip,'')
		FROM domain_records r JOIN cis c ON c.id=r.domain_ci_id
		WHERE r.ignored=0`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	internalN := 0
	for rows.Next() {
		var it certInspectItem
		var host, domain string
		var exp sql.NullTime
		var ignored int
		if rows.Scan(&it.RecordID, &it.DomainCIID, &host, &domain, &exp, &it.CheckMsg, &ignored, &it.IgnoreReason,
			&it.OriginIP) != nil {
			continue
		}
		it.Kind = "online"
		it.Domain = domain
		it.FQDN = recordFQDN(host, domain)
		if exp.Valid {
			it.ExpiryAt = exp.Time.Format("2006-01-02")
		}
		it.Ignored = ignored == 1
		it.DomainStatus = statusMap[it.DomainCIID]
		if r := classifyTLSError(it.CheckMsg, it.OriginIP); r.Key != "" {
			it.ReasonKey, it.ReasonLabel, it.Scope = r.Key, r.Label, r.Scope
			if r.Scope == scopeInternal {
				internalN++
			}
		}
		out = append(out, it)
	}
	rows.Close()
	if internalN > 0 {
		// 这个数字直接决定"检测失败 N"要不要信，必须留痕
		logx.J("cert_inspect", "internal_targets", map[string]any{
			"count": internalN, "note": "解析到内网地址，公网探测不适用，已从失败计数中分出",
		})
	}

	// 域名注册到期（WHOIS）——已忽略的主域名不纳入巡检
	drows, err := h.DB.Query(`SELECT c.id, c.name, d.expiry_at FROM cis c JOIN domains d ON d.ci_id=c.id WHERE c.type='domain' AND d.stale=0 AND d.ignored=0`)
	if err != nil {
		logx.J("cert_inspect", "query_fail", map[string]any{"item": "domain_expiry", "err": err.Error()})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "巡检查询失败(域名到期): " + err.Error()})
		return
	} else {
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
			it.DomainStatus = statusMap[it.DomainCIID]
			out = append(out, it)
		}
		drows.Close()
	}

	// ACME 签发证书
	arows, err := h.DB.Query(`SELECT cert.ci_id, c.name, cert.cn, cert.expiry_at
		FROM certificates cert JOIN cis c ON c.id=cert.ci_id WHERE cert.status='active'`)
	if err != nil {
		logx.J("cert_inspect", "query_fail", map[string]any{"item": "acme_certs", "err": err.Error()})
		c.JSON(http.StatusInternalServerError, gin.H{"error": "巡检查询失败(ACME 证书): " + err.Error()})
		return
	} else {
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
	SetAuditTarget(c, c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

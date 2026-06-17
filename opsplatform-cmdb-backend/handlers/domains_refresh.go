package handlers

import (
	"crypto/tls"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/likexian/whois"
	whoisparser "github.com/likexian/whois-parser"
	"golang.org/x/net/publicsuffix"
)

// Refresh 刷新单个域名的注册到期(WHOIS) + 线上 HTTPS 证书到期(连 443)。
func (h *DomainHandler) Refresh(c *gin.Context) {
	ciid := c.Param("ciid")
	var name string
	if err := h.DB.QueryRow(`SELECT name FROM cis WHERE id=? AND type='domain'`, ciid).Scan(&name); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	msg := refreshOneDomain(h.DB, ciid, name)
	WriteAudit(h.DB, c, "refresh_domain", name)
	c.JSON(http.StatusOK, gin.H{"ok": true, "msg": msg})
}

// RefreshAll 一键刷新所有域名的到期时间。
func (h *DomainHandler) RefreshAll(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT c.id, c.name FROM cis c JOIN domains d ON d.ci_id=c.id WHERE c.type='domain'`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	type item struct {
		id   int64
		name string
	}
	var items []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.name) == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	for _, it := range items {
		refreshOneDomain(h.DB, it.id, it.name)
	}
	WriteAudit(h.DB, c, "refresh_domain_all", fmt.Sprintf("%d 个", len(items)))
	c.JSON(http.StatusOK, gin.H{"ok": true, "count": len(items)})
}

func refreshOneDomain(db *sql.DB, ciid any, name string) string {
	var msgs []string

	// ① 域名注册到期（WHOIS）
	if t := whoisExpiry(name); t != nil {
		_, _ = db.Exec(`UPDATE domains SET expiry_at=? WHERE ci_id=?`, *t, ciid)
		msgs = append(msgs, "域名到期 "+t.Format("2006-01-02"))
	} else {
		msgs = append(msgs, "WHOIS 未取到注册到期")
	}

	// ② 线上 HTTPS 证书到期（连 443）
	if t2, cmsg := tlsCertExpiry(name); t2 != nil {
		_, _ = db.Exec(`UPDATE domains SET cert_expiry_at=?, cert_check_at=NOW(), cert_check_msg='' WHERE ci_id=?`, *t2, ciid)
		msgs = append(msgs, "证书到期 "+t2.Format("2006-01-02"))
	} else {
		_, _ = db.Exec(`UPDATE domains SET cert_check_at=NOW(), cert_check_msg=? WHERE ci_id=?`, truncate(cmsg, 250), ciid)
		msgs = append(msgs, "证书检查: "+cmsg)
	}
	return strings.Join(msgs, "；")
}

// whoisExpiry 查询域名注册到期时间。WHOIS 只认主域名(eTLD+1)，自动去掉 www 等子域。
func whoisExpiry(domain string) *time.Time {
	root, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil || root == "" {
		root = domain
	}
	raw, err := whois.Whois(root)
	if err != nil {
		return nil
	}
	parsed, err := whoisparser.Parse(raw)
	if err != nil {
		return nil
	}
	if parsed.Domain != nil && parsed.Domain.ExpirationDateInTime != nil {
		return parsed.Domain.ExpirationDateInTime
	}
	return nil
}

// tlsCertExpiry 连接 domain:443 读线上证书的到期时间。
func tlsCertExpiry(domain string) (*time.Time, string) {
	d := &net.Dialer{Timeout: 8 * time.Second}
	conn, err := tls.DialWithDialer(d, "tcp", domain+":443", &tls.Config{ServerName: domain, InsecureSkipVerify: true})
	if err != nil {
		return nil, "连接 443 失败: " + err.Error()
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, "未取到证书"
	}
	na := certs[0].NotAfter
	return &na, ""
}

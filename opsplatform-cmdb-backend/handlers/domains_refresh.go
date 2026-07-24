package handlers

import (
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"io"
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

// RefreshAll 一键刷新域名注册到期。与定时任务 refresh_expiry 完全一致：
// 数据源(origin=sync)域名跳过（到期日由 DNS 同步维护）、只刷 manual/无到期日域名、RDAP→WHOIS+自动重试。
// 证书到期请走「到期巡检」；单个域名的「刷到期」按钮仍会同时刷注册+证书（Refresh）。
func (h *DomainHandler) RefreshAll(c *gin.Context) {
	msg, failures, _ := refreshAllWhoisCore(context.Background(), h.DB, nil, nil)
	WriteAudit(h.DB, c, "refresh_domain_all", msg)
	c.JSON(http.StatusOK, gin.H{"ok": true, "msg": msg, "failures": failures})
}

func refreshOneDomain(db *sql.DB, ciid any, name string) string {
	var msgs []string

	// ① 域名注册到期（RDAP 优先，WHOIS 兜底）
	if t, reason := domainExpiry(name); t != nil {
		_, _ = db.Exec(`UPDATE domains SET expiry_at=? WHERE ci_id=?`, *t, ciid)
		msgs = append(msgs, "域名到期 "+t.Format("2006-01-02"))
	} else {
		msgs = append(msgs, "注册到期未取到："+reason)
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

// domainExpiry 查域名注册到期：RDAP 优先（结构化、限流宽松），WHOIS 兜底；只认主域名(eTLD+1)。
// 返回到期时间 + 失败原因（成功时原因为空）。
func domainExpiry(domain string) (*time.Time, string) {
	root, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil || root == "" {
		root = domain
	}
	// ① RDAP
	if t := rdapExpiry(root); t != nil {
		return t, ""
	}
	// ② WHOIS 兜底
	return whoisExpiryReason(root)
}

// rdapExpiry 通过 rdap.org 聚合入口查到期日（RFC 7482，events[expiration]）；尽力而为，失败返回 nil。
func rdapExpiry(root string) *time.Time {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", "https://rdap.org/domain/"+root, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/rdap+json")
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil
	}
	var data struct {
		Events []struct {
			Action string `json:"eventAction"`
			Date   string `json:"eventDate"`
		} `json:"events"`
	}
	if json.Unmarshal(body, &data) != nil {
		return nil
	}
	for _, e := range data.Events {
		if e.Action == "expiration" {
			if t, err := time.Parse(time.RFC3339, e.Date); err == nil {
				return &t
			}
		}
	}
	return nil
}

// whoisExpiryReason WHOIS 查到期日，带 10s 超时；失败返回具体原因。
func whoisExpiryReason(root string) (*time.Time, string) {
	client := whois.NewClient()
	client.SetTimeout(10 * time.Second)
	raw, err := client.Whois(root)
	if err != nil {
		return nil, whoisReason(err.Error())
	}
	parsed, err := whoisparser.Parse(raw)
	if err != nil {
		return nil, whoisReason(err.Error())
	}
	if parsed.Domain != nil && parsed.Domain.ExpirationDateInTime != nil {
		return parsed.Domain.ExpirationDateInTime, ""
	}
	return nil, "RDAP/WHOIS 查到响应但无到期字段"
}

// whoisReason 把底层错误归类成人话原因。
func whoisReason(errMsg string) string {
	m := strings.ToLower(errMsg)
	switch {
	case strings.Contains(m, "timeout") || strings.Contains(m, "deadline") || strings.Contains(m, "timed out"):
		return "RDAP/WHOIS 查询超时（10s）"
	case strings.Contains(m, "rate") || strings.Contains(m, "limit") || strings.Contains(m, "quota") || strings.Contains(m, "denied") || strings.Contains(m, "refused"):
		return "WHOIS 服务器限流/拒绝"
	case strings.Contains(m, "no whois") || strings.Contains(m, "not found") || strings.Contains(m, "no such"):
		return "该后缀无 WHOIS 服务器"
	case strings.Contains(m, "no data") || strings.Contains(m, "parse"):
		return "RDAP/WHOIS 查到响应但无到期字段"
	default:
		return "RDAP/WHOIS 查询失败：" + truncate(errMsg, 80)
	}
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

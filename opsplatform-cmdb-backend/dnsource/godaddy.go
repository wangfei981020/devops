package dnsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// GoDaddy 数据源 adapter。API: https://api.godaddy.com，认证头 Authorization: sso-key KEY:SECRET。
type GoDaddy struct {
	key    string
	secret string
	lim    *Limiter
}

const godaddyBase = "https://api.godaddy.com"

var gdClient = &http.Client{Timeout: 15 * time.Second}

func (a *GoDaddy) do(ctx context.Context, path string, out any) error {
	if err := a.lim.Allow(); err != nil {
		return err // *RateLimitError，未真正打 GoDaddy
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, godaddyBase+path, nil)
	req.Header.Set("Authorization", fmt.Sprintf("sso-key %s:%s", a.key, a.secret))
	req.Header.Set("Accept", "application/json")
	resp, err := gdClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("GoDaddy 返回 429（厂商限流），请稍后再试")
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("GoDaddy API %d: %s", resp.StatusCode, truncateStr(string(body), 200))
	}
	return json.Unmarshal(body, out)
}

func (a *GoDaddy) ListDomains(ctx context.Context) ([]Domain, error) {
	// 拉账户下**全部状态**的域名（不再限 ACTIVE，否则新注册/待验证等中间态域名会被漏掉）。
	// GoDaddy 列表按域名字母序、游标(marker)翻页：每页取 limit 条，marker=上页最后一个域名，
	// 直到某页不足 limit 即为最后一页。
	const pageSize = 1000
	out := make([]Domain, 0, pageSize)
	marker := ""
	for {
		path := fmt.Sprintf("/v1/domains?limit=%d", pageSize)
		if marker != "" {
			path += "&marker=" + url.QueryEscape(marker)
		}
		var raw []struct {
			Domain  string `json:"domain"`
			Status  string `json:"status"`
			Expires string `json:"expires"`
		}
		if err := a.do(ctx, path, &raw); err != nil {
			return nil, err
		}
		for _, d := range raw {
			dm := Domain{Name: d.Domain, Status: d.Status}
			if d.Expires != "" {
				if t, err := time.Parse(time.RFC3339, d.Expires); err == nil {
					dm.ExpiresAt = &t
				}
			}
			out = append(out, dm)
		}
		if len(raw) < pageSize {
			break // 最后一页
		}
		marker = raw[len(raw)-1].Domain
	}
	return out, nil
}

func (a *GoDaddy) ListRecords(ctx context.Context, domain string) ([]DNSRecord, error) {
	var raw []struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Data     string `json:"data"`
		TTL      int    `json:"ttl"`
		Priority *int   `json:"priority,omitempty"`
	}
	if err := a.do(ctx, "/v1/domains/"+domain+"/records", &raw); err != nil {
		return nil, err
	}
	out := make([]DNSRecord, 0, len(raw))
	for _, r := range raw {
		out = append(out, DNSRecord{Type: r.Type, Name: r.Name, Data: r.Data, TTL: r.TTL, Priority: r.Priority})
	}
	return out, nil
}

func truncateStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

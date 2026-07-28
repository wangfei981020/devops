// Package cdnsource CDN 数据源适配层：只读同步 CDN 厂商的 Zone / DNS / 关键配置。
// 当前实现 Cloudflare。只读——CMDB 不回写任何 CDN 配置。
package cdnsource

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Zone 一个 CDN 站点（对应一个根域名）。
type Zone struct {
	ZoneID      string
	Name        string
	Status      string
	Paused      bool
	Plan        string
	NameServers []string
}

// DNSRecord CDN 侧的一条解析记录。
// Proxied 即「橙云」：true 表示流量经 CDN 转发，false 表示直连源站——
// 这个字段决定 CDN 到底有没有生效，是排查「改了配置却没效果」的第一落点。
type DNSRecord struct {
	RecordID string
	Type     string
	Name     string
	Content  string
	Proxied  bool
	TTL      int
}

// Setting Zone 级配置项（CF 的 settings 接口本身就是列表形态）。
type Setting struct {
	Name  string
	Value string
}

// Client Cloudflare API v4 只读客户端。
type Client struct {
	token string
	http  *http.Client
	base  string
}

func NewCloudflare(token string) *Client {
	return &Client{
		token: token,
		base:  "https://api.cloudflare.com/client/v4",
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

// cfResp Cloudflare 统一响应包。errors 里才有真实原因，
// HTTP 200 也可能 success=false，必须看这个字段。
type cfResp struct {
	Success bool            `json:"success"`
	Errors  []cfError       `json:"errors"`
	Result  json.RawMessage `json:"result"`
	Info    struct {
		Page       int `json:"page"`
		TotalPages int `json:"total_pages"`
	} `json:"result_info"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) get(ctx context.Context, path string, q url.Values) (*cfResp, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	var r cfResp
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("解析 Cloudflare 响应失败(HTTP %d): %s", resp.StatusCode, trunc(string(body), 200))
	}
	if !r.Success {
		return nil, fmt.Errorf("Cloudflare 返回失败(HTTP %d): %s", resp.StatusCode, joinErrors(r.Errors))
	}
	return &r, nil
}

// getPaged 逐页拉取并把各页 result 合并。CF 默认每页 20 条，
// DNS 记录动辄上百条，不翻页会静默丢数据。
func (c *Client) getPaged(ctx context.Context, path string, q url.Values) ([]json.RawMessage, error) {
	if q == nil {
		q = url.Values{}
	}
	q.Set("per_page", "100")
	out := []json.RawMessage{}
	for page := 1; ; page++ {
		q.Set("page", fmt.Sprintf("%d", page))
		r, err := c.get(ctx, path, q)
		if err != nil {
			return nil, err
		}
		var items []json.RawMessage
		if err := json.Unmarshal(r.Result, &items); err != nil {
			return nil, err
		}
		out = append(out, items...)
		if r.Info.TotalPages <= page || len(items) == 0 {
			return out, nil
		}
	}
}

// Verify 校验 token 是否真的能用（配账号时先探一下，免得配错了到同步才发现）。
//
// 不用 /user/tokens/verify：那个端点只认「用户 API 令牌」，而 Cloudflare 的
// 「账户 API 令牌」(Account-owned) 不属于任何用户，调它一律返回 401 Invalid API Token——
// 哪怕这个 token 完全正常。实测就撞过：verify 报 401，同一个 token 同步却成功拉回
// 6 个站点 45 条解析记录。账户令牌要验得带 account_id，但那个字段是选填的，靠它不可靠。
//
// 所以直接探一个真正要调的只读接口。好处是顺带把权限也验了：
// tokens/verify 只回答「token 有没有效」，答不了「Zone:Read 给了没有」，
// 而后者才是配错时最常见的问题。
func (c *Client) Verify(ctx context.Context) error {
	q := url.Values{}
	q.Set("per_page", "1") // 只要能通就行，不必真拉数据
	_, err := c.get(ctx, "/zones", q)
	return err
}

func (c *Client) ListZones(ctx context.Context) ([]Zone, error) {
	items, err := c.getPaged(ctx, "/zones", nil)
	if err != nil {
		return nil, err
	}
	out := make([]Zone, 0, len(items))
	for _, raw := range items {
		var z struct {
			ID          string   `json:"id"`
			Name        string   `json:"name"`
			Status      string   `json:"status"`
			Paused      bool     `json:"paused"`
			NameServers []string `json:"name_servers"`
			Plan        struct {
				Name string `json:"name"`
			} `json:"plan"`
		}
		if json.Unmarshal(raw, &z) != nil {
			continue
		}
		out = append(out, Zone{
			ZoneID: z.ID, Name: z.Name, Status: z.Status, Paused: z.Paused,
			Plan: z.Plan.Name, NameServers: z.NameServers,
		})
	}
	return out, nil
}

func (c *Client) ListDNSRecords(ctx context.Context, zoneID string) ([]DNSRecord, error) {
	items, err := c.getPaged(ctx, "/zones/"+zoneID+"/dns_records", nil)
	if err != nil {
		return nil, err
	}
	out := make([]DNSRecord, 0, len(items))
	for _, raw := range items {
		var r struct {
			ID      string `json:"id"`
			Type    string `json:"type"`
			Name    string `json:"name"`
			Content string `json:"content"`
			Proxied bool   `json:"proxied"`
			TTL     int    `json:"ttl"`
		}
		if json.Unmarshal(raw, &r) != nil {
			continue
		}
		out = append(out, DNSRecord{
			RecordID: r.ID, Type: r.Type, Name: r.Name,
			Content: r.Content, Proxied: r.Proxied, TTL: r.TTL,
		})
	}
	return out, nil
}

// wantedSettings 只取排障真正用得上的几项，避免把几十项无关配置全灌进库。
// ssl=flexible 意味着 CF 到源站是明文，是最常见的错误配置。
var wantedSettings = map[string]bool{
	"ssl": true, "always_use_https": true, "min_tls_version": true,
	"tls_1_3": true, "automatic_https_rewrites": true,
	"development_mode": true, "cache_level": true, "browser_cache_ttl": true,
	"security_level": true, "waf": true, "http2": true, "http3": true,
}

func (c *Client) ListZoneSettings(ctx context.Context, zoneID string) ([]Setting, error) {
	r, err := c.get(ctx, "/zones/"+zoneID+"/settings", nil)
	if err != nil {
		return nil, err
	}
	var items []struct {
		ID    string          `json:"id"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(r.Result, &items); err != nil {
		return nil, err
	}
	out := make([]Setting, 0, len(wantedSettings))
	for _, it := range items {
		if !wantedSettings[it.ID] {
			continue
		}
		out = append(out, Setting{Name: it.ID, Value: rawToString(it.Value)})
	}
	return out, nil
}

// rawToString 设置值可能是字符串/数字/布尔/对象，统一转成可读文本。
func rawToString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return trunc(strings.TrimSpace(string(raw)), 512)
}

func joinErrors(es []cfError) string {
	if len(es) == 0 {
		return "无错误详情"
	}
	parts := make([]string, 0, len(es))
	for _, e := range es {
		parts = append(parts, fmt.Sprintf("[%d] %s", e.Code, e.Message))
	}
	return strings.Join(parts, "; ")
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

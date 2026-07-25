package dnsource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"
)

// GoDaddy 数据源 adapter。API: https://api.godaddy.com（测试 https://api.ote-godaddy.com），
// 认证头 Authorization: sso-key KEY:SECRET（经典 API 密钥）。
type GoDaddy struct {
	key    string
	secret string
	base   string // 空则用生产 godaddyBase；OTE 测试填 https://api.ote-godaddy.com
	dryRun bool   // 写回预演：只打日志不真发（生产护栏）
	lim    *Limiter
}

const godaddyBase = "https://api.godaddy.com"

var gdClient = &http.Client{Timeout: 15 * time.Second}

// baseURL 返回该源的 API 根地址（默认生产）。
func (a *GoDaddy) baseURL() string {
	if a.base != "" {
		return a.base
	}
	return godaddyBase
}

// DryRun / EnvLabel 实现 WriteAdapter 的元信息。
func (a *GoDaddy) DryRun() bool { return a.dryRun }
func (a *GoDaddy) EnvLabel() string {
	if a.base != "" && a.base != godaddyBase {
		return "OTE测试(" + a.base + ")"
	}
	return "生产"
}

func (a *GoDaddy) do(ctx context.Context, path string, out any) error {
	// 撞客户端限流时节流等待（不再直接失败），让全量同步能完整跑完；仍守住 50/分钟不打爆 GoDaddy。
	if err := a.lim.Wait(ctx); err != nil {
		return err // 仅 ctx 取消/超时才返回
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, a.baseURL()+path, nil)
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

// doWrite 发写请求（PUT/PATCH/DELETE），不关心响应体。
func (a *GoDaddy) doWrite(ctx context.Context, method, path string, payload any) error {
	_, err := a.doWriteBody(ctx, method, path, payload)
	return err
}

// doWriteBody 发写请求并返回响应体（续费要抓订单号）。dryRun 时只打日志不真发（生产护栏），返回空体。
// 全链路诊断日志：环境 + 方法 + 路径 + 报文，出错带 GoDaddy 响应体。
func (a *GoDaddy) doWriteBody(ctx context.Context, method, path string, payload any) ([]byte, error) {
	var bodyStr string
	var reader io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		bodyStr = string(b)
		reader = bytes.NewReader(b)
	}
	if a.dryRun {
		log.Printf("[godaddy写回][DRY-RUN][%s] %s %s%s  body=%s （预演，未真发）", a.EnvLabel(), method, a.baseURL(), path, bodyStr)
		return nil, nil
	}
	log.Printf("[godaddy写回][%s] %s %s%s  body=%s", a.EnvLabel(), method, a.baseURL(), path, bodyStr)
	if err := a.lim.Wait(ctx); err != nil {
		return nil, err
	}
	req, _ := http.NewRequestWithContext(ctx, method, a.baseURL()+path, reader)
	req.Header.Set("Authorization", fmt.Sprintf("sso-key %s:%s", a.key, a.secret))
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := gdClient.Do(req)
	if err != nil {
		log.Printf("[godaddy写回][%s] 请求失败 %s %s: %v", a.EnvLabel(), method, path, err)
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("GoDaddy 返回 429（厂商限流），请稍后再试")
	}
	if resp.StatusCode >= 300 {
		log.Printf("[godaddy写回][%s] 失败 %d: %s", a.EnvLabel(), resp.StatusCode, truncateStr(string(respBody), 300))
		return nil, fmt.Errorf("GoDaddy API %d: %s", resp.StatusCode, truncateStr(string(respBody), 200))
	}
	log.Printf("[godaddy写回][%s] 成功 %s %s  resp=%s", a.EnvLabel(), method, path, truncateStr(string(respBody), 200))
	return respBody, nil
}

// ---- WriteAdapter：按「类型+主机名」整组读改写（GoDaddy typed records 语义）----

// gdRecord GoDaddy typed records 端点的记录体（type/name 在 URL，body 只带 data/ttl/priority）。
type gdRecord struct {
	Data     string `json:"data"`
	TTL      int    `json:"ttl,omitempty"`
	Priority *int   `json:"priority,omitempty"`
}

// GetGroup GET /v1/domains/{domain}/records/{type}/{name} —— 取某 (type,name) 当前记录组。
func (a *GoDaddy) GetGroup(ctx context.Context, domain, rtype, name string) ([]DNSRecord, error) {
	path := fmt.Sprintf("/v1/domains/%s/records/%s/%s", domain, rtype, url.PathEscape(name))
	var raw []gdRecord
	if err := a.do(ctx, path, &raw); err != nil {
		log.Printf("[godaddy读组][%s] 失败 %s/%s@%s: %v", a.EnvLabel(), rtype, name, domain, err)
		return nil, err
	}
	out := make([]DNSRecord, 0, len(raw))
	for _, r := range raw {
		out = append(out, DNSRecord{Type: rtype, Name: name, Data: r.Data, TTL: r.TTL, Priority: r.Priority})
	}
	return out, nil
}

// ReplaceGroup PUT /v1/domains/{domain}/records/{type}/{name} —— 整组替换（新增/编辑）。
// 传空组会被 GoDaddy 拒（PUT 需≥1条）；删空场景走 DeleteGroup。
func (a *GoDaddy) ReplaceGroup(ctx context.Context, domain, rtype, name string, recs []DNSRecord) error {
	path := fmt.Sprintf("/v1/domains/%s/records/%s/%s", domain, rtype, url.PathEscape(name))
	body := make([]gdRecord, 0, len(recs))
	for _, r := range recs {
		body = append(body, gdRecord{Data: r.Data, TTL: r.TTL, Priority: r.Priority})
	}
	return a.doWrite(ctx, http.MethodPut, path, body)
}

// DeleteGroup DELETE /v1/domains/{domain}/records/{type}/{name} —— 删整组。
func (a *GoDaddy) DeleteGroup(ctx context.Context, domain, rtype, name string) error {
	path := fmt.Sprintf("/v1/domains/%s/records/%s/%s", domain, rtype, url.PathEscape(name))
	return a.doWrite(ctx, http.MethodDelete, path, nil)
}

// gdFullRecord PATCH /records 的记录体（type/name 在 body 里，与 typed PUT 不同）。
type gdFullRecord struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Data     string `json:"data"`
	TTL      int    `json:"ttl,omitempty"`
	Priority *int   `json:"priority,omitempty"`
}

// AddRecords PATCH /v1/domains/{domain}/records —— 一次追加多条（批量新增）。追加语义，不去重。
func (a *GoDaddy) AddRecords(ctx context.Context, domain string, recs []DNSRecord) error {
	body := make([]gdFullRecord, 0, len(recs))
	for _, r := range recs {
		body = append(body, gdFullRecord{Type: r.Type, Name: r.Name, Data: r.Data, TTL: r.TTL, Priority: r.Priority})
	}
	return a.doWrite(ctx, http.MethodPatch, "/v1/domains/"+domain+"/records", body)
}

// ---- 域名续费 / 自动续费 ----

// GetDomainDetail GET /v1/domains/{domain} —— 取到期/自动续费/状态。
func (a *GoDaddy) GetDomainDetail(ctx context.Context, domain string) (DomainDetail, error) {
	var raw struct {
		Expires   string `json:"expires"`
		RenewAuto bool   `json:"renewAuto"`
		Privacy   bool   `json:"privacy"`
		Status    string `json:"status"`
	}
	if err := a.do(ctx, "/v1/domains/"+domain, &raw); err != nil {
		log.Printf("[godaddy域名详情][%s] 失败 %s: %v", a.EnvLabel(), domain, err)
		return DomainDetail{}, err
	}
	d := DomainDetail{RenewAuto: raw.RenewAuto, Privacy: raw.Privacy, Status: raw.Status}
	if raw.Expires != "" {
		if t, err := time.Parse(time.RFC3339, raw.Expires); err == nil {
			d.Expires = &t
		}
	}
	return d, nil
}

// GetRenewalPrice 取续费挂牌价（估算）：GET /v1/domains/available?domain=X 返回 renewalPrice(微单位)+currency。
// 挂牌价按 TLD 定，与是否已注册无关，可作续费估算；查不到返回零值不报错（前端展示"以厂商结算为准"）。
func (a *GoDaddy) GetRenewalPrice(ctx context.Context, domain string) (RenewalPrice, error) {
	var raw struct {
		RenewalPrice int64  `json:"renewalPrice"`
		Price        int64  `json:"price"`
		Currency     string `json:"currency"`
	}
	if err := a.do(ctx, "/v1/domains/available?domain="+url.QueryEscape(domain), &raw); err != nil {
		log.Printf("[godaddy续费价][%s] 查价失败(不阻断) %s: %v", a.EnvLabel(), domain, err)
		return RenewalPrice{}, nil // 查价失败不阻断续费，返回零值
	}
	amt := raw.RenewalPrice
	if amt == 0 {
		amt = raw.Price // 兜底用注册价
	}
	return RenewalPrice{AmountMicro: amt, Currency: raw.Currency}, nil
}

// RenewDomain POST /v1/domains/{domain}/renew —— 续费 period 年。⚠️会真实扣费；dry_run 时只打日志不真扣。
// 抓 GoDaddy 返回的订单号（金额字段厂商不一定给）留档，供防超付核对。
func (a *GoDaddy) RenewDomain(ctx context.Context, domain string, period int) (RenewResult, error) {
	if period <= 0 {
		period = 1
	}
	body, err := a.doWriteBody(ctx, http.MethodPost, "/v1/domains/"+domain+"/renew", map[string]int{"period": period})
	if err != nil {
		return RenewResult{}, err
	}
	res := RenewResult{RawBody: truncateStr(string(body), 1000)}
	if len(body) > 0 {
		var raw struct {
			OrderID  json.Number `json:"orderId"`
			Total    int64       `json:"total"`
			Currency string      `json:"currency"`
		}
		if json.Unmarshal(body, &raw) == nil {
			res.OrderID = raw.OrderID.String()
			res.AmountMicro = raw.Total
			res.Currency = raw.Currency
		}
	}
	return res, nil
}

// SetAutoRenew PATCH /v1/domains/{domain} —— 开/关自动续费（不扣费）。
func (a *GoDaddy) SetAutoRenew(ctx context.Context, domain string, enabled bool) error {
	return a.doWrite(ctx, http.MethodPatch, "/v1/domains/"+domain, map[string]bool{"renewAuto": enabled})
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

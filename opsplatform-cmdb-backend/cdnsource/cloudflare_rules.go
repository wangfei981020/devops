package cdnsource

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strings"
)

// Cloudflare 规则采集（Page Rules + Rulesets）。
//
// ⚠️ 字段映射按 Cloudflare API v4 官方文档写，**未经真实账号验证**——本地没有 CF 凭据。
// 因此这里的原则是：解析不出来就明确报错并把原始片段带进日志，绝不静默跳过。
// 部署到有真实 token 的环境后，先看 [cf-rules] 开头的日志再采信数据。
//
// 为什么两套都要采：Page Rules 是老体系（按 URL 匹配、有套餐数量上限），
// Rulesets 是新体系（按 phase 分类、表达式语法），两者可以同时存在且互相覆盖。
// 只看一套会得出「没配缓存规则」这种错误结论。

// Rule 一条规则，两种体系归一化后的形式。
type Rule struct {
	Source      string // pagerule | ruleset
	RuleID      string
	Name        string
	Phase       string
	Kind        string
	Priority    int
	Status      string
	Expression  string
	Actions     string
	LastUpdated string
}

// pageRuleLimits 各套餐的 Page Rules 数量上限（Cloudflare 公开文档）。
// 用途是「快到上限了」的预警——加不进新规则时报错很晚才会被发现。
var pageRuleLimits = map[string]int{
	"free": 3, "pro": 20, "business": 50, "enterprise": 125,
}

// PageRuleLimit 按套餐名返回上限，未知套餐返回 0（表示不做上限判定）。
// 套餐名形如 "Free Website"/"Pro Plan"，取首词小写匹配。
func PageRuleLimit(plan string) int {
	f := strings.ToLower(strings.TrimSpace(plan))
	if i := strings.IndexByte(f, ' '); i > 0 {
		f = f[:i]
	}
	return pageRuleLimits[f]
}

// ListPageRules 拉取 zone 的 Page Rules。
//
// 这个端点不分页，一次返回全部（受套餐上限约束，最多 125 条）。
func (c *Client) ListPageRules(ctx context.Context, zoneID string) ([]Rule, error) {
	r, err := c.get(ctx, "/zones/"+zoneID+"/pagerules", nil)
	if err != nil {
		log.Printf("[cf-rules] ERROR zone=%s 取 Page Rules 失败: %v（若提示权限不足，token 需要 Zone·Page Rules·Read）", zoneID, err)
		return nil, err
	}
	var raw []struct {
		ID      string `json:"id"`
		Targets []struct {
			Target     string `json:"target"`
			Constraint struct {
				Operator string `json:"operator"`
				Value    string `json:"value"`
			} `json:"constraint"`
		} `json:"targets"`
		Actions []struct {
			ID    string          `json:"id"`
			Value json.RawMessage `json:"value"`
		} `json:"actions"`
		Priority   int    `json:"priority"`
		Status     string `json:"status"`
		ModifiedOn string `json:"modified_on"`
	}
	if err := json.Unmarshal(r.Result, &raw); err != nil {
		// 结构对不上时把原始片段带出来，否则只看到「解析失败」无从修正映射
		log.Printf("[cf-rules] ERROR zone=%s Page Rules 响应结构与预期不符: %v，原始片段: %s",
			zoneID, err, trunc(string(r.Result), 300))
		return nil, fmt.Errorf("解析 Page Rules 失败: %w", err)
	}

	out := make([]Rule, 0, len(raw))
	for _, p := range raw {
		match := ""
		for _, t := range p.Targets {
			if t.Constraint.Value != "" {
				match = t.Constraint.Value
				break
			}
		}
		acts := make([]string, 0, len(p.Actions))
		for _, a := range p.Actions {
			acts = append(acts, actionSummary(a.ID, a.Value))
		}
		out = append(out, Rule{
			Source: "pagerule", RuleID: p.ID, Name: match, Priority: p.Priority,
			Status: p.Status, Expression: match, Actions: strings.Join(acts, ", "),
			LastUpdated: p.ModifiedOn,
		})
	}
	log.Printf("[cf-rules] zone=%s Page Rules %d 条", zoneID, len(out))
	return out, nil
}

// actionSummary 把动作压成「id=值」的短串。值可能是字符串、数字或对象，
// 统一按 JSON 原样截断——保真比好看重要，排查时要能看出实际配了什么。
func actionSummary(id string, val json.RawMessage) string {
	v := strings.TrimSpace(string(val))
	if v == "" || v == "null" {
		return id
	}
	v = strings.Trim(v, `"`)
	return id + "=" + trunc(v, 60)
}

// ListRulesets 拉取 zone 的 Rulesets。
//
// 只展开 kind=zone（用户自建）的 ruleset 详情：managed ruleset 是 Cloudflare 托管的
// WAF 规则集，条目上千且不可改，拉下来既没用又会打爆请求数。
func (c *Client) ListRulesets(ctx context.Context, zoneID string) ([]Rule, error) {
	r, err := c.get(ctx, "/zones/"+zoneID+"/rulesets", nil)
	if err != nil {
		log.Printf("[cf-rules] ERROR zone=%s 取 Rulesets 失败: %v（若提示权限不足，token 需要 Zone·Zone Settings·Read 或 Zone·Config·Read）", zoneID, err)
		return nil, err
	}
	var sets []struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Kind        string `json:"kind"`
		Phase       string `json:"phase"`
		LastUpdated string `json:"last_updated"`
	}
	if err := json.Unmarshal(r.Result, &sets); err != nil {
		log.Printf("[cf-rules] ERROR zone=%s Rulesets 响应结构与预期不符: %v，原始片段: %s",
			zoneID, err, trunc(string(r.Result), 300))
		return nil, fmt.Errorf("解析 Rulesets 失败: %w", err)
	}

	out := make([]Rule, 0, len(sets))
	for _, s := range sets {
		// 先记下 ruleset 本身，即使详情拉不到也留有痕迹
		base := Rule{
			Source: "ruleset", RuleID: s.ID, Name: s.Name, Phase: s.Phase,
			Kind: s.Kind, LastUpdated: s.LastUpdated, Status: "active",
		}
		if s.Kind != "zone" && s.Kind != "custom" {
			// managed/root：只留摘要，不展开
			base.Expression = "(Cloudflare 托管规则集，未展开明细)"
			out = append(out, base)
			continue
		}
		rules, e := c.rulesetDetail(ctx, zoneID, s.ID)
		if e != nil {
			base.Expression = "(明细获取失败: " + e.Error() + ")"
			out = append(out, base)
			continue
		}
		if len(rules) == 0 {
			base.Expression = "(空规则集)"
			out = append(out, base)
			continue
		}
		for _, rl := range rules {
			rl.Phase, rl.Kind = s.Phase, s.Kind
			if rl.Name == "" {
				rl.Name = s.Name
			}
			out = append(out, rl)
		}
	}
	log.Printf("[cf-rules] zone=%s Rulesets 展开后 %d 条", zoneID, len(out))
	return out, nil
}

// rulesetDetail 拉单个 ruleset 的规则明细。
func (c *Client) rulesetDetail(ctx context.Context, zoneID, rulesetID string) ([]Rule, error) {
	r, err := c.get(ctx, "/zones/"+zoneID+"/rulesets/"+rulesetID, url.Values{})
	if err != nil {
		return nil, err
	}
	var detail struct {
		Rules []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
			Expression  string `json:"expression"`
			Action      string `json:"action"`
			Enabled     *bool  `json:"enabled"`
			LastUpdated string `json:"last_updated"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(r.Result, &detail); err != nil {
		log.Printf("[cf-rules] ERROR zone=%s ruleset=%s 明细结构与预期不符: %v，原始片段: %s",
			zoneID, rulesetID, err, trunc(string(r.Result), 300))
		return nil, fmt.Errorf("解析 ruleset 明细失败: %w", err)
	}
	out := make([]Rule, 0, len(detail.Rules))
	for i, rl := range detail.Rules {
		st := "active"
		// enabled 缺省视为启用（CF 的默认），显式 false 才是禁用
		if rl.Enabled != nil && !*rl.Enabled {
			st = "disabled"
		}
		out = append(out, Rule{
			Source: "ruleset", RuleID: rl.ID, Name: rl.Description,
			Priority: i + 1, Status: st, Expression: rl.Expression,
			Actions: rl.Action, LastUpdated: rl.LastUpdated,
		})
	}
	return out, nil
}

// Certificate CDN 边缘证书（Cloudflare Certificate Pack）。
//
// 与我方部署在源站的证书是两回事：边缘证书由 Cloudflare 自动续期，
// 源站证书要自己管。两者到期时间互相独立，不能用一个推另一个。
type Certificate struct {
	PackID    string
	Type      string // universal / advanced / custom
	Hosts     []string
	Issuer    string
	Status    string
	ExpiresOn string // RFC3339，可能为空
}

// ListCertificates 拉取 zone 的证书包。
//
// 需要 token 具备 Zone·SSL and Certificates·Read。缺权限时 CF 返回 success=false，
// 这里把原因原样带进日志——否则会被当成「这个 zone 没有证书」，那是完全相反的结论。
func (c *Client) ListCertificates(ctx context.Context, zoneID string) ([]Certificate, error) {
	r, err := c.get(ctx, "/zones/"+zoneID+"/ssl/certificate_packs", nil)
	if err != nil {
		log.Printf("[cf-cert] ERROR zone=%s 取证书包失败: %v（若提示权限不足，token 需要 Zone·SSL and Certificates·Read；"+
			"注意：没有该权限时本项为空，不代表该站点没有证书）", zoneID, err)
		return nil, err
	}
	var packs []struct {
		ID                 string   `json:"id"`
		Type               string   `json:"type"`
		Hosts              []string `json:"hosts"`
		Status             string   `json:"status"`
		PrimaryCertificate string   `json:"primary_certificate"`
		Certificates       []struct {
			Issuer    string `json:"issuer"`
			ExpiresOn string `json:"expires_on"`
			Status    string `json:"status"`
		} `json:"certificates"`
	}
	if err := json.Unmarshal(r.Result, &packs); err != nil {
		log.Printf("[cf-cert] ERROR zone=%s 证书包响应结构与预期不符: %v，原始片段: %s",
			zoneID, err, trunc(string(r.Result), 300))
		return nil, fmt.Errorf("解析证书包失败: %w", err)
	}

	out := make([]Certificate, 0, len(packs))
	for _, p := range packs {
		cert := Certificate{PackID: p.ID, Type: p.Type, Hosts: p.Hosts, Status: p.Status}
		// 一个 pack 可能含多张证书（RSA + ECDSA）。取最早到期的那张——
		// 续期出问题时先失效的是它，按最晚的算会漏报。
		for _, cc := range p.Certificates {
			if cc.ExpiresOn == "" {
				continue
			}
			if cert.ExpiresOn == "" || cc.ExpiresOn < cert.ExpiresOn {
				cert.ExpiresOn, cert.Issuer = cc.ExpiresOn, cc.Issuer
			}
		}
		out = append(out, cert)
	}
	log.Printf("[cf-cert] zone=%s 证书包 %d 个", zoneID, len(out))
	return out, nil
}

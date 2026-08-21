package cdnsource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Cloudflare API Token 权限体检。
//
// 为什么要单独做这个：CMDB 里所有 CDN 查询接口读的都是 cdn_rules / cdn_zones 等**库表**，
// 那是上一次同步时落库的快照。改完 CF 权限后去查这些接口，看到的仍是旧结果——
// 曾据此误判过一次「权限没生效」，实际只是没重新同步。
//
// 所以本文件的所有探测**一律实时调 Cloudflare**，不碰数据库。
// 它回答三个库表答不了的问题：
//  1. CMDB 到底在用哪个 token（返回 token id，可与 CF 控制台逐字对照）
//  2. 每一项只读能力是通还是不通
//  3. 不通的那项，缺的是 CF 控制台里的哪一条权限（给原文，照着勾即可）

// ProbeCheck 一项能力的探测结果。
//
// 四态必须分清，不能压成 bool——四种处置完全不同：
//
//	pass            通过
//	no_permission   权限不足，去 CF 控制台按 Need 勾上即可
//	not_applicable  端点不支持这类 token（账号级 vs 用户级），勾权限也没用，无需处理
//	error           其它故障（token 失效、网络不通、CF 抽风），要查或重试
type ProbeCheck struct {
	Name     string `json:"name"`      // 这项能力是干什么的
	Need     string `json:"need"`      // 对应 CF 控制台权限项的原文
	Endpoint string `json:"endpoint"`  // 实际探测的接口
	OK       bool   `json:"ok"`        //
	HTTPCode int    `json:"http_code"` // 0 = 请求没发出去
	Verdict  string `json:"verdict"`   // pass / no_permission / error
	Detail   string `json:"detail"`    // CF 原样返回的错误，不做加工
	Impact   string `json:"impact"`    // 不通会导致哪个 MCP 工具没数据
}

// TokenProbe 一次完整体检。
type TokenProbe struct {
	TokenID    string       `json:"token_id"`    // ⭐ 与 CF 控制台对照用
	TokenState string       `json:"token_state"` // active / disabled / expired
	ExpiresOn  string       `json:"expires_on"`
	NotBefore  string       `json:"not_before"`
	ProbedZone string       `json:"probed_zone"`
	ProbedAt   string       `json:"probed_at"`
	Checks     []ProbeCheck `json:"checks"`
	Summary    string       `json:"summary"`
}

// call 发一次请求并把 HTTP 状态码、CF 错误一并带回。
//
// 与 get() 的区别：get() 只回 error 字符串，状态码被丢掉了，
// 于是「403 权限不足」和「500 CF 抽风」在调用方看来长得一样。体检必须能分开这两种。
func (c *Client) call(ctx context.Context, method, path string, q url.Values, body []byte) (int, *cfResp, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))

	var r cfResp
	if err := json.Unmarshal(raw, &r); err != nil {
		return resp.StatusCode, nil, fmt.Errorf("响应不是合法 JSON: %s", trunc(string(raw), 300))
	}
	if !r.Success {
		return resp.StatusCode, &r, fmt.Errorf("%s", joinErrors(r.Errors))
	}
	return resp.StatusCode, &r, nil
}

// verdictOf 把一次探测归到四态之一。
//
// ⚠️ 不能只看 HTTP 状态码：Cloudflare 的 rulesets 类接口权限不足时回的是
// HTTP 200 + success=false + "request is not authorized"，只看码会误判成通过。
//
// ⚠️ 也不能把「这个端点不支持这类 token」当成权限不足：Cloudflare 的 token 分
// **用户级**和**账号级**两种，`/user/tokens/verify` 与 Page Rules 只认用户级，
// 账号级 token 调它们会回 401 `Invalid API Token` / 400 `does not support account
// owned tokens`。那是端点自身的限制，**勾再多权限也不会变**——
// 报成 no_permission 会让人去控制台白折腾一轮（本工具第一版就犯了这个错）。
func verdictOf(code int, err error) (string, bool) {
	if err == nil {
		return "pass", true
	}
	msg := strings.ToLower(err.Error())

	// 账号级 token 的固有限制，与权限无关
	if strings.Contains(msg, "account owned token") ||
		strings.Contains(msg, "does not support account") ||
		strings.Contains(msg, "invalid api token") {
		return "not_applicable", true
	}

	if code == 403 || code == 401 ||
		strings.Contains(msg, "not authorized") ||
		strings.Contains(msg, "authentication error") ||
		strings.Contains(msg, "permission") {
		return "no_permission", false
	}
	return "error", false
}

func (p *TokenProbe) add(name, need, endpoint, impact string, code int, err error) {
	verdict, ok := verdictOf(code, err)
	ck := ProbeCheck{
		Name: name, Need: need, Endpoint: endpoint, Impact: impact,
		HTTPCode: code, Verdict: verdict, OK: ok,
	}
	if err != nil {
		ck.Detail = trunc(err.Error(), 400)
	}
	p.Checks = append(p.Checks, ck)
}

// ProbeToken 实时体检当前 token。zoneName 为空则自动挑第一个 zone 来探。
func (c *Client) ProbeToken(ctx context.Context, zoneName string) *TokenProbe {
	p := &TokenProbe{ProbedAt: time.Now().Format("2006-01-02 15:04:05")}

	// —— 1. token 身份。这是全场最关键的一条：
	// 「权限改了没生效」十次有九次是改错了 token，而不是权限项选错。
	code, r, err := c.call(ctx, http.MethodGet, "/user/tokens/verify", nil, nil)
	p.add("token 身份与有效性", "（无需额外权限，任何 token 都能调）",
		"GET /user/tokens/verify",
		"拿不到就无法确认 CMDB 用的是哪个 token", code, err)
	if err == nil && r != nil {
		var v struct {
			ID        string `json:"id"`
			Status    string `json:"status"`
			NotBefore string `json:"not_before"`
			ExpiresOn string `json:"expires_on"`
		}
		if json.Unmarshal(r.Result, &v) == nil {
			p.TokenID, p.TokenState = v.ID, v.Status
			p.NotBefore, p.ExpiresOn = v.NotBefore, v.ExpiresOn
		}
	}

	// —— 2. 站点列表：所有 CDN 能力的地基
	q := url.Values{}
	q.Set("per_page", "50")
	code, r, err = c.call(ctx, http.MethodGet, "/zones", q, nil)
	p.add("列站点", "Zone · Zone · Read", "GET /zones",
		"list_cdn_zones 全部为空", code, err)

	var zoneID, zoneNm string
	if err == nil && r != nil {
		var zs []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(r.Result, &zs) == nil {
			for _, z := range zs {
				if zoneName == "" || strings.EqualFold(z.Name, zoneName) {
					zoneID, zoneNm = z.ID, z.Name
					break
				}
			}
		}
	}
	if zoneID == "" {
		p.ProbedZone = zoneName
		p.Summary = "拿不到站点列表或指定站点不存在，后续逐项探测已跳过——先解决上面这条"
		return p
	}
	p.ProbedZone = zoneNm

	// —— 3. 逐项探测只读能力
	code, _, err = c.call(ctx, http.MethodGet, "/zones/"+zoneID+"/dns_records", url.Values{"per_page": {"1"}}, nil)
	p.add("解析记录", "Zone · DNS · Read", "GET /zones/{id}/dns_records",
		"list_cdn_dns 为空、dns_consistency 失效", code, err)

	code, _, err = c.call(ctx, http.MethodGet, "/zones/"+zoneID+"/settings", nil, nil)
	p.add("站点设置（SSL 模式等）", "Zone · Zone Settings · Read", "GET /zones/{id}/settings",
		"list_cdn_zones 的 ssl_mode 为空", code, err)

	code, _, err = c.call(ctx, http.MethodGet, "/zones/"+zoneID+"/pagerules", nil, nil)
	p.add("Page Rules", "Zone · Page Rules · Read", "GET /zones/{id}/pagerules",
		"list_cdn_rules 缺 pagerule 来源的规则", code, err)

	// Rulesets 分两层：列表能过不代表明细能过，这正是本次踩的坑，必须分开探。
	code, r, err = c.call(ctx, http.MethodGet, "/zones/"+zoneID+"/rulesets", nil, nil)
	p.add("规则集列表", "Zone · Config Settings · Read", "GET /zones/{id}/rulesets",
		"list_cdn_rules 缺全部 ruleset", code, err)

	if err == nil && r != nil {
		var sets []struct {
			ID    string `json:"id"`
			Name  string `json:"name"`
			Kind  string `json:"kind"`
			Phase string `json:"phase"`
		}
		if json.Unmarshal(r.Result, &sets) == nil {
			for _, s := range sets {
				if s.Kind != "zone" && s.Kind != "custom" {
					continue // 托管规则集本就不展开
				}
				code, _, err = c.call(ctx, http.MethodGet, "/zones/"+zoneID+"/rulesets/"+s.ID, url.Values{}, nil)
				p.add("规则集明细 · "+s.Phase, needForPhase(s.Phase),
					"GET /zones/{id}/rulesets/{ruleset_id}",
					"list_cdn_rules 里这条只有名字，expression/actions 是「明细获取失败」", code, err)
			}
		}
	}

	// —— 4. GraphQL Analytics：判「边缘→源站」耗时的唯一数据源
	code, err = c.probeGraphQL(ctx, zoneID, gqlHTTPRequests)
	p.add("流量分析（边缘/源站耗时）", "Zone · Zone Analytics · Read", "POST /graphql httpRequestsAdaptiveGroups",
		"拿不到 CF 侧耗时，无法判定延迟卡在 CF 段还是对端", code, err)

	code, err = c.probeGraphQL(ctx, zoneID, gqlFirewallEvents)
	p.add("防火墙事件（WAF/限速/DDoS 缓解）", "Zone · Firewall Services · Read", "POST /graphql firewallEventsAdaptive",
		"无法确认请求有没有被 WAF/限速拦下或挑战", code, err)

	// —— 汇总
	var bad, perm, na int
	for _, ck := range p.Checks {
		switch {
		case ck.Verdict == "not_applicable":
			na++
		case !ck.OK:
			bad++
			if ck.Verdict == "no_permission" {
				perm++
			}
		}
	}

	// token id 拿不到本身就是一条信息：说明这是账号级 token（该端点只认用户级）。
	// 不能只写「token id=」留个空，那会让人以为工具坏了。
	idNote := "token id=" + p.TokenID
	if p.TokenID == "" {
		idNote = "token id 未取到（账号级 token 无法通过 /user/tokens/verify 查询身份，属正常）"
	}
	naNote := ""
	if na > 0 {
		naNote = fmt.Sprintf("，另有 %d 项不适用（账号级 token 的固有限制，与权限无关，无需处理）", na)
	}

	switch {
	case bad == 0:
		p.Summary = fmt.Sprintf("关键能力全部通过（%d 项通过%s）。%s，站点 %s",
			len(p.Checks)-na-bad, naNote, idNote, p.ProbedZone)
	default:
		p.Summary = fmt.Sprintf("%d/%d 项不通过（其中 %d 项是权限不足，按 need 字段照着 CF 控制台勾选即可）%s。%s，站点 %s",
			bad, len(p.Checks), perm, naNote, idNote, p.ProbedZone)
	}
	return p
}

// needForPhase 把 ruleset 的 phase 映射到 CF 控制台的权限项原文。
// 照抄控制台的措辞，用户才能一字不差地找到那一行。
func needForPhase(phase string) string {
	switch phase {
	case "http_ratelimit":
		return "Zone · Zone WAF Rules · Read（限速规则归在 WAF 下）"
	case "http_request_firewall_custom", "http_request_firewall_managed":
		return "Zone · Zone WAF Rules · Read"
	case "http_request_cache_settings":
		return "Zone · Cache Settings · Read"
	case "http_request_dynamic_redirect":
		return "Zone · Dynamic URL Redirects · Read"
	case "http_request_origin":
		return "Zone · Origin · Read"
	case "http_request_transform", "http_response_headers_transform":
		return "Zone · Zone Transform Rules · Read"
	case "http_config_settings":
		return "Zone · Config Settings · Read"
	default:
		return "Zone · Config Settings · Read（该 phase 未收录，以 CF 返回的错误为准）"
	}
}

const (
	gqlHTTPRequests = `query($zone:String!,$since:Time!,$until:Time!){
  viewer{zones(filter:{zoneTag:$zone}){
    httpRequestsAdaptiveGroups(limit:1,filter:{datetime_geq:$since,datetime_leq:$until}){count}
  }}}`

	gqlFirewallEvents = `query($zone:String!,$since:Time!,$until:Time!){
  viewer{zones(filter:{zoneTag:$zone}){
    firewallEventsAdaptive(limit:1,filter:{datetime_geq:$since,datetime_leq:$until}){action}
  }}}`
)

// probeGraphQL 只验「这个查询能不能跑」，不取数据（limit:1）。
//
// ⚠️ GraphQL 与 REST 的失败形态不同：它恒回 HTTP 200，错误藏在 body 的 errors 数组里。
// 照 REST 的写法只看状态码，权限不足会被当成通过。
func (c *Client) probeGraphQL(ctx context.Context, zoneID, query string) (int, error) {
	until := time.Now().UTC()
	since := until.Add(-30 * time.Minute)
	body, _ := json.Marshal(map[string]any{
		"query": query,
		"variables": map[string]string{
			"zone":  zoneID,
			"since": since.Format(time.RFC3339),
			"until": until.Format(time.RFC3339),
		},
	})

	u := c.base + "/graphql"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	var out struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return resp.StatusCode, fmt.Errorf("GraphQL 响应不是合法 JSON: %s", trunc(string(raw), 300))
	}
	if len(out.Errors) > 0 {
		msgs := make([]string, 0, len(out.Errors))
		for _, e := range out.Errors {
			msgs = append(msgs, e.Message)
		}
		return resp.StatusCode, fmt.Errorf("%s", strings.Join(msgs, "; "))
	}
	return resp.StatusCode, nil
}

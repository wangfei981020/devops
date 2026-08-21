package cdnsource

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Cloudflare GraphQL Analytics：查「CF 边缘对这个请求做了什么」。
//
// 为什么必须有这个：CMDB 其余 CDN 接口只能回答「规则是怎么配的」，
// 回答不了「这条规则有没有真的命中过某个请求」。排查跨公网调用超时时，
// 「边缘有没有把它挑战/拦下」和「边缘到源站花了多久」是唯二能把责任分到
// 「对端出网」还是「我方 CDN」的证据——规则台账本身给不出这个判断。
//
// ⚠️ 时间口径：CF GraphQL 一律 UTC。本文件对外同时给 UTC 和北京时间两列，
// 因为业务群里说的都是北京时间，只给 UTC 必然有人算错 8 小时。

// FirewallEvent 一条边缘安全事件。
type FirewallEvent struct {
	DatetimeUTC string `json:"datetime_utc"`
	DatetimeCST string `json:"datetime_cst"` // 北京时间，群里对时用
	Action      string `json:"action"`       // block / managed_challenge / challenge / skip / allow / log
	Source      string `json:"source"`       // ratelimit / firewallCustom / l7ddos / waf ...
	RuleID      string `json:"rule_id"`
	ClientIP    string `json:"client_ip"`
	Country     string `json:"country"`
	Host        string `json:"host"`
	Path        string `json:"path"`
	Method      string `json:"method"`
	UserAgent   string `json:"user_agent"`
	EdgeStatus  int    `json:"edge_response_status"`
}

// FirewallEventQuery 查询条件。Host 为空表示不限主机名。
type FirewallEventQuery struct {
	ZoneID string
	Host   string
	Since  time.Time
	Until  time.Time
	Limit  int
}

const gqlFirewallEventsDetail = `query($zone:String!,$since:Time!,$until:Time!,$limit:Int!){
  viewer{zones(filter:{zoneTag:$zone}){
    firewallEventsAdaptive(
      filter:{datetime_geq:$since,datetime_leq:$until%s}
      limit:$limit
      orderBy:[datetime_DESC]
    ){
      datetime action source ruleId clientIP clientCountryName
      clientRequestHTTPHost clientRequestPath clientRequestHTTPMethodName
      userAgent edgeResponseStatus
    }
  }}}`

// FirewallEvents 拉边缘安全事件明细。
//
// 空结果是有意义的结论（「这段时间边缘没拦过任何请求」），但**必须与
// 「查询失败」区分开**——两者含义完全相反。所以失败一律返回 error，
// 绝不吞掉错误返回空切片。
func (c *Client) FirewallEvents(ctx context.Context, q FirewallEventQuery) ([]FirewallEvent, error) {
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 200
	}
	hostFilter := ""
	vars := map[string]any{
		"zone":  q.ZoneID,
		"since": q.Since.UTC().Format(time.RFC3339),
		"until": q.Until.UTC().Format(time.RFC3339),
		"limit": q.Limit,
	}
	if strings.TrimSpace(q.Host) != "" {
		hostFilter = ",clientRequestHTTPHost:$host"
		vars["host"] = q.Host
	}

	query := fmt.Sprintf(gqlFirewallEventsDetail, hostFilter)
	if hostFilter != "" {
		query = strings.Replace(query,
			"query($zone:String!,$since:Time!,$until:Time!,$limit:Int!)",
			"query($zone:String!,$since:Time!,$until:Time!,$limit:Int!,$host:String!)", 1)
	}

	raw, err := c.graphql(ctx, query, vars)
	if err != nil {
		return nil, err
	}

	var out struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					Events []struct {
						Datetime   string `json:"datetime"`
						Action     string `json:"action"`
						Source     string `json:"source"`
						RuleID     string `json:"ruleId"`
						ClientIP   string `json:"clientIP"`
						Country    string `json:"clientCountryName"`
						Host       string `json:"clientRequestHTTPHost"`
						Path       string `json:"clientRequestPath"`
						Method     string `json:"clientRequestHTTPMethodName"`
						UserAgent  string `json:"userAgent"`
						EdgeStatus int    `json:"edgeResponseStatus"`
					} `json:"firewallEventsAdaptive"`
				} `json:"zones"`
			} `json:"viewer"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析 firewallEventsAdaptive 失败: %w", err)
	}
	if len(out.Data.Viewer.Zones) == 0 {
		return nil, fmt.Errorf("GraphQL 没有返回该 zone 的数据——检查 zone id 是否正确、token 是否覆盖该站点")
	}

	evs := out.Data.Viewer.Zones[0].Events
	res := make([]FirewallEvent, 0, len(evs))
	for _, e := range evs {
		res = append(res, FirewallEvent{
			DatetimeUTC: e.Datetime, DatetimeCST: toCST(e.Datetime),
			Action: e.Action, Source: e.Source, RuleID: e.RuleID,
			ClientIP: e.ClientIP, Country: e.Country, Host: e.Host,
			Path: e.Path, Method: e.Method, UserAgent: trunc(e.UserAgent, 120),
			EdgeStatus: e.EdgeStatus,
		})
	}
	return res, nil
}

// toCST 把 CF 的 UTC 时间串转成北京时间串。转不了就原样返回，
// 不编造时间——宁可显示成 UTC 让人发现，也不能给个错的北京时间。
func toCST(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
}

// graphql 发一次 GraphQL 查询，返回原始响应体。
//
// ⚠️ GraphQL 与 REST 的失败形态不同：它恒回 HTTP 200，错误在 body 的 errors 数组里。
// 按 REST 的写法只看状态码，权限不足和语法错误都会被当成成功。
func (c *Client) graphql(ctx context.Context, query string, vars map[string]any) ([]byte, error) {
	body, err := json.Marshal(map[string]any{"query": query, "variables": vars})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))

	var probe struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("GraphQL 响应不是合法 JSON(HTTP %d): %s", resp.StatusCode, trunc(string(raw), 300))
	}
	if len(probe.Errors) > 0 {
		msgs := make([]string, 0, len(probe.Errors))
		for _, e := range probe.Errors {
			msgs = append(msgs, e.Message)
		}
		return nil, fmt.Errorf("GraphQL 报错(HTTP %d): %s", resp.StatusCode, strings.Join(msgs, "; "))
	}
	return raw, nil
}

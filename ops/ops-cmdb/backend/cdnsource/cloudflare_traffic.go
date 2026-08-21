package cdnsource

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Cloudflare 逐条请求时序：回答「CF 边缘是什么时候收到这个请求的」。
//
// 这是排查「对方说访问我们超时」时唯一能把责任切开的证据。
// 安全事件（firewallEventsAdaptive）只能证明「边缘有没有拦它」，
// 证明不了「边缘有没有压着它」——两件事结论相反，不能混为一谈：
//
//	对方发出 ──①── CF 边缘收到 ──②── 回源到我方 ──③── 我方应用读到
//
// 我方日志只能测出「①+②+③」的总和。只有拿到边缘的 edgeStartTimestamp，
// 才能把 ① 和 ②+③ 分开：
//   - 边缘收到的时刻 ≈ 我方应用读到的时刻  →  慢在 ①，即对端出网侧，与 CF 无关
//   - 边缘收到的时刻 ≈ 对方发出的时刻      →  慢在 ②，即 CF 内部滞留，是 CF 的问题

// HTTPRequestQuery 查询条件。Fields 为空则用 DefaultTrafficFields。
type HTTPRequestQuery struct {
	ZoneID      string
	Host        string
	Path        string
	ClientIP    string
	Since       time.Time
	Until       time.Time
	Limit       int
	MinOriginMs int      // 只看回源耗时超过该值的（0=不过滤）
	Fields      []string // 运行时覆盖字段列表，省得为改一个字段名重新发版
}

// DefaultTrafficFields 默认取的字段。
//
// ⚠️ GraphQL 与 Logpush 的字段命名**不是一套**：Logpush 里叫 edgeStartTimestamp，
// GraphQL 里叫 datetime。第一版照 Logpush 的名字写，直接被 CF 回
// `unknown field "edgeStartTimestamp"` 打回来。
//
// 这里只放**已验证可用**的字段（前 8 个与 firewallEventsAdaptive 同名且已实测通过）。
// 想试更多字段不必改代码——用 fields 参数在运行时覆盖，或先用 introspect 查有哪些。
var DefaultTrafficFields = []string{
	"datetime", // ⭐ 边缘收到该请求的时刻，本工具的核心
	"clientIP", "clientCountryName",
	"clientRequestHTTPHost", "clientRequestPath", "clientRequestHTTPMethodName",
	"userAgent", "edgeResponseStatus",
	"originResponseDurationMs", // 回源耗时，判我方源站快慢
}

const gqlHTTPRequestsDetail = `query($zone:String!,$since:Time!,$until:Time!,$limit:Int!%s){
  viewer{zones(filter:{zoneTag:$zone}){
    httpRequestsAdaptive(
      filter:{datetime_geq:$since,datetime_leq:$until%s}
      limit:$limit
      orderBy:[datetime_DESC]
    ){
      %s
    }
  }}}`

// IntrospectFields 查某个 GraphQL 类型有哪些字段。
//
// 存在的理由：字段名靠猜就要反复发版，一次猜错就是一轮部署。
// 有了它，字段对不上时当场能查出正确的名字。
func (c *Client) IntrospectFields(ctx context.Context, typeName string) ([]string, error) {
	if strings.TrimSpace(typeName) == "" {
		typeName = "Zone"
	}
	q := `query($t:String!){__type(name:$t){name fields{name}}}`
	raw, err := c.graphql(ctx, q, map[string]any{"t": typeName})
	if err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
			Type *struct {
				Name   string `json:"name"`
				Fields []struct {
					Name string `json:"name"`
				} `json:"fields"`
			} `json:"__type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析 introspection 失败: %w", err)
	}
	if out.Data.Type == nil {
		return nil, fmt.Errorf("GraphQL 里没有类型 %q——换个类型名再试（如 Zone / ZoneHttpRequestsAdaptive）", typeName)
	}
	names := make([]string, 0, len(out.Data.Type.Fields))
	for _, f := range out.Data.Type.Fields {
		names = append(names, f.Name)
	}
	return names, nil
}

// HTTPRequests 拉逐条请求时序。
//
// ⚠️ 该数据集按计划有采样与保留期限制；空结果**不代表没有请求**，
// 可能是查询窗口超出保留期。调用方必须先用近期窗口自证能查到数据。
func (c *Client) HTTPRequests(ctx context.Context, q HTTPRequestQuery) ([]map[string]any, error) {
	if q.Limit <= 0 || q.Limit > 1000 {
		q.Limit = 100
	}
	fields := q.Fields
	if len(fields) == 0 {
		fields = DefaultTrafficFields
	}

	var varDecl, filters []string
	vars := map[string]any{
		"zone":  q.ZoneID,
		"since": q.Since.UTC().Format(time.RFC3339),
		"until": q.Until.UTC().Format(time.RFC3339),
		"limit": q.Limit,
	}
	add := func(decl, filter, key string, val any) {
		varDecl = append(varDecl, decl)
		filters = append(filters, filter)
		vars[key] = val
	}
	if h := strings.TrimSpace(q.Host); h != "" {
		add(",$host:String!", ",clientRequestHTTPHost:$host", "host", h)
	}
	if p := strings.TrimSpace(q.Path); p != "" {
		add(",$path:String!", ",clientRequestPath:$path", "path", p)
	}
	if ip := strings.TrimSpace(q.ClientIP); ip != "" {
		add(",$cip:String!", ",clientIP:$cip", "cip", ip)
	}
	if q.MinOriginMs > 0 {
		add(",$mindur:uint32!", ",originResponseDurationMs_geq:$mindur", "mindur", q.MinOriginMs)
	}

	query := fmt.Sprintf(gqlHTTPRequestsDetail,
		strings.Join(varDecl, ""), strings.Join(filters, ""), strings.Join(fields, " "))
	raw, err := c.graphql(ctx, query, vars)
	if err != nil {
		return nil, err
	}

	// 字段可由调用方指定，所以不能用固定 struct 承接——原样收下再加工。
	var out struct {
		Data struct {
			Viewer struct {
				Zones []struct {
					Reqs []map[string]any `json:"httpRequestsAdaptive"`
				} `json:"zones"`
			} `json:"viewer"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("解析 httpRequestsAdaptive 失败: %w", err)
	}
	if len(out.Data.Viewer.Zones) == 0 {
		return nil, fmt.Errorf("GraphQL 没有返回该 zone 的数据——检查 zone id 与 token 覆盖范围")
	}

	rs := out.Data.Viewer.Zones[0].Reqs
	res := make([]map[string]any, 0, len(rs))
	for _, r := range rs {
		// datetime 是本工具的核心（边缘收到请求的时刻），额外给一列北京时间：
		// 群里对时都说北京时间，只给 UTC 必然有人算错 8 小时。
		if v, ok := r["datetime"].(string); ok {
			r["datetime_cst"] = toCST(v)
		}
		res = append(res, r)
	}
	return res, nil
}

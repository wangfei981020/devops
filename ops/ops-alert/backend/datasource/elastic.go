package datasource

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// elasticAdapter 同时服务 Elasticsearch 7/8 与 OpenSearch。
// 三者的 _search 接口在本产品用到的范围内兼容；差异只在认证
// （ES 8 的 API Key）与部分响应字段（total 的对象/数字形态）。
type elasticAdapter struct {
	cfg    Config
	client *http.Client
}

func newElastic(cfg Config) *elasticAdapter {
	tr := &http.Transport{}
	if cfg.SkipTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402
	}
	return &elasticAdapter{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second, Transport: tr}}
}

func (e *elasticAdapter) Kind() string { return e.cfg.Kind }

func (e *elasticAdapter) Probe(ctx context.Context) error {
	req, err := e.newRequest(ctx, http.MethodGet, "/", nil)
	if err != nil {
		return err
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// timeField 返回时间字段名。默认 @timestamp —— 但绝不静默假设：
// 字段名错的话查询恒返回 0 条，表现为「一直没有告警」，
// 这正是本产品最想根治的静默失败，所以数据源配置里必须能显式指定。
func (e *elasticAdapter) timeField() string {
	if e.cfg.Spec.TimeField != "" {
		return e.cfg.Spec.TimeField
	}
	return "@timestamp"
}

func (e *elasticAdapter) Query(ctx context.Context, q Query) (*Result, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 500
	}
	index := e.cfg.Spec.IndexPattern
	if index == "" {
		index = "*"
	}

	// Expr 支持两种写法：以 { 开头当作原生 DSL query 子句，否则当 query_string。
	// 不做"智能猜测"以外的加工——运维写的 DSL 必须原样送达，
	// 被平台悄悄改写过的查询是排障时最难对齐的东西。
	var userQuery json.RawMessage
	expr := strings.TrimSpace(q.Expr)
	if strings.HasPrefix(expr, "{") {
		userQuery = json.RawMessage(expr)
	} else {
		qs := map[string]any{"query_string": map[string]any{"query": expr, "analyze_wildcard": true}}
		b, _ := json.Marshal(qs)
		userQuery = b
	}

	body := map[string]any{
		"size": limit,
		"sort": []any{map[string]any{e.timeField(): map[string]string{"order": "desc"}}},
		"query": map[string]any{
			"bool": map[string]any{
				"filter": []any{
					json.RawMessage(userQuery),
					map[string]any{"range": map[string]any{e.timeField(): map[string]any{
						"gte":    q.From.UTC().Format(time.RFC3339Nano),
						"lte":    q.To.UTC().Format(time.RFC3339Nano),
						"format": "strict_date_optional_time",
					}}},
				},
			},
		},
		// track_total_hits：不开的话 total 在超过 10000 时被截断成 10000，
		// 「日志量突变」的比例会算错，而且错得毫无征兆。
		"track_total_hits": true,
	}

	buf, _ := json.Marshal(body)
	req, err := e.newRequest(ctx, http.MethodPost, "/"+index+"/_search", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	started := time.Now()
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询失败: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var parsed struct {
		Hits struct {
			Total any `json:"total"` // ES7+ 是对象 {value,relation}，老版本是数字
			Hits  []struct {
				Source map[string]any `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("解析响应: %w", err)
	}

	res := &Result{TookMs: int(time.Since(started).Milliseconds())}
	for _, h := range parsed.Hits.Hits {
		hit := Hit{Fields: h.Source, Labels: map[string]string{}}
		if ts, ok := h.Source[e.timeField()].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				hit.Time = t
			}
		}
		// 分组维度从 _source 里取，支持 a.b.c 点号路径
		for _, g := range q.GroupBy {
			if v, ok := lookupPath(h.Source, g); ok {
				hit.Labels[g] = fmt.Sprint(v)
			}
		}
		if msg, ok := h.Source["message"].(string); ok {
			hit.Line = msg
		}
		res.Hits = append(res.Hits, hit)
	}
	res.Total = parseESTotal(parsed.Hits.Total, len(res.Hits))
	res.Truncated = len(res.Hits) >= limit
	res.Groups = CountByGroup(res.Hits, q.GroupBy)
	return res, nil
}

// parseESTotal 兼容 total 的两种形态。判断不了时回落到实际返回条数，
// 而不是 0：0 会让「命中数 ≥ 阈值」永远不成立，规则静静地失效。
func parseESTotal(v any, fallback int) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case map[string]any:
		if val, ok := t["value"].(float64); ok {
			return int64(val)
		}
	}
	return int64(fallback)
}

// lookupPath 按 a.b.c 取嵌套字段。
func lookupPath(src map[string]any, path string) (any, bool) {
	cur := any(src)
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

func (e *elasticAdapter) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	// 多地址时取第一个。真正的多节点负载留给二期（需要健康探测与摘除，
	// 半吊子的轮询在节点挂掉时反而比单点更难排查）。
	endpoint := strings.TrimSuffix(strings.Split(e.cfg.Endpoint, ",")[0], "/")
	req, err := http.NewRequestWithContext(ctx, method, endpoint+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	switch {
	case e.cfg.Auth.APIKey != "":
		req.Header.Set("Authorization", "ApiKey "+e.cfg.Auth.APIKey)
	case e.cfg.Auth.Username != "":
		req.SetBasicAuth(e.cfg.Auth.Username, e.cfg.Auth.Password)
	}
	return req, nil
}

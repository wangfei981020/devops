package datasource

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type lokiAdapter struct {
	cfg    Config
	client *http.Client
}

func newLoki(cfg Config) *lokiAdapter {
	tr := &http.Transport{}
	if cfg.SkipTLS {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 —— 由数据源显式开启，界面上有明示
	}
	return &lokiAdapter{
		cfg: cfg,
		// 超时必须有：Loki 在大时间范围上会一直算下去，
		// 没有超时的话检测协程会被一条慢查询占住，后续周期全部堆积。
		client: &http.Client{Timeout: 30 * time.Second, Transport: tr},
	}
}

func (l *lokiAdapter) Kind() string { return KindLoki }

func (l *lokiAdapter) Probe(ctx context.Context) error {
	req, err := l.newRequest(ctx, "/loki/api/v1/labels", url.Values{})
	if err != nil {
		return err
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return fmt.Errorf("连接失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (l *lokiAdapter) Query(ctx context.Context, q Query) (*Result, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 1000
	}
	params := url.Values{}
	params.Set("query", q.Expr)
	params.Set("start", strconv.FormatInt(q.From.UnixNano(), 10))
	params.Set("end", strconv.FormatInt(q.To.UnixNano(), 10))
	params.Set("limit", strconv.Itoa(limit))
	// backward = 从新到旧。命中被截断时留下的是最近的样本，
	// 而排障要看的正是最近发生了什么。
	params.Set("direction", "backward")

	req, err := l.newRequest(ctx, "/loki/api/v1/query_range", params)
	if err != nil {
		return nil, err
	}
	started := time.Now()
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("查询失败: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("读取响应: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		// 把 Loki 的原文带上：LogQL 语法错误的提示很具体，
		// 吞掉它只留「查询失败」会让人无从改起。
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var raw struct {
		Data struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Stream map[string]string `json:"stream"`
				Values [][2]string       `json:"values"`
				// 指标型 LogQL（rate/count_over_time）返回 values 为 [ts, "值"]，
				// 结构与日志流相同，靠 resultType 区分。
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("解析响应: %w", err)
	}

	res := &Result{Groups: map[string]int{}, TookMs: int(time.Since(started).Milliseconds())}
	isMatrix := raw.Data.ResultType == "matrix"
	for _, stream := range raw.Data.Result {
		for _, v := range stream.Values {
			ts := parseLokiTS(v[0], isMatrix)
			h := Hit{Time: ts, Labels: stream.Stream}
			if isMatrix {
				f, _ := strconv.ParseFloat(v[1], 64)
				h.Value = f
			} else {
				h.Line = v[1]
			}
			res.Hits = append(res.Hits, h)
		}
	}
	res.Total = int64(len(res.Hits))
	res.Truncated = len(res.Hits) >= limit
	res.Groups = CountByGroup(res.Hits, q.GroupBy)
	return res, nil
}

// parseLokiTS：日志流的时间戳是纳秒字符串，矩阵是秒（可能带小数）。
// 两者混用会让时间差出 10^9 倍——事件的「首次发生」会落到 1970 年，
// 而界面上只是显示得怪，不会报错。
func parseLokiTS(s string, matrix bool) time.Time {
	if matrix {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return time.Time{}
		}
		return time.Unix(int64(f), 0)
	}
	ns, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (l *lokiAdapter) newRequest(ctx context.Context, path string, params url.Values) (*http.Request, error) {
	endpoint := strings.TrimSuffix(l.cfg.Endpoint, "/") + path
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	if l.cfg.Auth.Username != "" {
		req.SetBasicAuth(l.cfg.Auth.Username, l.cfg.Auth.Password)
	}
	if l.cfg.Auth.Token != "" {
		req.Header.Set("Authorization", "Bearer "+l.cfg.Auth.Token)
	}
	if l.cfg.Auth.OrgID != "" {
		req.Header.Set("X-Scope-OrgID", l.cfg.Auth.OrgID)
	}
	return req, nil
}

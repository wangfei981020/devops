// Package n9e 夜莺（Nightingale）告警平台的只读客户端。
//
//	CMDB 接夜莺只做一件事：把告警拉进来，和资产、变更、到期放在同一条时间线上。
//	排障时最费时间的往往不是"看告警"，而是在告警系统和资产系统之间来回切、
//	自己在脑子里做关联。事件中心已经有到期/变更/同步失败/K8s Warning，
//	告警是缺的最后一块。
//
//	实现从 opsplatform-confluence-backend/n9e/client.go 搬过来并做了两处补充：
//	  - 加了当前活跃告警（confluence 只用历史事件出周报，CMDB 排障要看"现在还在响的"）
//	  - 拉取带上下文超时（原版没有，一个慢的夜莺会把调用方拖住）
package n9e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func New(url, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(url, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 20 * time.Second},
	}
}

// AlertEvent 一条告警。字段名对齐夜莺 v6/v7 的返回。
type AlertEvent struct {
	ID               int64             `json:"id"`
	Cate             string            `json:"cate"`
	IsRecovered      int               `json:"is_recovered"`
	Cluster          string            `json:"cluster"`
	GroupName        string            `json:"group_name"`
	RuleID           int64             `json:"rule_id"`
	RuleName         string            `json:"rule_name"`
	RuleNote         string            `json:"rule_note"`
	Severity         int               `json:"severity"` // 1=紧急 2=告警 3=提醒
	TargetIdent      string            `json:"target_ident"`
	TriggerTime      int64             `json:"trigger_time"`
	TriggerValue     string            `json:"trigger_value"`
	RecoverTime      int64             `json:"recover_time"`
	Tags             []string          `json:"tags"`
	Annotations      map[string]string `json:"annotations"`
	FirstTriggerTime int64             `json:"first_trigger_time"`
}

// SeverityLabel 夜莺的 severity 是数字，直接展示给人看没有意义
func (e AlertEvent) SeverityLabel() string {
	switch e.Severity {
	case 1:
		return "critical"
	case 2:
		return "warning"
	default:
		return "info"
	}
}

// Object 告警对象：优先用监控对象标识，退化到规则名
func (e AlertEvent) Object() string {
	if e.TargetIdent != "" {
		return e.TargetIdent
	}
	if len(e.Tags) > 0 {
		return strings.Join(e.Tags, ",")
	}
	return e.RuleName
}

type listResp struct {
	Dat struct {
		List  []AlertEvent `json:"list"`
		Total int64        `json:"total"`
	} `json:"dat"`
	Err string `json:"err"`
}

func (c *Client) get(ctx context.Context, path string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL+path, nil)
	if err != nil {
		return nil, 0, err
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
		// 夜莺部分版本用这个头做 API 鉴权，两个都带上兼容性最好
		req.Header.Set("X-User-Token", c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, resp.StatusCode, nil
}

// TestConnection 连通性验证（接入配置页的「测试」按钮用）
func (c *Client) TestConnection(ctx context.Context) error {
	b, status, err := c.get(ctx, "/api/n9e/alert-cur-events/list?limit=1&p=1")
	if err != nil {
		return fmt.Errorf("连接失败：%v", err)
	}
	if status == 401 || status == 403 {
		return fmt.Errorf("鉴权失败（HTTP %d）：检查 Token 是否有效", status)
	}
	if status != 200 {
		return fmt.Errorf("夜莺返回 HTTP %d：%.120s", status, string(b))
	}
	var r listResp
	if err := json.Unmarshal(b, &r); err != nil {
		return fmt.Errorf("响应不是预期的 JSON（可能地址填成了 UI 而不是 API）：%.120s", string(b))
	}
	if r.Err != "" {
		return fmt.Errorf("夜莺返回错误：%s", r.Err)
	}
	return nil
}

// CurrentAlerts 当前活跃告警（未恢复的）。排障时最先要看的就是这个。
func (c *Client) CurrentAlerts(ctx context.Context, limit int) ([]AlertEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	b, status, err := c.get(ctx, fmt.Sprintf("/api/n9e/alert-cur-events/list?limit=%d&p=1", limit))
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("夜莺 HTTP %d：%.120s", status, string(b))
	}
	var r listResp
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("解析失败：%v", err)
	}
	if r.Err != "" {
		return nil, fmt.Errorf("夜莺返回错误：%s", r.Err)
	}
	return r.Dat.List, nil
}

// HistoryAlerts 历史告警（含已恢复）。分页拉，但设了硬上限——
// 时间窗开大时夜莺可能有几万条，全拉回来既慢又没人看得完。
func (c *Client) HistoryAlerts(ctx context.Context, stime, etime int64, maxTotal int) ([]AlertEvent, int64, error) {
	if maxTotal <= 0 {
		maxTotal = 1000
	}
	var all []AlertEvent
	var total int64
	const pageSize = 500

	for page := 1; ; page++ {
		select {
		case <-ctx.Done():
			// 超时就返回已拿到的部分，并把 total 带回去让调用方知道没取全
			return all, total, ctx.Err()
		default:
		}
		b, status, err := c.get(ctx, fmt.Sprintf(
			"/api/n9e/alert-his-events/list?stime=%d&etime=%d&limit=%d&p=%d", stime, etime, pageSize, page))
		if err != nil {
			return all, total, err
		}
		if status != 200 {
			return all, total, fmt.Errorf("夜莺 HTTP %d：%.120s", status, string(b))
		}
		var r listResp
		if err := json.Unmarshal(b, &r); err != nil {
			return all, total, fmt.Errorf("解析失败：%v", err)
		}
		if r.Err != "" {
			return all, total, fmt.Errorf("夜莺返回错误：%s", r.Err)
		}
		total = r.Dat.Total
		all = append(all, r.Dat.List...)
		if len(r.Dat.List) < pageSize || len(all) >= int(total) || len(all) >= maxTotal {
			break
		}
	}
	return all, total, nil
}

package n9e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client 夜莺 N9E API 客户端
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// NewClientFromParams 从参数创建 N9E 客户端
func NewClientFromParams(url, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(url, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// AlertEvent 历史告警事件
type AlertEvent struct {
	ID               int64             `json:"id"`
	Cate             string            `json:"cate"`
	IsRecovered      int               `json:"is_recovered"`
	DatasourceID     int64             `json:"datasource_id"`
	Cluster          string            `json:"cluster"`
	GroupID          int64             `json:"group_id"`
	GroupName        string            `json:"group_name"`
	Hash             string            `json:"hash"`
	RuleID           int64             `json:"rule_id"`
	RuleName         string            `json:"rule_name"`
	RuleNote         string            `json:"rule_note"`
	Severity         int               `json:"severity"`
	TargetIdent      string            `json:"target_ident"`
	TriggerTime      int64             `json:"trigger_time"`
	TriggerValue     string            `json:"trigger_value"`
	RecoverTime      int64             `json:"recover_time"`
	LastEvalTime     int64             `json:"last_eval_time"`
	Tags             []string          `json:"tags"`
	Annotations      map[string]string `json:"annotations"`
	FirstTriggerTime int64             `json:"first_trigger_time"`
}

// AlertHisEventsResp 历史告警响应
type AlertHisEventsResp struct {
	Dat struct {
		List  []AlertEvent `json:"list"`
		Total int64        `json:"total"`
	} `json:"dat"`
	Err string `json:"err"`
}

// Do 发送认证请求到 N9E
func (c *Client) Do(method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, c.BaseURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-User-Token", c.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return c.HTTP.Do(req)
}

// Get 发送 GET 请求
func (c *Client) Get(path string) ([]byte, int, error) {
	resp, err := c.Do("GET", path, nil)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// TestConnection 测试 N9E 连接
func (c *Client) TestConnection() error {
	data, status, err := c.Get("/api/n9e/alert-his-events/list?limit=1&hours=1")
	if err != nil {
		return fmt.Errorf("无法连接到夜莺服务器: %v", err)
	}
	if status == 401 || status == 403 {
		return fmt.Errorf("认证失败 (HTTP %d)，请检查 Token 是否正确", status)
	}
	if status != 200 {
		return fmt.Errorf("连接失败 (HTTP %d): %s", status, string(data))
	}
	var resp AlertHisEventsResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("响应解析失败: %v", err)
	}
	if resp.Err != "" {
		return fmt.Errorf("N9E 返回错误: %s", resp.Err)
	}
	return nil
}

// FetchAlertHisEvents 获取历史告警事件（分页获取全部）
func (c *Client) FetchAlertHisEvents(stime, etime int64) ([]AlertEvent, int64, error) {
	var allEvents []AlertEvent
	var total int64
	limit := 500
	page := 1

	for {
		path := fmt.Sprintf("/api/n9e/alert-his-events/list?stime=%d&etime=%d&limit=%d&p=%d",
			stime, etime, limit, page)
		data, status, err := c.Get(path)
		if err != nil {
			return nil, 0, fmt.Errorf("请求失败: %v", err)
		}
		if status != 200 {
			return nil, 0, fmt.Errorf("N9E API 错误 (HTTP %d): %s", status, string(data))
		}

		var resp AlertHisEventsResp
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, 0, fmt.Errorf("响应解析失败: %v", err)
		}
		if resp.Err != "" {
			return nil, 0, fmt.Errorf("N9E 返回错误: %s", resp.Err)
		}

		total = resp.Dat.Total
		allEvents = append(allEvents, resp.Dat.List...)

		if len(resp.Dat.List) < limit || len(allEvents) >= int(total) {
			break
		}
		page++
	}

	return allEvents, total, nil
}

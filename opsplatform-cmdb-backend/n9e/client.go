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

// AlertEvent 一条告警。
//
//	字段类型按**生产实例的真实返回**对齐（2026-08-04 实测 n9e-extra）。
//	⚠️ is_recovered 是 **bool** 不是 int——从 confluence 那份抄来的定义写的是 int，
//	一接真实数据就 `cannot unmarshal bool into Go struct field`。
//	夜莺不同版本字段类型有出入，抄定义务必用真实响应验一遍。
type AlertEvent struct {
	ID          int64  `json:"id"`
	Cate        string `json:"cate"`
	IsRecovered bool   `json:"is_recovered"`
	Cluster     string `json:"cluster"`
	GroupName   string `json:"group_name"`
	RuleID      int64  `json:"rule_id"`
	RuleName    string `json:"rule_name"`
	RuleNote    string `json:"rule_note"`
	Severity    int    `json:"severity"` // 1=紧急 2=告警 3=提醒
	// 实测生产上 target_ident 多为空，真正能标识对象的在 tags_map / annotations 里
	TargetIdent      string            `json:"target_ident"`
	TriggerTime      int64             `json:"trigger_time"`
	TriggerValue     string            `json:"trigger_value"`
	RecoverTime      int64             `json:"recover_time"`
	Tags             []string          `json:"tags"`     // ["cluster=ecs","env=prod",...]
	TagsMap          map[string]string `json:"tags_map"` // 同上但已拆成 kv，取 instance 用这个
	Annotations      map[string]string `json:"annotations"`
	FirstTriggerTime int64             `json:"first_trigger_time"`
	NotifyCurNumber  int               `json:"notify_cur_number"` // 已通知次数，反映告警持续了多久
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

// Object 告警对象：这条告警到底在说哪台机器/哪个实例。
//
//	实测生产数据里 target_ident 基本是空的，有用的标识散在 tags_map
//	和 annotations 里。直接把整串 tags 拼出来（"cluster=ecs,env=prod,instance=..."）
//	在列表里没法看，所以按有用程度依次取。
func (e AlertEvent) Object() string {
	if e.TargetIdent != "" {
		return e.TargetIdent
	}
	// 业务标识优先于基础设施标识。
	//
	//	实测教训：域名到期告警的 instance 是 domain-exporter 的地址
	//	（172.16.14.16:8080），30 条域名告警全长一个样，完全没法排障；
	//	真正要看的 g01prod.com 在 tags_map.domain 里。
	//	证书类同理。所以先找"这条告警在说哪个业务对象"，
	//	找不到才退回"哪台机器报出来的"。
	// ① 明确的业务对象标签
	for _, k := range []string{"domain", "cn", "certificate", "cert", "pod", "deployment", "service", "node", "database", "topic", "name"} {
		if v := e.TagsMap[k]; v != "" {
			return v
		}
	}
	// ② annotations 里的标识优先于 tags 里的 instance。
	//
	//	annotations 是告警规则作者写给人看的，通常带业务含义；
	//	tags.instance 往往只是 exporter 的地址。实测视频流告警：
	//	  tags.instance        = 10.170.96.153:8080     ← 一个 exporter 上几十条流，全一样
	//	  annotations.instance = g01-source_..._N7103   ← 这才是出问题的那条流
	//	按 tags.instance 取，列表会出现十几行长得一模一样的告警。
	for _, k := range []string{"instance", "hostname", "host", "target"} {
		if v := e.Annotations[k]; v != "" {
			return v
		}
	}
	// ③ 兜底：exporter 地址
	for _, k := range []string{"instance", "ident", "host", "hostname"} {
		if v := e.TagsMap[k]; v != "" {
			return v
		}
	}
	if len(e.Tags) > 0 {
		return strings.Join(e.Tags, ",")
	}
	return e.RuleName
}

// BizTags 除去噪音后的业务标签，给列表当次要信息展示。
// 过滤掉 instance/ident 这类已经在 Object 里体现的，避免重复占地方。
func (e AlertEvent) BizTags() map[string]string {
	skip := map[string]bool{"instance": true, "ident": true, "host": true, "hostname": true, "__name__": true}
	out := map[string]string{}
	for k, v := range e.TagsMap {
		if !skip[k] && v != "" {
			out[k] = v
		}
	}
	return out
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
//
//	返回的 total 是**夜莺侧的真实总数**，不是本次取回的条数。
//	两者必须分开：limit=8 时取回 8 条但线上有 298 条，
//	把 8 当成总数就是把"只看了个开头"显示成"总共就这些"。
func (c *Client) CurrentAlerts(ctx context.Context, limit int) ([]AlertEvent, int64, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	b, status, err := c.get(ctx, fmt.Sprintf("/api/n9e/alert-cur-events/list?limit=%d&p=1", limit))
	if err != nil {
		return nil, 0, err
	}
	if status != 200 {
		return nil, 0, fmt.Errorf("夜莺 HTTP %d：%.120s", status, string(b))
	}
	var r listResp
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, 0, fmt.Errorf("解析失败：%v", err)
	}
	if r.Err != "" {
		return nil, 0, fmt.Errorf("夜莺返回错误：%s", r.Err)
	}
	return r.Dat.List, r.Dat.Total, nil
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

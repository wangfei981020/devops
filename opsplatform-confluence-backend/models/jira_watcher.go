package models

import (
	"encoding/json"
	"time"
)

// NotifyRule 监听规则
type NotifyRule struct {
	ID          int             `json:"id"`
	Name        string          `json:"name"`
	Enabled     bool            `json:"enabled"`
	JiraConnID  int             `json:"jira_conn_id"`
	LarkConnID  int             `json:"lark_conn_id"`
	ProjectKeys []string        `json:"project_keys"`
	IssueTypes  []string        `json:"issue_types"`
	ExtraJQL    string          `json:"extra_jql"`
	Targets     []RuleTarget    `json:"targets"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

// RuleTarget 规则下的目标状态
type RuleTarget struct {
	ID                  int    `json:"id"`
	RuleID              int    `json:"rule_id"`
	StatusName          string `json:"status_name"`
	UseDefaultTemplate  bool   `json:"use_default_template"`
	CustomTemplate      string `json:"custom_template"`
	StartupLookbackMin  int    `json:"startup_lookback_min"`
}

// StatusUserMap (项目+状态) → @ 人 映射
type StatusUserMap struct {
	ID          int       `json:"id"`
	ProjectKey  string    `json:"project_key"`
	StatusName  string    `json:"status_name"`
	AtUsers     []AtUser  `json:"at_users"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AtUser 飞书人员
type AtUser struct {
	OpenID string `json:"open_id"`
	UserID string `json:"user_id,omitempty"`
	Mobile string `json:"mobile,omitempty"`
	Email  string `json:"email,omitempty"`
	Name   string `json:"name"`
}

// NotifyHistory 通知历史
type NotifyHistory struct {
	ID            int64     `json:"id"`
	RuleID        int       `json:"rule_id"`
	TargetID      int       `json:"target_id"`
	IssueKey      string    `json:"issue_key"`
	IssueSummary  string    `json:"issue_summary"`
	FromStatus    string    `json:"from_status"`
	ToStatus      string    `json:"to_status"`
	TransitionAt  time.Time `json:"transition_at"`
	NotifiedAt    time.Time `json:"notified_at"`
	Status        string    `json:"status"` // success/failed/pending
	RetryCount    int       `json:"retry_count"`
	LastError     string    `json:"last_error"`
	LarkMessageID string    `json:"lark_message_id"`
}

// StatusTransition 解析自 Jira changelog 的状态流转事件
type StatusTransition struct {
	From string
	To   string
	At   time.Time
}

// SendTask 发送队列任务
type SendTask struct {
	Rule         *NotifyRule
	Target       *RuleTarget
	IssueKey     string
	IssueSummary string
	ProjectKey   string
	FromStatus   string
	ToStatus     string
	TransitionAt time.Time
	JiraIssueURL string
	Assignee     string
	Reporter     string
	Priority     string
	Labels       []string
}

// MarshalJSONArray 把字符串切片编码为 JSON 数组（DB 存储用）
func MarshalJSONArray(arr []string) string {
	if arr == nil {
		arr = []string{}
	}
	b, _ := json.Marshal(arr)
	return string(b)
}

// UnmarshalJSONArray 把 DB 里的 JSON 字符串解码为字符串切片
func UnmarshalJSONArray(s string) []string {
	var arr []string
	if s == "" || s == "null" {
		return []string{}
	}
	_ = json.Unmarshal([]byte(s), &arr)
	return arr
}

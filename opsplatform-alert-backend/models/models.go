package models

import (
	"database/sql"
	"time"
)

// LokiConnection Loki连接配置
type LokiConnection struct {
	ID            int       `json:"id"`
	Name          string    `json:"name"`
	URL           string    `json:"url"`
	Username      string    `json:"username"`
	Password      string    `json:"password,omitempty"`
	OrgID         string    `json:"org_id"`
	SkipTLSVerify bool      `json:"skip_tls_verify"`
	Description   string    `json:"description"`
	Status        int       `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreateLokiConnectionReq struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	OrgID         string `json:"org_id"`
	SkipTLSVerify bool   `json:"skip_tls_verify"`
	Description   string `json:"description"`
}

// ESConnection ES集群连接配置
type ESConnection struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`          // 多个地址用逗号分隔
	Version     string    `json:"version"`      // "7" or "8"
	Username    string    `json:"username"`
	Password    string    `json:"password,omitempty"`
	APIKey        string    `json:"api_key,omitempty"` // ES 8.x
	SkipTLSVerify bool      `json:"skip_tls_verify"`   // 跳过TLS证书验证
	Description   string    `json:"description"`
	Status      int       `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// LarkConfig Lark/飞书 Webhook 配置
type LarkConfig struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	WebhookURL  string    `json:"webhook_url"`
	Secret      string    `json:"secret,omitempty"` // 签名密钥
	LarkType    string    `json:"lark_type"`        // "feishu" or "larksuite"
	Description string    `json:"description"`
	Status      int       `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// FilterField ES查询过滤字段
type FilterField struct {
	Field string `json:"field"` // ES字段路径
	Value string `json:"value"` // 匹配值
	Op    string `json:"op"`    // 操作: match/term/wildcard/exists, 默认 match
}

// ExtractField 字段提取规则
type ExtractField struct {
	Name    string `json:"name"`    // 提取后的变量名
	Path    string `json:"path"`    // ES _source 中的字段路径
	Pattern string `json:"pattern"` // 正则表达式，可选
}

// AtUser Lark @用户
type AtUser struct {
	Name   string `json:"name"`    // 显示名
	UserID string `json:"user_id"` // Lark open_id 或 user_id
}

// AlertRule 告警规则
type AlertRule struct {
	ID              int            `json:"id"`
	Name            string         `json:"name"`
	DataSourceType  string         `json:"data_source_type"`   // es or loki
	ESConnectionID  int            `json:"es_connection_id"`
	LokiConnectionID int           `json:"loki_connection_id"`
	LarkConfigID    int            `json:"lark_config_id"`
	ESIndex         string         `json:"es_index"`
	Schedule        string         `json:"schedule"`
	TimeRange       string         `json:"time_range"`
	QueryDSL        string         `json:"query_dsl"`         // 自定义ES查询JSON
	Keyword         string         `json:"keyword"`           // 简单关键词搜索
	LogQL           string         `json:"logql"`             // LogQL查询(Loki)
	FilterFields    string         `json:"filter_fields"`     // JSON string of []FilterField
	ExtractFields   string         `json:"extract_fields"`    // JSON string of []ExtractField
	MessageTitle    string         `json:"message_title"`
	MessageTemplate string         `json:"message_template"`
	AtUsers         string         `json:"at_users"`          // JSON string of []AtUser
	AtAll            int            `json:"at_all"`
	AlertMode        string         `json:"alert_mode"`        // found or not_found
	RecoveryEnabled  int            `json:"recovery_enabled"`
	RecoveryTitle    string         `json:"recovery_title"`
	RecoveryTemplate string         `json:"recovery_template"`
	Severity         string         `json:"severity"`
	GroupBy          string         `json:"group_by"`
	DedupField       string         `json:"dedup_field"`
	DedupTTL         int            `json:"dedup_ttl"`
	MaxAlerts        int            `json:"max_alerts"`
	PrometheusConfig string         `json:"prometheus_config"`
	Status          int            `json:"status"`
	LastRunAt       sql.NullTime   `json:"last_run_at"`
	LastError       sql.NullString `json:"last_error"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`

	// Join fields (not in DB)
	ESConnectionName string `json:"es_connection_name,omitempty"`
	LarkConfigName   string `json:"lark_config_name,omitempty"`
}

// AlertLog 告警日志
type AlertLog struct {
	ID           int64     `json:"id"`
	RuleID       int       `json:"rule_id"`
	RuleName     string    `json:"rule_name"`
	Severity     string    `json:"severity"`
	Message      string    `json:"message"`
	ESRaw        string    `json:"es_raw,omitempty"`
	LarkResponse string    `json:"lark_response,omitempty"`
	Status       string    `json:"status"`
	ErrorMsg     string    `json:"error_msg,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

// User 用户
type User struct {
	ID           int       `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	DisplayName  string    `json:"display_name"`
	Role         string    `json:"role"`
	OIDCSubject  string    `json:"oidc_subject,omitempty"`
	Status       int       `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// AlertRuleListItem 列表展示用
type AlertRuleListItem struct {
	AlertRule
	ESConnectionName string `json:"es_connection_name"`
	LarkConfigName   string `json:"lark_config_name"`
	RunCount         int64  `json:"run_count"`
}

// API Request/Response types

type CreateESConnectionReq struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	Version       string `json:"version"`
	Username      string `json:"username"`
	Password      string `json:"password"`
	APIKey        string `json:"api_key"`
	SkipTLSVerify bool   `json:"skip_tls_verify"`
	Description   string `json:"description"`
}

type CreateLarkConfigReq struct {
	Name        string `json:"name"`
	WebhookURL  string `json:"webhook_url"`
	Secret      string `json:"secret"`
	LarkType    string `json:"lark_type"`
	Description string `json:"description"`
}

type CreateAlertRuleReq struct {
	Name             string `json:"name"`
	DataSourceType   string `json:"data_source_type"`
	ESConnectionID   int    `json:"es_connection_id"`
	LokiConnectionID int    `json:"loki_connection_id"`
	LarkConfigID     int    `json:"lark_config_id"`
	ESIndex          string `json:"es_index"`
	Schedule         string `json:"schedule"`
	TimeRange        string `json:"time_range"`
	QueryDSL         string `json:"query_dsl"`
	Keyword          string `json:"keyword"`
	LogQL            string `json:"logql"`
	FilterFields    string `json:"filter_fields"`
	ExtractFields   string `json:"extract_fields"`
	MessageTitle    string `json:"message_title"`
	MessageTemplate string `json:"message_template"`
	AtUsers         string `json:"at_users"`
	AtAll            int    `json:"at_all"`
	AlertMode        string `json:"alert_mode"`
	RecoveryEnabled  int    `json:"recovery_enabled"`
	RecoveryTitle    string `json:"recovery_title"`
	RecoveryTemplate string `json:"recovery_template"`
	Severity         string `json:"severity"`
	GroupBy          string `json:"group_by"`
	DedupField       string `json:"dedup_field"`
	DedupTTL         int    `json:"dedup_ttl"`
	MaxAlerts        int    `json:"max_alerts"`
	PrometheusConfig string `json:"prometheus_config"`
}

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type PaginatedResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
	Total   int64       `json:"total"`
	Page    int         `json:"page"`
	Limit   int         `json:"limit"`
}

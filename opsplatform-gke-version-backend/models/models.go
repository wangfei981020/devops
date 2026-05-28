package models

import "time"

type Cluster struct {
	ID          int       `json:"id" db:"id"`
	ProjectID   string    `json:"project_id" db:"project_id"`
	Location    string    `json:"location" db:"location"`
	Name        string    `json:"name" db:"name"`
	SAKeyJSON   string    `json:"-" db:"sa_key_json"` // 敏感字段，永不在 JSON 响应里返回
	HasSAKey    bool      `json:"has_sa_key"`         // 前端用来判断是否已配置
	Enabled     int       `json:"enabled" db:"enabled"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

type NodePoolInfo struct {
	Name                          string  `json:"name"`
	CurrentVersion                string  `json:"current_version"`
	MaxUpgradableVersion          string  `json:"max_upgradable_version"`
	LatestAvailableVersion        string  `json:"latest_available_version"`
	CurrentToMaxVersionsBehind    int     `json:"current_to_max_versions_behind"`
	CurrentToMaxVersionDiff       float64 `json:"current_to_max_version_diff"`
	MaxToLatestVersionsBehind     int     `json:"max_to_latest_versions_behind"`
	MaxToLatestVersionDiff        float64 `json:"max_to_latest_version_diff"`
	CurrentToLatestVersionsBehind int     `json:"current_to_latest_versions_behind"`
	CurrentToLatestVersionDiff    float64 `json:"current_to_latest_version_diff"`
	StdSupportEnd                 string  `json:"std_support_end"`
	ExtSupportEnd                 string  `json:"ext_support_end"`
}

type ClusterSnapshot struct {
	ClusterID                     int            `json:"cluster_id"`
	CurrentVersion                string         `json:"current_version"`
	MaxUpgradableVersion          string         `json:"max_upgradable_version"`
	LatestAvailableVersion        string         `json:"latest_available_version"`
	CurrentToMaxVersionsBehind    int            `json:"current_to_max_versions_behind"`
	CurrentToMaxVersionDiff       float64        `json:"current_to_max_version_diff"`
	MaxToLatestVersionsBehind     int            `json:"max_to_latest_versions_behind"`
	MaxToLatestVersionDiff        float64        `json:"max_to_latest_version_diff"`
	CurrentToLatestVersionsBehind int            `json:"current_to_latest_versions_behind"`
	CurrentToLatestVersionDiff    float64        `json:"current_to_latest_version_diff"`
	StdSupportEnd                 string         `json:"std_support_end"`
	ExtSupportEnd                 string         `json:"ext_support_end"`
	NodePools                     []NodePoolInfo `json:"nodepools"`
	LastRefreshedAt               *time.Time     `json:"last_refreshed_at"`
	LastError                     string         `json:"last_error"`
}

// Node：GKE 集群里的一个 VM 实例（一个 node）
// 数据来源：GCP Compute API；每次 scrape 全量刷新（DELETE+INSERT 事务）
type Node struct {
	ID           int       `json:"id" db:"id"`
	ClusterID    int       `json:"cluster_id" db:"cluster_id"`
	NodepoolName string    `json:"nodepool_name" db:"nodepool_name"`
	NodeName     string    `json:"node_name" db:"node_name"`
	Zone         string    `json:"zone" db:"zone"`
	Version      string    `json:"version" db:"version"`
	GCPCreatedAt time.Time `json:"gcp_created_at" db:"gcp_created_at"`
	LastSeenAt   time.Time `json:"last_seen_at" db:"last_seen_at"`
}

type Setting struct {
	Key       string    `json:"k"`
	Value     string    `json:"v"`
	UpdatedAt time.Time `json:"updated_at"`
}

type NotifyUser struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	LarkID    string    `json:"lark_id"`
	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type LarkWebhook struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AlertRule struct {
	ID                      int       `json:"id"`
	Name                    string    `json:"name"`
	Target                  string    `json:"target"` // cluster | nodepool
	VersionsBehindThreshold int       `json:"versions_behind_threshold"`
	EOLDaysThreshold        *int      `json:"eol_days_threshold,omitempty"`
	ClusterIDs              []int     `json:"cluster_ids"`        // 空 = 全部
	WebhookID               int       `json:"webhook_id"`
	MentionUserIDs          []int     `json:"mention_user_ids"`
	IntervalMinutes         int       `json:"interval_minutes"`
	Enabled                 int       `json:"enabled"`
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

type AlertHistory struct {
	ID             int        `json:"id"`
	RuleID         int        `json:"rule_id"`
	ClusterID      int        `json:"cluster_id"`
	NodepoolName   string     `json:"nodepool_name,omitempty"`
	VersionsBehind int        `json:"versions_behind"`
	TriggerTime    time.Time  `json:"trigger_time"`
	Status         string     `json:"status"`
	LarkResponse   string     `json:"lark_response,omitempty"`
}

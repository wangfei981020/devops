package models

import "time"

type Cluster struct {
	ID        int       `json:"id" db:"id"`
	ProjectID string    `json:"project_id" db:"project_id"`
	Location  string    `json:"location" db:"location"`
	Name      string    `json:"name" db:"name"`
	Enabled   int       `json:"enabled" db:"enabled"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
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

type Setting struct {
	Key       string    `json:"k"`
	Value     string    `json:"v"`
	UpdatedAt time.Time `json:"updated_at"`
}

package models

import "time"

// Project 项目
type Project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Environment 环境字典 (每环境独立 ArgoCD)
type Environment struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"display_name"`
	AutoSync    int       `json:"auto_sync"`
	ArgocdURL   string    `json:"argocd_url"`
	ArgocdToken string    `json:"argocd_token"`
	Description string    `json:"description"`
	SortOrder   int       `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectEnv 项目-环境
type ProjectEnv struct {
	ID            int64     `json:"id"`
	ProjectID     int64     `json:"project_id"`
	EnvID         int64     `json:"env_id"`
	GitRepo       string    `json:"git_repo"`
	GitBranch     string    `json:"git_branch"`
	GitBasePath   string    `json:"git_base_path"`
	Namespace     string    `json:"namespace"`
	ArgocdProject string    `json:"argocd_project"`
	ArgocdCluster string    `json:"argocd_cluster"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// 关联展示字段
	ProjectName string `json:"project_name,omitempty"`
	EnvName     string `json:"env_name,omitempty"`
}

// ChartTemplate Helm 模板
type ChartTemplate struct {
	ID              int64     `json:"id"`
	Name            string    `json:"name"`
	Type            string    `json:"type"` // backend / frontend
	Description     string    `json:"description"`
	SourceType      string    `json:"source_type"`
	GitRepo         string    `json:"git_repo"`
	ChartPath       string    `json:"chart_path"`
	DefaultValues   string    `json:"default_values"`
	ProbeConfig     string    `json:"probe_config"`
	ConfigmapSchema string    `json:"configmap_schema"`
	Version         string    `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Module 模块
type Module struct {
	ID                   int64     `json:"id"`
	ProjectEnvID         int64     `json:"project_env_id"`
	Name                 string    `json:"name"`
	TemplateID           int64     `json:"template_id"`
	TemplateVersion      string    `json:"template_version"`
	ImageRepo            string    `json:"image_repo"`
	CurrentTag           string    `json:"current_tag"`
	Replicas             int       `json:"replicas"`
	Autoscaling          string    `json:"autoscaling"`
	Resources            string    `json:"resources"`
	RollingUpdate        string    `json:"rolling_update"`
	RevisionHistoryLimit int       `json:"revision_history_limit"`
	EnvVars              string    `json:"env_vars"`
	ExtraEnvVars         string    `json:"extra_env_vars"`
	TidbSecrets          string    `json:"tidb_secrets"`
	ProbeOverride        string    `json:"probe_override"`
	ConfigmapData        string    `json:"configmap_data"`
	GitChartPath         string    `json:"git_chart_path"`
	ArgocdAppName        string    `json:"argocd_app_name"`
	Status               string    `json:"status"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`

	// 展示字段
	ProjectName  string `json:"project_name,omitempty"`
	EnvName      string `json:"env_name,omitempty"`
	TemplateName string `json:"template_name,omitempty"`
}

// Secret K8s Secret 定义
type Secret struct {
	ID            int64      `json:"id"`
	ProjectEnvID  int64      `json:"project_env_id"`
	Name          string     `json:"name"`
	Type          string     `json:"type"`
	Data          string     `json:"data"` // JSON, value 加密
	Description   string     `json:"description"`
	SyncedAt      *time.Time `json:"synced_at"`
	SyncStatus    string     `json:"sync_status"`
	SyncError     string     `json:"sync_error"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// Contact 通知人
type Contact struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	LarkID    string    `json:"lark_id"`
	Remark    string    `json:"remark"`
	Status    int       `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LarkConfig Lark 群配置
type LarkConfig struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	WebhookURL  string    `json:"webhook_url"`
	Secret      string    `json:"secret"`
	LarkType    string    `json:"lark_type"`
	Description string    `json:"description"`
	Status      int       `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ProjectEnvNotify 项目-环境通知绑定
type ProjectEnvNotify struct {
	ID           int64     `json:"id"`
	ProjectEnvID int64     `json:"project_env_id"`
	LarkConfigID int64     `json:"lark_config_id"`
	ContactIDs   string    `json:"contact_ids"` // JSON 数组
	UpdatedAt    time.Time `json:"updated_at"`
}

// Deployment 发布历史
type Deployment struct {
	ID               int64     `json:"id"`
	ModuleID         int64     `json:"module_id"`
	ModuleName       string    `json:"module_name"`
	ProjectEnvID     int64     `json:"project_env_id"`
	Action           string    `json:"action"`
	FromTag          string    `json:"from_tag"`
	ToTag            string    `json:"to_tag"`
	ValuesBefore     string    `json:"values_before,omitempty"`
	ValuesAfter      string    `json:"values_after,omitempty"`
	GitCommit        string    `json:"git_commit"`
	GitCommitURL     string    `json:"git_commit_url"`
	ArgocdSyncStatus string    `json:"argocd_sync_status"`
	ArgocdSyncMsg    string    `json:"argocd_sync_msg"`
	NotifyStatus     string    `json:"notify_status"`
	NotifyMsg        string    `json:"notify_msg"`
	Operator         string    `json:"operator"`
	Status           string    `json:"status"`
	ErrorMsg         string    `json:"error_msg"`
	CreatedAt        time.Time `json:"created_at"`
}

// EnvTemplate 环境变量模板
type EnvTemplate struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	EnvVars     string    `json:"env_vars"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// GlobalConfig 全局配置 (单行)
type GlobalConfig struct {
	ID             int64     `json:"id"`
	GitlabURL      string    `json:"gitlab_url"`
	GitlabToken    string    `json:"gitlab_token"`
	GitlabUser     string    `json:"gitlab_user"`
	GitlabEmail    string    `json:"gitlab_email"`
	HarborURL      string    `json:"harbor_url"`
	HarborUser     string    `json:"harbor_user"`
	HarborPassword string    `json:"harbor_password"`
	ArgocdURL      string    `json:"argocd_url"`
	ArgocdToken    string    `json:"argocd_token"`
	UpdatedAt      time.Time `json:"updated_at"`
}

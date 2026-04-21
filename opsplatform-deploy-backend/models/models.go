package models

import "time"

type GlobalConfig struct {
	ID                 int64     `json:"id"`
	GitlabURL          string    `json:"gitlab_url"`
	GitlabUser         string    `json:"gitlab_user"`
	GitlabEmail        string    `json:"gitlab_email"`
	GitlabToken        string    `json:"gitlab_token,omitempty"`
	LarkDefaultWebhook string    `json:"lark_default_webhook"`
	LarkDefaultSecret  string    `json:"lark_default_secret,omitempty"`
	PollIntervalSec    int       `json:"poll_interval_sec"`
	PollTimeoutMin     int       `json:"poll_timeout_min"`
	GitRetryCount      int       `json:"git_retry_count"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type ProjectEnv struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	DisplayName      string    `json:"display_name"`
	EnvType          string    `json:"env_type"` // "uat" | "prod"
	GitRepo          string    `json:"git_repo"`
	GitBranch        string    `json:"git_branch"`
	ChartBasePath    string    `json:"chart_base_path"`
	Namespace        string    `json:"namespace"` // 遗留字段（默认 namespace）
	ArgocdURL        string    `json:"argocd_url,omitempty"`   // 遗留字段
	ArgocdToken      string    `json:"argocd_token,omitempty"` // 遗留字段
	ArgocdInstanceID *int64    `json:"argocd_instance_id"`
	LarkWebhook      string    `json:"lark_webhook"`
	LarkSecret       string    `json:"lark_secret,omitempty"`
	AutoSync         int       `json:"auto_sync"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Account 账号（身份选择 / Lark 艾特）
type Account struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	LarkID      string    `json:"lark_id"`
	Email       string    `json:"email"`
	Remark      string    `json:"remark"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// ArgocdInstance ArgoCD 实例
type ArgocdInstance struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Token       string    `json:"token,omitempty"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Module struct {
	ID              int64      `json:"id"`
	ProjectEnvID    int64      `json:"project_env_id"`
	Name            string     `json:"name"`
	CurrentTag      string     `json:"current_tag"`
	ImageRepository string     `json:"image_repository"`
	ArgocdAppName   string     `json:"argocd_app_name"`
	Namespace       string     `json:"namespace"` // 该模块实际部署的 K8s namespace
	LastScannedAt   *time.Time `json:"last_scanned_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type Change struct {
	Module  string `json:"module"`
	FromTag string `json:"from_tag"`
	ToTag   string `json:"to_tag"`
}

type ArgocdAppResult struct {
	App         string `json:"app"`
	SyncStatus  string `json:"sync_status"`
	Health      string `json:"health"`
	DurationSec int    `json:"duration_sec"`
	Msg         string `json:"msg"`
}

type Deployment struct {
	ID              int64             `json:"id"`
	ProjectEnvID    int64             `json:"project_env_id"`
	Action          string            `json:"action"` // update_image|restart|rollback
	RefDeploymentID *int64            `json:"ref_deployment_id"`
	ModuleNames     []string          `json:"module_names"`
	Changes         []Change          `json:"changes"`
	GitCommit       string            `json:"git_commit"`
	GitCommitURL    string            `json:"git_commit_url"`
	ArgocdResults   []ArgocdAppResult `json:"argocd_results"`
	LarkNotify      string            `json:"lark_notify"` // success|failed|skipped
	Operator        string            `json:"operator"`
	Status          string            `json:"status"` // pending|success|partial|failed|no_change
	ErrorMsg        string            `json:"error_msg"`
	DurationSec     int               `json:"duration_sec"`
	CreatedAt       time.Time         `json:"created_at"`
}

const (
	EnvUAT  = "uat"
	EnvPROD = "prod"

	ActionUpdateImage = "update_image"
	ActionRestart     = "restart"
	ActionRollback    = "rollback"

	StatusPending  = "pending"
	StatusSuccess  = "success"
	StatusPartial  = "partial"
	StatusFailed   = "failed"
	StatusNoChange = "no_change"
)

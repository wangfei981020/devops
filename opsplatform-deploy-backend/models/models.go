package models

import "time"

type GlobalConfig struct {
	ID                  int64     `json:"id"`
	GitlabURL           string    `json:"gitlab_url"`
	GitlabUser          string    `json:"gitlab_user"`
	GitlabEmail         string    `json:"gitlab_email"`
	GitlabToken         string    `json:"gitlab_token,omitempty"`
	TestRepoPath        string    `json:"test_repo_path"`         // 测试连接用的仓库相对路径
	DeployCenterBaseURL string    `json:"deploy_center_base_url"` // Lark 通知「查看详情」跳转用
	LarkDefaultWebhook  string    `json:"lark_default_webhook"`
	LarkDefaultSecret   string    `json:"lark_default_secret,omitempty"`
	PollIntervalSec     int       `json:"poll_interval_sec"`
	PollTimeoutMin      int       `json:"poll_timeout_min"`
	GitRetryCount       int       `json:"git_retry_count"`
	UpdatedAt           time.Time `json:"updated_at"`
	// MinIO 配置——失败 pod 日志归档用。未配则跳过归档功能
	MinIOEndpoint      string `json:"minio_endpoint"`
	MinIOBucket        string `json:"minio_bucket"`
	MinIOAccessKey     string `json:"minio_access_key"`
	MinIOSecretKey     string `json:"minio_secret_key,omitempty"` // 返回时脱敏
	MinIORegion        string `json:"minio_region"`
	MinIORetentionDays int    `json:"minio_retention_days"`
	// 发布历史保留天数（自动清理）
	HistoryRetentionDays int        `json:"history_retention_days"`
	LastHistoryCleanupAt *time.Time `json:"last_history_cleanup_at"`
	// Harbor 镜像仓库（镜像 tag 下拉 + 提交前校验）
	HarborURL            string `json:"harbor_url"`
	HarborUser           string `json:"harbor_user"`             // 通常是 robot$xxx 账号
	HarborToken          string `json:"harbor_token,omitempty"`  // 返回时脱敏
	HarborVerifyOnSubmit bool   `json:"harbor_verify_on_submit"` // true = 提交前校验 tag 是否在 Harbor
	// VM 部署：list-version API（拉版本号）
	ListVersionAPI   string `json:"list_version_api"`
	ListVersionToken string `json:"list_version_token,omitempty"` // 返回时脱敏
}

type ProjectEnv struct {
	ID               int64     `json:"id"`
	Name             string    `json:"name"`
	DisplayName      string    `json:"display_name"`
	EnvType          string    `json:"env_type"` // "uat" | "prod"
	GitRepo          string    `json:"git_repo"`
	GitBranch        string    `json:"git_branch"`
	ChartBasePath    string    `json:"chart_base_path"`
	Namespace        string    `json:"namespace"` // 默认 namespace（模块可覆盖）
	ArgocdURL        string    `json:"argocd_url,omitempty"`   // 遗留字段
	ArgocdToken      string    `json:"argocd_token,omitempty"` // 遗留字段
	ArgocdInstanceID *int64    `json:"argocd_instance_id"`
	LarkWebhook      string    `json:"lark_webhook,omitempty"` // 遗留字段
	LarkSecret       string    `json:"lark_secret,omitempty"`  // 遗留字段
	LarkBotID        *int64    `json:"lark_bot_id"`
	GitlabRepoID     *int64    `json:"gitlab_repo_id"`
	AutoSync         int       `json:"auto_sync"`
	IngressGateway   string    `json:"ingress_gateway"` // 该环境的 istio 网关名，新增模块时自动带出
	HarborProject    string    `json:"harbor_project"`  // 该环境的 Harbor 项目名，留空=自动用项目名；推导镜像仓库用
	DomainSuffix     string    `json:"domain_suffix"`   // 该环境的域名后缀，如 uat.slileisure.com；前端模块访问域名自动带出用
	DefaultNamespaces string   `json:"default_namespaces"` // 该环境可用 namespace 列表(换行/逗号分隔)，第一个作默认；新增模块下拉/自动填用
	ZkvSecretsPath   string    `json:"zkv_secrets_path"` // 后端 secret 集中处 z-kv-secrets 的 chart 路径；留空=自动推 <chart_base_path>/z-kv-secrets
	AtLarkIDs        string    `json:"at_lark_ids"`      // 固定艾特人的 Lark ID 列表(一行一个)；新增模块通知自动 @
	SecretPrefix     string    `json:"secret_prefix"`    // 密钥名项目前缀覆盖；留空=项目名去后缀转小写，填了=固定用它(如 g33 复用 g32 填 g32)
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// Account 平台登录账户
type Account struct {
	ID          int64     `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	LarkID      string    `json:"lark_id,omitempty"` // 保留兼容字段
	Email       string    `json:"email"`
	Remark      string    `json:"remark"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Contact 通知人（Lark 艾特专用）
type Contact struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	LarkID    string    `json:"lark_id"`
	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// GitlabRepo 登记的 GitLab 仓库（可被多个 project_env 复用）
type GitlabRepo struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	RepoURL       string    `json:"repo_url"`
	DefaultBranch string    `json:"default_branch"`
	Description   string    `json:"description"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// LarkBot Lark 机器人
type LarkBot struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Webhook     string    `json:"webhook"`
	Secret      string    `json:"secret,omitempty"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// DeployAgent VM agent 元数据（部署在 ansible 控制机上的 deploy-vm-agent）
//
//	跟 ArgocdInstance 同构：name + url + token + description
//	deploy-center 通过 url + token 调 agent 的 HTTP API 触发 ansible 任务
type DeployAgent struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`             // https://ansible-host:8443
	Token       string    `json:"token,omitempty"` // 返回时脱敏
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// VmProjectEnv VM 项目环境（独立于 K8s 的 project_env）
//
//	跟 ProjectEnv 业务概念对齐，但字段差异大（ansible vs argocd）
type VmProjectEnv struct {
	ID           int64     `json:"id"`
	ProjectID    int64     `json:"project_id"`
	Name         string    `json:"name"`
	DisplayName  string    `json:"display_name"`
	EnvType      string    `json:"env_type"` // LPT / UAT / PROD
	AgentID      int64     `json:"agent_id"`
	AnsibleRoot  string    `json:"ansible_root"`  // 默认 /etc/ansible
	ProjectCode  string    `json:"project_code"`  // G01 / G02
	LarkBotID    *int64    `json:"lark_bot_id"`   // 跟 K8s project_env.lark_bot_id 同义；NULL 走 gc.lark_default_webhook
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// VmService VM 服务（= ansible playbook 文件）
type VmService struct {
	ID              int64      `json:"id"`
	VmProjectEnvID  int64      `json:"vm_project_env_id"`
	Name            string     `json:"name"`              // playbook 文件名（去 .yaml）
	AnsibleGroup    string     `json:"ansible_group"`     // playbook 第一行 hosts: 值
	Hosts           []string   `json:"hosts"`             // 从 inventory 解析的 IP 列表
	CurrentVersion  string     `json:"current_version"`   // 上次 update_version 成功后写入
	LastScannedAt   *time.Time `json:"last_scanned_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
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
	App         string    `json:"app"`
	SyncStatus  string    `json:"sync_status"`
	Health      string    `json:"health"`
	DurationSec int       `json:"duration_sec"`
	Msg         string    `json:"msg"`
	// 该行被服务端最后一次更新的时刻（每次 onTick / 终态返回都设）
	// 前端用这个做单模块耗时插值锚点：刷新后仍能根据 last_polled_at 继续累积，
	// 避免「刷新就把秒数从 180 重新开始算」的视觉 bug。
	LastPolledAt time.Time `json:"last_polled_at"`
	// FailingPods 失败决策瞬间抓的 pod 快照（name + uid + namespace）。
	// 让 archive 能用快照直接拉日志，而不是后续再调 resource-tree
	// 否则 ReplicaSet 可能已把失败 pod 滚走，归档抓空。
	FailingPods []FailingPodSnapshot `json:"failing_pods,omitempty"`
}

// FailingPodSnapshot fail 决策那一刻的 pod 关键字段（archive 用）
type FailingPodSnapshot struct {
	Name         string `json:"name"`
	UID          string `json:"uid"`
	Namespace    string `json:"namespace"`
	StatusReason string `json:"status_reason,omitempty"`
	RestartCount string `json:"restart_count,omitempty"`
	// 内部字段（json:"-" 不出 API）：fail 决策瞬间从 ArgoCD 抓的日志/事件原文
	// 必须在 pod 还活着的时候立刻抓 —— 后续若被新 ReplicaSet 删除，pod 对象会从 etcd 移除，
	// kubectl logs 就拉不到了。在这里先落进 Go 变量，archive 时直接 PutLog 不再调 GetPodLogs。
	PreviousLog string `json:"-"`
	CurrentLog  string `json:"-"`
	EventsJSON  []byte `json:"-"`
}

type Deployment struct {
	ID              int64             `json:"id"`
	ProjectEnvID    int64             `json:"project_env_id"`
	Action          string            `json:"action"` // update_image|restart|rollback|vm_rsync|vm_update_version
	RefDeploymentID *int64            `json:"ref_deployment_id"`
	ModuleNames     []string          `json:"module_names"`
	Changes         []Change          `json:"changes"`
	GitCommit       string            `json:"git_commit"`
	GitCommitURL    string            `json:"git_commit_url"`
	ArgocdResults   []ArgocdAppResult `json:"argocd_results"`
	LarkNotify      string            `json:"lark_notify"` // success|failed|skipped
	Operator        string            `json:"operator"`
	Status          string            `json:"status"` // pending|success|partial|failed|no_change|canceled|interrupted
	ErrorMsg        string            `json:"error_msg"`
	// StatusNote 给人看的干净说明（对账/中断/自愈等），列表接口不脱敏；
	// 跟 ErrorMsg 分开：ErrorMsg 放真失败的技术细节，对非 admin 脱敏。
	StatusNote string `json:"status_note"`
	DurationSec     int               `json:"duration_sec"`
	CreatedAt       time.Time         `json:"created_at"`

	// VM 批量部署专用：vm_task_map JSON 列反序列化结果。
	// K8s 行没用，前端按 row.action 是否 vm_ 前缀决定看 ArgocdResults 还是 VmTaskMap
	VmTaskMap []VmTaskMapEntry `json:"vm_task_map,omitempty"`
}

// VmTaskMapEntry 复刻 handlers.vmTaskMapEntry，让 models 包能被前端 JSON 序列化用。
//
//	跟后端 handler 那个保持字段一致；其它 service 状态由 pollVmTaskBatch 持续更新。
type VmTaskMapEntry struct {
	Service      string `json:"service"`
	Version      string `json:"version,omitempty"`
	TaskID       string `json:"task_id"`
	Status       string `json:"status"`
	ErrMsg       string `json:"error_msg,omitempty"`
	LogObjectKey string `json:"log_object_key,omitempty"`
	LogSize      int    `json:"log_size,omitempty"`
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
	StatusCanceled = "canceled" // 用户在等待中点了"取消等待"，git 改动已 push、ArgoCD 在自己同步，仅停 deploy-center 这边的轮询
	// StatusInterrupted 发布还没 push 到 git，后端就重启了 → 服务实际没变化。
	// 期间已有别人对同模块发过更新版本 → 不覆盖，标此状态，前端橙色提示「服务未受影响，请重试」。
	// 跟 failed 区分：failed=已部署但不健康(要排查)；interrupted=根本没发生(一键重试即可)。
	StatusInterrupted = "interrupted"
)

// OrchestrationTemplate 参照模板：指向 git 里某个样板服务（存指针，运行时拷当前最新）
type OrchestrationTemplate struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Project     string    `json:"project"`      // 绑定项目；空=全局可用
	ModuleType  string    `json:"module_type"`  // "frontend" | "backend"
	SrcEnv      string    `json:"src_env"`      // 样板所在 project_env.name
	SrcService  string    `json:"src_service"`  // 样板服务目录名（chart_base_path 下）
	Description string    `json:"description"`
	Config      string    `json:"config,omitempty"` // 预留 JSON：推导规则/默认覆盖
	Enabled     int       `json:"enabled"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// DeployEnvironment 可配置环境（dev/test/uat/prod…），每环境独立权限档
type DeployEnvironment struct {
	Name           string    `json:"name"`
	DisplayName    string    `json:"display_name"`
	PermissionCode string    `json:"permission_code"` // 如 submit_uat / submit_dev
	SortOrder      int       `json:"sort_order"`
	Enabled        int       `json:"enabled"`
	CreatedAt      time.Time `json:"created_at"`
}

const (
	ModuleTypeFrontend = "frontend"
	ModuleTypeBackend  = "backend"
	ModuleTypeZkv      = "zkv" // z-kv-secrets 模板：整份密钥 chart，供新项目初始化复制
)

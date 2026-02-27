package models

// Record 数据源ID记录
type Record struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"` // 连接ID，唯一标识
	Project      string `json:"project"`
	Env          string `json:"env"`
	Module       string `json:"module"` // 模块名
	VID          string `json:"vid"`
	SrcIP        string `json:"src_ip"`
	SrcPort      string `json:"src_port"` // 源端口
	DestIP       string `json:"dest_ip"`
	DestPort     string `json:"dest_port"` // 目标端口（原 port 字段）
	Status       string `json:"status"`
	Operator     string `json:"operator"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	CreatedBy    string `json:"created_by"`
	UpdatedBy    string `json:"updated_by"`
}

// User 用户
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Password    string `json:"password,omitempty"`
	DisplayName string `json:"display_name"`
	Phone       string `json:"phone"`
	Email       string `json:"email"`
	Description string `json:"description"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	Permissions string `json:"permissions"`
	MFAEnabled  bool   `json:"mfa_enabled"`          // 是否启用 MFA（管理员配置）
	MFASecret   string `json:"mfa_secret,omitempty"` // MFA 密钥（不返回给前端）
	MFABound    bool   `json:"mfa_bound"`            // 是否已绑定 MFA（有密钥即为已绑定）
	Language    string `json:"language"`             // 用户界面语言: zh-CN, en-US
	AuthSource  string `json:"auth_source"`          // 认证来源: local, sso
	CreatedAt   string `json:"created_at"`
}

// AuditLog 审计日志
type AuditLog struct {
	ID         string `json:"id"`
	TraceID    string `json:"trace_id"`
	Action     string `json:"action"`
	RecordID   string `json:"record_id"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Operator   string `json:"operator"`
	OldData    string `json:"old_data"`
	NewData    string `json:"new_data"`
	Changes    string `json:"changes"`
	IP         string `json:"ip"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	StatusCode int    `json:"status_code"`
	Duration   int64  `json:"duration"`
	CreatedAt  string `json:"created_at"`
}

// DataSource 数据源
type DataSource struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"` // prometheus, jira, domain
	URL         string `json:"url"`
	Username    string `json:"username"`
	Password    string `json:"password,omitempty"`
	Token       string `json:"token,omitempty"`
	Description string `json:"description"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	CreatedBy   string `json:"created_by"`
}

// Metric 自定义监控指标
type Metric struct {
	ID          string `json:"id"`
	Name        string `json:"name"`        // 指标唯一标识，如 pod_running
	Label       string `json:"label"`       // 显示名称，如 "☸️ 运行中Pod数"
	PromQL      string `json:"promql"`      // PromQL 查询语句
	Unit        string `json:"unit"`        // 单位，如 %, GB, 个
	Group       string `json:"group"`       // 分组，如 k8s, container, node
	Description string `json:"description"` // 描述
	Enabled     bool   `json:"enabled"`     // 是否启用
	SortOrder   int    `json:"sort_order"`  // 排序
	CreatedAt   string `json:"created_at"`
	CreatedBy   string `json:"created_by"`
}

// Domain 域名管理
type Domain struct {
	ID             string `json:"id"`
	Project        string `json:"project"`          // 项目
	Module         string `json:"module"`           // 模块
	DomainName     string `json:"domain_name"`      // 域名
	Origin         string `json:"origin"`           // 回源地址
	OriginIP       string `json:"origin_ip"`        // 源站IP
	CDNProvider    string `json:"cdn_provider"`     // CDN 厂商
	Env            string `json:"env"`              // 环境：PROD, UAT, DEV
	ExpireTime     string `json:"expire_time"`      // 域名到期时间
	CertExpireTime string `json:"cert_expire_time"` // 证书到期时间
	Status         string `json:"status"`           // 状态：active, inactive, expired
	Remark         string `json:"remark"`           // 备注
	CreatedAt      string `json:"created_at"`
	CreatedBy      string `json:"created_by"`
	UpdatedAt      string `json:"updated_at"`
	UpdatedBy      string `json:"updated_by"`
}

// Merchant 商户管理
type Merchant struct {
	ID               string `json:"id"`
	Project          string `json:"project"`           // 项目
	Env              string `json:"env"`               // 环境: prod, uat, dev
	WebsiteName      string `json:"website_name"`      // 网站方
	ContactEmails    string `json:"contact_emails"`    // 对接邮箱（多个，JSON数组）
	WebsiteUrls      string `json:"website_urls"`      // 网站方网址（多个）
	PlayerRegions    string `json:"player_regions"`    // 玩家地区（多个）
	EstimatedPlayers string `json:"estimated_players"` // 预计在线玩家数量
	GameTypes        string `json:"game_types"`        // 游戏种类（多个）
	Handicaps        string `json:"handicaps"`         // 盘口（多个）
	Languages        string `json:"languages"`         // 语言（多个）
	Currencies       string `json:"currencies"`        // 币种（多个）
	SupportedPorts   string `json:"supported_ports"`   // 支持端口（多个）
	WalletTypes      string `json:"wallet_types"`      // 钱包类型（多个）
	CallbackDomains  string `json:"callback_domains"`  // 三方回调域名（多个）
	WhitelistIPs     string `json:"whitelist_ips"`     // 三方白名单
	HallDomains      string `json:"hall_domains"`      // 三方调用厅房域名（多个）
	SiteDomains      string `json:"site_domains"`      // 厅方站点系统域名（多个）
	SiteAccounts     string `json:"site_accounts"`     // 站点系统账号（多个）
	AppKeys          string `json:"app_keys"`          // appkey（多个）
	AppSecrets       string `json:"app_secrets"`       // appsecret（密码系统查看）
	GameDomains      string `json:"game_domains"`      // 游戏域名（多个）
	RedirectDomains  string `json:"redirect_domains"`  // 301域名（多个）
	CustomFields     string `json:"custom_fields"`     // 自定义字段（JSON对象）
	Remark           string `json:"remark"`            // 备注
	Status           string `json:"status"`            // 状态
	CreatedAt        string `json:"created_at"`
	CreatedBy        string `json:"created_by"`
	UpdatedAt        string `json:"updated_at"`
	UpdatedBy        string `json:"updated_by"`
}

// Website 网站管理 - 乙方（网站方/服务提供方）信息
type Website struct {
	ID            string `json:"id"`
	Name          string `json:"name"`               // 网站名称
	URL           string `json:"url"`                // 网站地址
	Category      string `json:"category"`           // 分类: internal, external, tool
	Description   string `json:"description"`        // 描述
	Icon          string `json:"icon"`               // 图标URL
	BizContact    string `json:"biz_contact"`        // 商务对接人
	BizPhone      string `json:"biz_phone"`          // 商务电话
	TechContact   string `json:"tech_contact"`       // 技术对接人
	TechPhone     string `json:"tech_phone"`         // 技术电话
	Username      string `json:"username"`           // 登录用户名
	Password      string `json:"password,omitempty"` // 登录密码(加密存储)
	ContractNo    string `json:"contract_no"`        // 合同编号
	ContractStart string `json:"contract_start"`     // 合同开始日期
	ContractEnd   string `json:"contract_end"`       // 合同结束日期
	CostInfo      string `json:"cost_info"`          // 费用说明
	SortOrder     int    `json:"sort_order"`         // 排序
	Status        string `json:"status"`             // 状态: active, inactive
	CreatedAt     string `json:"created_at"`
	CreatedBy     string `json:"created_by"`
	UpdatedAt     string `json:"updated_at"`
	UpdatedBy     string `json:"updated_by"`
}

// ServiceConfig 服务配置信息（一个服务模块）
type ServiceConfig struct {
	ID           string              `json:"id"`
	Project      string              `json:"project"`      // 项目名称
	ServiceName  string              `json:"service_name"` // 服务名称（如: web, api-gateway, user-service）
	ServiceType  string              `json:"service_type"` // 服务类型: web, backend, middleware, database, cache, mq, gateway, third_party
	Domain       string              `json:"domain"`       // 域名
	Port         string              `json:"port"`         // 端口
	Env          string              `json:"env"`          // 环境: prod, uat, dev
	Namespace    string              `json:"namespace"`    // K8s 命名空间
	Replicas     int                 `json:"replicas"`     // 副本数
	Image        string              `json:"image"`        // 镜像地址
	Remark       string              `json:"remark"`       // 备注
	Status       string              `json:"status"`       // 状态: active, inactive
	SortOrder    int                 `json:"sort_order"`   // 排序
	CreatedAt    string              `json:"created_at"`
	CreatedBy    string              `json:"created_by"`
	UpdatedAt    string              `json:"updated_at"`
	UpdatedBy    string              `json:"updated_by"`
	Dependencies []ServiceDependency `json:"dependencies,omitempty"` // 关联的依赖
}

// ServiceDependency 服务依赖关系
type ServiceDependency struct {
	ID             string `json:"id"`
	ServiceID      string `json:"service_id"`         // 所属服务ID
	DependencyType string `json:"dependency_type"`    // 依赖类型: mysql, redis, mq, api, third_party, mongodb, elasticsearch, other
	DependencyName string `json:"dependency_name"`    // 依赖名称（如: 主数据库、用户缓存、支付回调）
	Host           string `json:"host"`               // 连接地址/域名
	Port           string `json:"port"`               // 端口
	Database       string `json:"database"`           // 数据库名（如适用）
	Username       string `json:"username"`           // 用户名
	Password       string `json:"password,omitempty"` // 密码（加密存储）
	ConnString     string `json:"conn_string"`        // 完整连接串（可选）
	Remark         string `json:"remark"`             // 备注
	Status         string `json:"status"`             // 状态: active, inactive
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// Task 任务池
type Task struct {
	ID        string `json:"id"`
	Project   string `json:"project"`    // 项目
	Title     string `json:"title"`      // 需求描述/标题
	Source    string `json:"source"`     // 需求来源: product, operation, tech, customer, leader, other
	Category  string `json:"category"`   // 任务分类: feature, bugfix, optimization, infrastructure, security, other
	Priority  string `json:"priority"`   // 优先级: P0, P1, P2, P3
	Assignee  string `json:"assignee"`   // 任务负责人
	StartTime string `json:"start_time"` // 开始时间
	EndTime   string `json:"end_time"`   // 结束时间（原定）
	Status    string `json:"status"`     // 需求状态: pending, in_progress, testing, completed, cancelled
	Result    string `json:"result"`     // 结果
	Remark    string `json:"remark"`     // 备注
	// 延期相关
	IsDelayed    bool   `json:"is_delayed"`     // 是否延期
	DelayReason  string `json:"delay_reason"`   // 延期分类: difficulty, workload, dependency, requirement_change, other
	DelayDesc    string `json:"delay_desc"`     // 延期说明
	DelayEndTime string `json:"delay_end_time"` // 延期后结束时间
	// 完成分类
	CompletionType string `json:"completion_type"` // 完成分类: normal, delayed (自动计算)
	CreatedAt      string `json:"created_at"`
	CreatedBy      string `json:"created_by"`
	UpdatedAt      string `json:"updated_at"`
	UpdatedBy      string `json:"updated_by"`
}

// Incident 员工失误/异常记录
type Incident struct {
	ID            string `json:"id"`
	IncidentTime  string `json:"incident_time"`  // 发生时间
	Operator      string `json:"operator"`       // 操作人
	OperationType string `json:"operation_type"` // 操作类型: deploy, config_change, db_operation, permission, network, monitoring, other
	OperationDesc string `json:"operation_desc"` // 操作描述
	Status        string `json:"status"`         // 状态: pending, resolved, closed
	Severity      string `json:"severity"`       // 严重程度: low, medium, high, critical
	Reason        string `json:"reason"`         // 异常原因
	Impact        string `json:"impact"`         // 影响范围
	Solution      string `json:"solution"`       // 解决方案
	Checker       string `json:"checker"`        // 检查人
	CheckTime     string `json:"check_time"`     // 检查时间
	CheckResult   string `json:"check_result"`   // 检查结果
	Remark        string `json:"remark"`         // 备注
	CreatedAt     string `json:"created_at"`
	CreatedBy     string `json:"created_by"`
	UpdatedAt     string `json:"updated_at"`
	UpdatedBy     string `json:"updated_by"`
}

// DutyProject 值班项目配置
type DutyProject struct {
	ID          string `json:"id"`
	Name        string `json:"name"`        // 项目名称
	Code        string `json:"code"`        // 项目代码
	Description string `json:"description"` // 项目描述
	Status      string `json:"status"`      // 状态: active, disabled
	SortOrder   int    `json:"sort_order"`  // 排序
	CreatedAt   string `json:"created_at"`
	CreatedBy   string `json:"created_by"`
	UpdatedAt   string `json:"updated_at"`
}

// DutyRecord 值班记录
type DutyRecord struct {
	ID           string   `json:"id"`
	DutyDate     string   `json:"duty_date"`     // 值班日期时间
	DutyPerson   string   `json:"duty_person"`   // 值班人
	ProjectID    string   `json:"project_id"`    // 项目ID
	ProjectName  string   `json:"project_name"`  // 项目名称（关联查询）
	TaskDesc     string   `json:"task_desc"`     // 任务描述
	FeedbackType string   `json:"feedback_type"` // 反馈类型: proactive=主动反馈, customer=客户反馈
	EventType    string   `json:"event_type"`    // 事件类型: inspection=巡检发现, alert=监控告警, customer_feedback=客户反馈, proactive_check=值班人员主动排查
	Handler      string   `json:"handler"`       // 处理人
	HandleResult string   `json:"handle_result"` // 处理结果
	ProblemDesc  string   `json:"problem_desc"`  // 问题描述

	FirstCallTime string `json:"first_call_time"` // 首次拨打时间
	AnswerTime    string `json:"answer_time"`     // 接听时间
	CallCount     int    `json:"call_count"`      // 拨打次数
	IsAnswered    bool   `json:"is_answered"`     // 是否接听
	ResponseTime  int    `json:"response_time"`   // 响应时间(分钟)

	IsEscalated bool   `json:"is_escalated"` // 是否升级问题
	EscalateTo  string `json:"escalate_to"`  // 升级给谁: leader=组长, hod=HOD

	HasHandover     bool   `json:"has_handover"`     // 是否有工作交接
	HandoverPerson  string `json:"handover_person"`  // 工作交接人
	HandoverContent string `json:"handover_content"` // 工作交接内容

	Status                string   `json:"status"`                   // 状态: resolved=已解决, unresolved=未解决, pending=待解决, temporary=临时解决
	PlannedFixTime        string   `json:"planned_fix_time"`         // 计划修复时间
	PlannedFixTimeEdited  bool     `json:"planned_fix_time_edited"`  // 计划修复时间是否被编辑过
	IsOverdue             bool     `json:"is_overdue"`               // 是否逾期
	OverdueReason  string   `json:"overdue_reason"`   // 逾期原因
	Attachments    []string `json:"attachments"`      // 附件列表(图片URL数组)

	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

// WebsiteHall 甲方（厅方/客户方）信息
type WebsiteHall struct {
	ID        string `json:"id"`
	WebsiteID string `json:"website_id"`         // 关联的网站ID
	HallName  string `json:"hall_name"`          // 厅方名称
	Contact   string `json:"contact"`            // 联系人
	Phone     string `json:"phone"`              // 联系电话
	Username  string `json:"username"`           // 登录用户名
	Password  string `json:"password,omitempty"` // 登录密码(加密存储)
	Remark    string `json:"remark"`             // 备注
	Status    string `json:"status"`             // 状态: active, inactive
	CreatedAt string `json:"created_at"`
	CreatedBy string `json:"created_by"`
	UpdatedAt string `json:"updated_at"`
	UpdatedBy string `json:"updated_by"`
}

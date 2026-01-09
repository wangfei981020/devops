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
	DestIP       string `json:"dest_ip"`
	Port         string `json:"port"`
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
	Role        string `json:"role"`
	Status      string `json:"status"`
	Permissions string `json:"permissions"`
	MFAEnabled  bool   `json:"mfa_enabled"`          // 是否启用 MFA（管理员配置）
	MFASecret   string `json:"mfa_secret,omitempty"` // MFA 密钥（不返回给前端）
	MFABound    bool   `json:"mfa_bound"`            // 是否已绑定 MFA（有密钥即为已绑定）
	CreatedAt   string `json:"created_at"`
}

// AuditLog 审计日志
type AuditLog struct {
	ID        string `json:"id"`
	Action    string `json:"action"`
	RecordID  string `json:"record_id"`
	Operator  string `json:"operator"`
	OldData   string `json:"old_data"`
	NewData   string `json:"new_data"`
	Changes   string `json:"changes"`
	IP        string `json:"ip"`
	CreatedAt string `json:"created_at"`
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
	CDNProvider    string `json:"cdn_provider"`     // CDN 厂商
	ExpireTime     string `json:"expire_time"`      // 域名到期时间
	CertExpireTime string `json:"cert_expire_time"` // 证书到期时间
	Status         string `json:"status"`           // 状态：active, inactive, expired
	Remark         string `json:"remark"`           // 备注
	CreatedAt      string `json:"created_at"`
	CreatedBy      string `json:"created_by"`
	UpdatedAt      string `json:"updated_at"`
	UpdatedBy      string `json:"updated_by"`
}

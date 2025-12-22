package models

// Record 数据源ID记录
type Record struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"` // 连接ID，唯一标识
	Project      string `json:"project"`
	Env          string `json:"env"`
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

package models

// User 用户模型
type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Password    string `json:"password,omitempty"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	Status      string `json:"status"`
	OIDCSub     string `json:"oidc_sub,omitempty"`
	AuthSource  string `json:"auth_source"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at,omitempty"`
}

// SystemSetting 系统设置
type SystemSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// AuditLog 审计日志
type AuditLog struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	Action     string `json:"action"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Detail     string `json:"detail"`
	IP         string `json:"ip"`
	CreatedAt  string `json:"created_at"`
}

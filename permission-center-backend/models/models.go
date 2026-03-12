package models

import "time"

// User synced from SSO (no password stored here)
type User struct {
	ID          string     `json:"id"`
	Username    string     `json:"username"`
	DisplayName string     `json:"display_name"`
	Email       string     `json:"email"`
	Source      string     `json:"source"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastSyncAt  *time.Time `json:"last_sync_at,omitempty"`
}

// Role definition
type Role struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsSystem    bool   `json:"is_system"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	UserCount   int    `json:"user_count,omitempty"`
}

// Permission with service grouping
type Permission struct {
	ID          string       `json:"id"`
	ServiceCode string       `json:"service_code"`
	Code        string       `json:"code"`
	Name        string       `json:"name"`
	Type        string       `json:"type"`
	Resource    string       `json:"resource,omitempty"`
	ParentID    string       `json:"parent_id,omitempty"`
	Icon        string       `json:"icon,omitempty"`
	SortOrder   int          `json:"sort_order"`
	Description string       `json:"description,omitempty"`
	CreatedAt   string       `json:"created_at"`
	Children    []Permission `json:"children,omitempty"`
}

// Service registration
type Service struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	HomeURL     string `json:"home_url"`
	Icon        string `json:"icon"`
	Status      string `json:"status"`
	APIKey      string `json:"api_key,omitempty"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

// AuditLog entry
type AuditLog struct {
	ID         string `json:"id"`
	UserID     string `json:"user_id"`
	Username   string `json:"username"`
	Action     string `json:"action"`
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
	Detail     string `json:"detail"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent"`
	Status     string `json:"status"`
	CreatedAt  string `json:"created_at"`
}

// UserRole association
type UserRole struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	RoleID    string `json:"role_id"`
	RoleName  string `json:"role_name,omitempty"`
	CreatedAt string `json:"created_at"`
}

// VerifyRequest for /api/verify
type VerifyRequest struct {
	UserID      string `json:"user_id"`
	ServiceCode string `json:"service_code"`
}

// VerifyResponse for /api/verify
type VerifyResponse struct {
	Permissions map[string]bool `json:"permissions"`
	Menus       []Permission    `json:"menus"`
	Roles       []string        `json:"roles"`
}

// CheckRequest for /api/check
type CheckRequest struct {
	UserID         string `json:"user_id"`
	ServiceCode    string `json:"service_code"`
	PermissionCode string `json:"permission_code"`
}

// CheckResponse for /api/check
type CheckResponse struct {
	Allowed bool `json:"allowed"`
}

// SyncUserRequest for /api/sync/user
type SyncUserRequest struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	Source      string `json:"source"`
}

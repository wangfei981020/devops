package models

import "time"

// User SSO用户
type User struct {
	ID               string     `json:"id"`
	Username         string     `json:"username"`
	Password         string     `json:"-"`
	DisplayName      string     `json:"display_name"`
	Email            string     `json:"email"`
	Phone            string     `json:"phone"`
	Avatar           string     `json:"avatar"`
	Language         string     `json:"language"`
	Status           string     `json:"status"` // active, disabled, locked
	AuthSource       string     `json:"auth_source"` // local, oidc, ldap
	ExternalID       string     `json:"external_id"`
	ExternalProvider string     `json:"external_provider"`
	MFAEnabled       bool       `json:"mfa_enabled"`
	MFASecret        string     `json:"-"`
	LastLoginAt      *time.Time `json:"last_login_at"`
	LastLoginIP      string     `json:"last_login_ip"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Role SSO角色
type Role struct {
	ID          string    `json:"id"`
	Code        string    `json:"code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsSystem    bool      `json:"is_system"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

// Permission SSO权限
type Permission struct {
	ID          string `json:"id"`
	ClientID    string `json:"client_id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Type        string `json:"type"` // menu, button, data, api
	Resource    string `json:"resource"`
	ParentID    string `json:"parent_id"`
	Icon        string `json:"icon"`
	SortOrder   int    `json:"sort_order"`
	Description string `json:"description"`
}

// Client OIDC客户端/服务
type Client struct {
	ID                 string    `json:"id"`
	ClientID           string    `json:"client_id"`
	ClientSecret       string    `json:"-"`
	ClientName         string    `json:"client_name"`
	Description        string    `json:"description"`
	Icon               string    `json:"icon"`
	HomeURL            string    `json:"home_url"`
	RedirectURIs       string    `json:"redirect_uris"` // JSON array
	AllowedScopes      string    `json:"allowed_scopes"`
	TokenExpiry        int       `json:"token_expiry"`
	RefreshTokenExpiry int       `json:"refresh_token_expiry"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// Session SSO会话
type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	TokenHash string    `json:"token_hash"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// AuthCode OIDC授权码
type AuthCode struct {
	Code                string    `json:"code"`
	ClientID            string    `json:"client_id"`
	UserID              string    `json:"user_id"`
	RedirectURI         string    `json:"redirect_uri"`
	Scope               string    `json:"scope"`
	State               string    `json:"state"`
	Nonce               string    `json:"nonce"`
	CodeChallenge       string    `json:"code_challenge"`
	CodeChallengeMethod string    `json:"code_challenge_method"`
	ExpiresAt           time.Time `json:"expires_at"`
}

// RefreshToken
type RefreshToken struct {
	ID        string    `json:"id"`
	TokenHash string    `json:"token_hash"`
	UserID    string    `json:"user_id"`
	ClientID  string    `json:"client_id"`
	Scope     string    `json:"scope"`
	ExpiresAt time.Time `json:"expires_at"`
}

// AuditLog 审计日志
type AuditLog struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Username   string    `json:"username"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Detail     string    `json:"detail"`
	IP         string    `json:"ip"`
	UserAgent  string    `json:"user_agent"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

// Setting 系统配置
type Setting struct {
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PortalService 门户服务卡片
type PortalService struct {
	ClientID    string `json:"client_id"`
	ClientName  string `json:"client_name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	HomeURL     string `json:"home_url"`
}

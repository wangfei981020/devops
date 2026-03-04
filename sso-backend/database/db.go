package database

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"

	"sso-backend/config"
)

var DB *sql.DB

func InitDB() {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		config.DBUser, config.DBPassword, config.DBHost, config.DBPort, config.DBName)

	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal("[DB] Failed to open:", err)
	}

	DB.SetMaxOpenConns(20)
	DB.SetMaxIdleConns(5)
	DB.SetConnMaxLifetime(5 * time.Minute)

	if err = DB.Ping(); err != nil {
		log.Fatal("[DB] Failed to ping:", err)
	}
	log.Println("[DB] MySQL connected")

	createTables()
	initDefaultData()
}

func createTables() {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS sso_users (
			id VARCHAR(64) PRIMARY KEY,
			username VARCHAR(128) NOT NULL UNIQUE,
			password VARCHAR(255) NOT NULL DEFAULT '',
			display_name VARCHAR(128) NOT NULL DEFAULT '',
			email VARCHAR(255) DEFAULT '',
			phone VARCHAR(32) DEFAULT '',
			avatar VARCHAR(512) DEFAULT '',
			language VARCHAR(10) DEFAULT 'zh-CN',
			status ENUM('active','disabled','locked') DEFAULT 'active',
			auth_source VARCHAR(32) DEFAULT 'local',
			external_id VARCHAR(255) DEFAULT '',
			external_provider VARCHAR(64) DEFAULT '',
			mfa_enabled TINYINT(1) DEFAULT 0,
			mfa_secret VARCHAR(64) DEFAULT '',
			last_login_at DATETIME DEFAULT NULL,
			last_login_ip VARCHAR(64) DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_email (email),
			INDEX idx_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS sso_roles (
			id VARCHAR(64) PRIMARY KEY,
			code VARCHAR(64) NOT NULL UNIQUE,
			name VARCHAR(128) NOT NULL,
			description TEXT,
			is_system TINYINT(1) DEFAULT 0,
			status ENUM('active','disabled') DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS sso_user_roles (
			id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64) NOT NULL,
			role_id VARCHAR(64) NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_user (user_id),
			INDEX idx_role (role_id),
			UNIQUE KEY uk_user_role (user_id, role_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS sso_permissions (
			id VARCHAR(64) PRIMARY KEY,
			client_id VARCHAR(128) DEFAULT '',
			code VARCHAR(128) NOT NULL,
			name VARCHAR(128) NOT NULL,
			type ENUM('menu','button','data','api') NOT NULL,
			resource VARCHAR(255) DEFAULT '',
			parent_id VARCHAR(64) DEFAULT '',
			icon VARCHAR(64) DEFAULT '',
			sort_order INT DEFAULT 0,
			description TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY uk_client_code (client_id, code),
			INDEX idx_type (type),
			INDEX idx_client (client_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS sso_role_permissions (
			id VARCHAR(64) PRIMARY KEY,
			role_id VARCHAR(64) NOT NULL,
			permission_id VARCHAR(64) NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_role (role_id),
			UNIQUE KEY uk_role_permission (role_id, permission_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS sso_clients (
			id VARCHAR(64) PRIMARY KEY,
			client_id VARCHAR(128) NOT NULL UNIQUE,
			client_secret VARCHAR(255) NOT NULL,
			client_name VARCHAR(128) NOT NULL,
			description TEXT,
			icon VARCHAR(512) DEFAULT '',
			home_url VARCHAR(512) DEFAULT '',
			redirect_uris TEXT NOT NULL,
			allowed_scopes VARCHAR(512) DEFAULT 'openid profile email',
			token_expiry INT DEFAULT 3600,
			refresh_token_expiry INT DEFAULT 86400,
			status ENUM('active','disabled') DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS sso_user_clients (
			id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64) NOT NULL,
			client_id VARCHAR(128) NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY uk_user_client (user_id, client_id),
			INDEX idx_user (user_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS sso_sessions (
			id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64) NOT NULL,
			token_hash VARCHAR(128) NOT NULL,
			ip VARCHAR(64) DEFAULT '',
			user_agent TEXT,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_user (user_id),
			INDEX idx_token_hash (token_hash),
			INDEX idx_expires (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS sso_auth_codes (
			code VARCHAR(128) PRIMARY KEY,
			client_id VARCHAR(128) NOT NULL,
			user_id VARCHAR(64) NOT NULL,
			redirect_uri VARCHAR(512) NOT NULL,
			scope VARCHAR(512) DEFAULT '',
			state VARCHAR(512) DEFAULT '',
			nonce VARCHAR(255) DEFAULT '',
			code_challenge VARCHAR(255) DEFAULT '',
			code_challenge_method VARCHAR(10) DEFAULT '',
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_expires (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS sso_refresh_tokens (
			id VARCHAR(64) PRIMARY KEY,
			token_hash VARCHAR(128) NOT NULL UNIQUE,
			user_id VARCHAR(64) NOT NULL,
			client_id VARCHAR(128) NOT NULL,
			scope VARCHAR(512) DEFAULT '',
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_user (user_id),
			INDEX idx_expires (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS sso_audit_logs (
			id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64) DEFAULT '',
			username VARCHAR(128) DEFAULT '',
			action VARCHAR(64) NOT NULL,
			target_type VARCHAR(32) DEFAULT '',
			target_id VARCHAR(64) DEFAULT '',
			detail TEXT,
			ip VARCHAR(64) DEFAULT '',
			user_agent TEXT,
			status VARCHAR(16) DEFAULT 'success',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_user (user_id),
			INDEX idx_action (action),
			INDEX idx_created (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS sso_settings (
			setting_key VARCHAR(128) PRIMARY KEY,
			setting_value TEXT NOT NULL,
			description VARCHAR(255) DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}

	for _, t := range tables {
		if _, err := DB.Exec(t); err != nil {
			log.Printf("[DB] Warning: %v", err)
		}
	}
	log.Println("[DB] Tables ensured")
}

func initDefaultData() {
	var count int
	// Default admin user
	DB.QueryRow("SELECT COUNT(*) FROM sso_users WHERE username='admin'").Scan(&count)
	if count == 0 {
		hash, _ := bcrypt.GenerateFromPassword([]byte(config.DefaultAdminPassword), bcrypt.DefaultCost)
		DB.Exec(`INSERT INTO sso_users (id, username, password, display_name, email, status, auth_source)
			VALUES ('sso-admin-001', 'admin', ?, 'SSO Admin', 'admin@example.com', 'active', 'local')`, string(hash))
		log.Println("[DB] Default admin user created (admin / " + config.DefaultAdminPassword + ")")
	}

	// Default roles
	DB.QueryRow("SELECT COUNT(*) FROM sso_roles WHERE code='super_admin'").Scan(&count)
	if count == 0 {
		DB.Exec(`INSERT INTO sso_roles (id, code, name, description, is_system, status) VALUES
			('role-super-admin', 'super_admin', '超级管理员', '系统超级管理员', 1, 'active')`)
		DB.Exec(`INSERT INTO sso_roles (id, code, name, description, is_system, status) VALUES
			('role-admin', 'admin', '管理员', '系统管理员', 0, 'active')`)
		DB.Exec(`INSERT INTO sso_roles (id, code, name, description, is_system, status) VALUES
			('role-user', 'user', '普通用户', '普通用户', 0, 'active')`)
		DB.Exec(`INSERT INTO sso_user_roles (id, user_id, role_id) VALUES ('ur-001', 'sso-admin-001', 'role-super-admin')`)
		log.Println("[DB] Default roles created")
	}

	// Default opsplatform client
	DB.QueryRow("SELECT COUNT(*) FROM sso_clients WHERE client_id='opsplatform'").Scan(&count)
	if count == 0 {
		DB.Exec(`INSERT INTO sso_clients (id, client_id, client_secret, client_name, description, icon, home_url, redirect_uris, status)
			VALUES ('client-opsplatform', 'opsplatform', 'opsplatform-secret', '运维平台', '运维管理平台', 'settings', 'http://localhost:30080', '["http://localhost:30080/api/oidc/callback"]', 'active')`)
		DB.Exec(`INSERT INTO sso_user_clients (id, user_id, client_id) VALUES ('uc-001', 'sso-admin-001', 'opsplatform')`)
		log.Println("[DB] Default opsplatform client created")
	}
}

// --- Utility functions ---

func GenerateID(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(b))
}

func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// --- Session functions ---

func CreateSession(id, userID, tokenHash, ip, userAgent string, expiresAt time.Time) error {
	_, err := DB.Exec(`INSERT INTO sso_sessions (id, user_id, token_hash, ip, user_agent, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id, userID, tokenHash, ip, userAgent, expiresAt)
	return err
}

func ValidateSession(tokenHash string) (string, string, error) {
	var userID, username string
	err := DB.QueryRow(`SELECT s.user_id, u.username FROM sso_sessions s
		JOIN sso_users u ON s.user_id = u.id
		WHERE s.token_hash = ? AND s.expires_at > NOW() AND u.status = 'active'`,
		tokenHash).Scan(&userID, &username)
	return userID, username, err
}

func DeleteSessionByHash(tokenHash string) error {
	_, err := DB.Exec("DELETE FROM sso_sessions WHERE token_hash = ?", tokenHash)
	return err
}

func DeleteUserSessions(userID string) error {
	_, err := DB.Exec("DELETE FROM sso_sessions WHERE user_id = ?", userID)
	return err
}

// --- User functions ---

type UserRow struct {
	ID          string
	Username    string
	Password    string
	DisplayName string
	Email       string
	Phone       string
	Avatar      string
	Language    string
	Status      string
	AuthSource  string
	MFAEnabled  bool
	MFASecret   string
}

func GetUserByUsername(username string) (*UserRow, error) {
	u := &UserRow{}
	err := DB.QueryRow(`SELECT id, username, password, display_name, email, phone, avatar, language, status, auth_source, mfa_enabled, COALESCE(mfa_secret,'')
		FROM sso_users WHERE username = ?`, username).Scan(
		&u.ID, &u.Username, &u.Password, &u.DisplayName, &u.Email, &u.Phone, &u.Avatar, &u.Language, &u.Status, &u.AuthSource, &u.MFAEnabled, &u.MFASecret)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func GetUserByID(id string) (*UserRow, error) {
	u := &UserRow{}
	err := DB.QueryRow(`SELECT id, username, password, display_name, email, phone, avatar, language, status, auth_source, mfa_enabled, COALESCE(mfa_secret,'')
		FROM sso_users WHERE id = ?`, id).Scan(
		&u.ID, &u.Username, &u.Password, &u.DisplayName, &u.Email, &u.Phone, &u.Avatar, &u.Language, &u.Status, &u.AuthSource, &u.MFAEnabled, &u.MFASecret)
	if err != nil {
		return nil, err
	}
	return u, nil
}

func UpdateUserLastLogin(userID, ip string) error {
	_, err := DB.Exec("UPDATE sso_users SET last_login_at = NOW(), last_login_ip = ? WHERE id = ?", ip, userID)
	return err
}

// --- Role/Permission functions ---

func IsUserAdmin(userID string) (bool, error) {
	var count int
	err := DB.QueryRow(`SELECT COUNT(*) FROM sso_user_roles ur
		JOIN sso_roles r ON ur.role_id = r.id
		WHERE ur.user_id = ? AND r.code IN ('super_admin', 'admin') AND r.status = 'active'`, userID).Scan(&count)
	return count > 0, err
}

func GetUserRoleCodes(userID string) ([]string, error) {
	rows, err := DB.Query(`SELECT r.code FROM sso_user_roles ur
		JOIN sso_roles r ON ur.role_id = r.id
		WHERE ur.user_id = ? AND r.status = 'active'`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var codes []string
	for rows.Next() {
		var code string
		rows.Scan(&code)
		codes = append(codes, code)
	}
	return codes, nil
}

// --- Client functions ---

type ClientRow struct {
	ID                 string
	ClientID           string
	ClientSecret       string
	ClientName         string
	Description        string
	Icon               string
	HomeURL            string
	RedirectURIs       string
	AllowedScopes      string
	TokenExpiry        int
	RefreshTokenExpiry int
	Status             string
}

func GetClientByClientID(clientID string) (*ClientRow, error) {
	c := &ClientRow{}
	err := DB.QueryRow(`SELECT id, client_id, client_secret, client_name, COALESCE(description,''), COALESCE(icon,''), COALESCE(home_url,''), redirect_uris, COALESCE(allowed_scopes,''), token_expiry, refresh_token_expiry, status
		FROM sso_clients WHERE client_id = ? AND status = 'active'`, clientID).Scan(
		&c.ID, &c.ClientID, &c.ClientSecret, &c.ClientName, &c.Description, &c.Icon, &c.HomeURL, &c.RedirectURIs, &c.AllowedScopes, &c.TokenExpiry, &c.RefreshTokenExpiry, &c.Status)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// --- Auth Code functions ---

func SaveAuthCode(code, clientID, userID, redirectURI, scope, state, nonce string, expiresAt time.Time) error {
	_, err := DB.Exec(`INSERT INTO sso_auth_codes (code, client_id, user_id, redirect_uri, scope, state, nonce, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, code, clientID, userID, redirectURI, scope, state, nonce, expiresAt)
	return err
}

type AuthCodeRow struct {
	Code        string
	ClientID    string
	UserID      string
	RedirectURI string
	Scope       string
	State       string
	Nonce       string
	ExpiresAt   time.Time
}

func GetAndDeleteAuthCode(code string) (*AuthCodeRow, error) {
	ac := &AuthCodeRow{}
	err := DB.QueryRow(`SELECT code, client_id, user_id, redirect_uri, COALESCE(scope,''), COALESCE(state,''), COALESCE(nonce,''), expires_at
		FROM sso_auth_codes WHERE code = ? AND expires_at > NOW()`, code).Scan(
		&ac.Code, &ac.ClientID, &ac.UserID, &ac.RedirectURI, &ac.Scope, &ac.State, &ac.Nonce, &ac.ExpiresAt)
	if err != nil {
		return nil, err
	}
	// Delete (one-time use)
	DB.Exec("DELETE FROM sso_auth_codes WHERE code = ?", code)
	return ac, nil
}

// --- Portal functions ---

type PortalServiceRow struct {
	ClientID    string `json:"client_id"`
	ClientName  string `json:"client_name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	HomeURL     string `json:"home_url"`
}

func GetUserPortalServices(userID string) ([]PortalServiceRow, error) {
	// Admin users can access all active clients
	isAdmin, _ := IsUserAdmin(userID)

	var rows *sql.Rows
	var err error

	if isAdmin {
		rows, err = DB.Query(`SELECT client_id, client_name, COALESCE(description,''), COALESCE(icon,''), COALESCE(home_url,'')
			FROM sso_clients WHERE status = 'active' ORDER BY client_name`)
	} else {
		rows, err = DB.Query(`SELECT c.client_id, c.client_name, COALESCE(c.description,''), COALESCE(c.icon,''), COALESCE(c.home_url,'')
			FROM sso_user_clients uc JOIN sso_clients c ON uc.client_id = c.client_id
			WHERE uc.user_id = ? AND c.status = 'active' ORDER BY c.client_name`, userID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var services []PortalServiceRow
	for rows.Next() {
		var s PortalServiceRow
		rows.Scan(&s.ClientID, &s.ClientName, &s.Description, &s.Icon, &s.HomeURL)
		services = append(services, s)
	}
	return services, nil
}

// --- Audit functions ---

func SaveAuditLog(userID, username, action, targetType, targetID, detail, ip, userAgent, status string) {
	id := GenerateID("audit")
	DB.Exec(`INSERT INTO sso_audit_logs (id, user_id, username, action, target_type, target_id, detail, ip, user_agent, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, userID, username, action, targetType, targetID, detail, ip, userAgent, status)
}

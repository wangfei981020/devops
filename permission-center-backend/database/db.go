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

	"permission-center-backend/cache"
	"permission-center-backend/config"
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
		// Users (synced from SSO, no password)
		`CREATE TABLE IF NOT EXISTS pc_users (
			id VARCHAR(64) PRIMARY KEY,
			username VARCHAR(128) NOT NULL UNIQUE,
			display_name VARCHAR(128) NOT NULL DEFAULT '',
			email VARCHAR(255) DEFAULT '',
			source VARCHAR(32) DEFAULT 'sso',
			status ENUM('active','disabled') DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			last_sync_at DATETIME DEFAULT NULL,
			INDEX idx_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// Roles
		`CREATE TABLE IF NOT EXISTS pc_roles (
			id VARCHAR(64) PRIMARY KEY,
			code VARCHAR(64) NOT NULL UNIQUE,
			name VARCHAR(128) NOT NULL,
			description TEXT,
			is_system TINYINT(1) DEFAULT 0,
			status ENUM('active','disabled') DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// User-Role association
		`CREATE TABLE IF NOT EXISTS pc_user_roles (
			id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64) NOT NULL,
			role_id VARCHAR(64) NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_user (user_id),
			INDEX idx_role (role_id),
			UNIQUE KEY uk_user_role (user_id, role_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// Permissions (grouped by service_code)
		`CREATE TABLE IF NOT EXISTS pc_permissions (
			id VARCHAR(64) PRIMARY KEY,
			service_code VARCHAR(128) NOT NULL DEFAULT '',
			code VARCHAR(128) NOT NULL,
			name VARCHAR(128) NOT NULL,
			type ENUM('menu','button','data','api') NOT NULL,
			resource VARCHAR(255) DEFAULT '',
			parent_id VARCHAR(64) DEFAULT '',
			icon VARCHAR(64) DEFAULT '',
			sort_order INT DEFAULT 0,
			description TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY uk_service_code (service_code, code),
			INDEX idx_type (type),
			INDEX idx_service (service_code),
			INDEX idx_parent (parent_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// Role-Permission association
		`CREATE TABLE IF NOT EXISTS pc_role_permissions (
			id VARCHAR(64) PRIMARY KEY,
			role_id VARCHAR(64) NOT NULL,
			permission_id VARCHAR(64) NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_role (role_id),
			UNIQUE KEY uk_role_permission (role_id, permission_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// Services (registered applications)
		`CREATE TABLE IF NOT EXISTS pc_services (
			id VARCHAR(64) PRIMARY KEY,
			code VARCHAR(128) NOT NULL UNIQUE,
			name VARCHAR(128) NOT NULL,
			description TEXT,
			home_url VARCHAR(512) DEFAULT '',
			icon VARCHAR(64) DEFAULT '',
			status ENUM('active','disabled') DEFAULT 'active',
			api_key VARCHAR(128) NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// Audit logs
		`CREATE TABLE IF NOT EXISTS pc_audit_logs (
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

		// Settings
		`CREATE TABLE IF NOT EXISTS pc_settings (
			setting_key VARCHAR(128) PRIMARY KEY,
			setting_value TEXT NOT NULL,
			description VARCHAR(255) DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// Sessions (for local login)
		`CREATE TABLE IF NOT EXISTS pc_sessions (
			id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64) NOT NULL,
			token_hash VARCHAR(128) NOT NULL,
			ip VARCHAR(64) DEFAULT '',
			user_agent TEXT,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_token_hash (token_hash),
			INDEX idx_expires (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}

	for _, t := range tables {
		if _, err := DB.Exec(t); err != nil {
			log.Printf("[DB] Warning: %v", err)
		}
	}

	// Migration: add password_hash column to pc_users (ignore error if already exists)
	DB.Exec(`ALTER TABLE pc_users ADD COLUMN password_hash VARCHAR(255) NOT NULL DEFAULT '' AFTER source`)

	log.Println("[DB] Tables ensured")
}

func initDefaultData() {
	var count int

	// Default admin user (local user with password)
	DB.QueryRow("SELECT COUNT(*) FROM pc_users WHERE username='admin'").Scan(&count)
	if count == 0 {
		hash, _ := bcrypt.GenerateFromPassword([]byte(config.DefaultAdminPassword), bcrypt.DefaultCost)
		DB.Exec(`INSERT INTO pc_users (id, username, display_name, email, source, password_hash, status)
			VALUES ('pc-admin-001', 'admin', '系统管理员', 'admin@example.com', 'local', ?, 'active')`, string(hash))
		log.Println("[DB] Default admin user created")
	} else {
		// Migration: set password for existing admin if empty
		var existingHash string
		DB.QueryRow("SELECT password_hash FROM pc_users WHERE username='admin'").Scan(&existingHash)
		if existingHash == "" {
			hash, _ := bcrypt.GenerateFromPassword([]byte(config.DefaultAdminPassword), bcrypt.DefaultCost)
			DB.Exec("UPDATE pc_users SET password_hash = ? WHERE username='admin'", string(hash))
			log.Println("[DB] Default admin password set")
		}
	}

	// Default roles (matching opsplatform)
	DB.QueryRow("SELECT COUNT(*) FROM pc_roles WHERE code='super_admin'").Scan(&count)
	if count == 0 {
		roles := []struct {
			id, code, name, desc string
			isSystem             int
		}{
			{"role_super_admin", "super_admin", "超级管理员", "系统超级管理员，拥有所有权限", 1},
			{"role_admin", "admin", "管理员", "系统管理员", 1},
			{"role_operator", "operator", "操作员", "资源管理与操作", 0},
			{"role_viewer", "viewer", "只读用户", "只读访问权限", 0},
		}
		for _, r := range roles {
			DB.Exec(`INSERT INTO pc_roles (id, code, name, description, is_system, status) VALUES (?, ?, ?, ?, ?, 'active')`,
				r.id, r.code, r.name, r.desc, r.isSystem)
		}
		// Assign super_admin role to admin user
		DB.Exec(`INSERT INTO pc_user_roles (id, user_id, role_id) VALUES ('ur-001', 'pc-admin-001', 'role_super_admin')`)
		log.Println("[DB] Default roles created")
	}

	// Default opsplatform service
	DB.QueryRow("SELECT COUNT(*) FROM pc_services WHERE code='opsplatform'").Scan(&count)
	if count == 0 {
		apiKey := generateAPIKey()
		DB.Exec(`INSERT INTO pc_services (id, code, name, description, home_url, icon, status, api_key)
			VALUES ('svc-opsplatform', 'opsplatform', '运维平台', '运维管理平台', 'http://localhost:30081', 'settings', 'active', ?)`, apiKey)
		log.Printf("[DB] Default opsplatform service created (api_key: %s)", apiKey)
	}

	// Default permissions for opsplatform (matching current opsplatform sidebar)
	DB.QueryRow("SELECT COUNT(*) FROM pc_permissions WHERE service_code='opsplatform'").Scan(&count)
	if count == 0 {
		initOpsplatformPermissions()
		log.Println("[DB] Default opsplatform permissions created")
	}

	// Default settings
	DB.QueryRow("SELECT COUNT(*) FROM pc_settings WHERE setting_key='local_login_enabled'").Scan(&count)
	if count == 0 {
		DB.Exec(`INSERT INTO pc_settings (setting_key, setting_value, description) VALUES ('local_login_enabled', 'false', '是否启用本地用户登录')`)
		log.Println("[DB] Default settings created")
	}
}

func initOpsplatformPermissions() {
	// Menu permissions matching opsplatform Sidebar.vue
	menus := []struct {
		id, code, name, parentID, icon string
		sortOrder                      int
	}{
		// Top-level groups
		{"perm-m-system", "menu:system", "系统管理", "", "🏠", 100},
		{"perm-m-ops", "menu:operations", "运维管理", "", "🔧", 200},
		{"perm-m-stats", "menu:statistics", "统计分析", "", "📊", 300},
		{"perm-m-table", "menu:table", "桌台管理", "", "🎰", 400},

		// System management items
		{"perm-m-welcome", "menu:welcome", "欢迎页", "perm-m-system", "🏠", 101},
		{"perm-m-users", "menu:users", "用户管理", "perm-m-system", "👥", 102},
		{"perm-m-roles", "menu:roles", "角色管理", "perm-m-system", "🔑", 103},
		{"perm-m-permissions", "menu:permissions", "权限管理", "perm-m-system", "🛡️", 104},
		{"perm-m-settings", "menu:settings", "系统设置", "perm-m-system", "⚙️", 105},
		{"perm-m-audit", "menu:audit_logs", "审计日志", "perm-m-system", "📋", 106},

		// Operations items
		{"perm-m-duty", "menu:duty", "排班表", "perm-m-ops", "📅", 201},
		{"perm-m-duty-records", "menu:duty_records", "值班记录", "perm-m-ops", "📝", 202},
		{"perm-m-upload", "menu:upload", "文件上传", "perm-m-ops", "📤", 203},
		{"perm-m-files", "menu:files", "文件管理", "perm-m-ops", "📁", 204},

		// Statistics items
		{"perm-m-statistics", "menu:statistics_page", "统计报表", "perm-m-stats", "📊", 301},

		// Table management items
		{"perm-m-table-maint", "menu:table_maintenance", "桌台维护记录", "perm-m-table", "🔧", 401},
		{"perm-m-table-config", "menu:table_hierarchy_config", "桌台层级配置", "perm-m-table", "⚙️", 402},
	}

	for _, m := range menus {
		DB.Exec(`INSERT INTO pc_permissions (id, service_code, code, name, type, parent_id, icon, sort_order)
			VALUES (?, 'opsplatform', ?, ?, 'menu', ?, ?, ?)`,
			m.id, m.code, m.name, m.parentID, m.icon, m.sortOrder)
	}

	// Assign all permissions to super_admin and admin roles
	var permIDs []string
	rows, _ := DB.Query("SELECT id FROM pc_permissions WHERE service_code='opsplatform'")
	if rows != nil {
		for rows.Next() {
			var id string
			rows.Scan(&id)
			permIDs = append(permIDs, id)
		}
		rows.Close()
	}

	for _, permID := range permIDs {
		// super_admin gets all
		DB.Exec(`INSERT IGNORE INTO pc_role_permissions (id, role_id, permission_id) VALUES (?, 'role_super_admin', ?)`,
			GenerateID("rp"), permID)
		// admin gets all
		DB.Exec(`INSERT IGNORE INTO pc_role_permissions (id, role_id, permission_id) VALUES (?, 'role_admin', ?)`,
			GenerateID("rp"), permID)
	}

	// operator gets operations + table menus
	operatorPerms := []string{
		"perm-m-welcome", "perm-m-ops", "perm-m-duty", "perm-m-duty-records",
		"perm-m-upload", "perm-m-files", "perm-m-table", "perm-m-table-maint",
		"perm-m-table-config", "perm-m-stats", "perm-m-statistics",
	}
	for _, permID := range operatorPerms {
		DB.Exec(`INSERT IGNORE INTO pc_role_permissions (id, role_id, permission_id) VALUES (?, 'role_operator', ?)`,
			GenerateID("rp"), permID)
	}

	// viewer gets welcome only
	DB.Exec(`INSERT IGNORE INTO pc_role_permissions (id, role_id, permission_id) VALUES (?, 'role_viewer', 'perm-m-welcome')`,
		GenerateID("rp"))
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

func generateAPIKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// --- User functions ---

func GetUserByUsername(username string) (string, string, string, error) {
	var id, displayName, email string
	err := DB.QueryRow(`SELECT id, display_name, COALESCE(email,'') FROM pc_users WHERE username = ? AND status = 'active'`,
		username).Scan(&id, &displayName, &email)
	return id, displayName, email, err
}

func GetUserByID(id string) (string, string, string, error) {
	var username, displayName, email string
	err := DB.QueryRow(`SELECT username, display_name, COALESCE(email,'') FROM pc_users WHERE id = ? AND status = 'active'`,
		id).Scan(&username, &displayName, &email)
	return username, displayName, email, err
}

func SyncUser(id, username, displayName, email, source string) error {
	_, err := DB.Exec(`INSERT INTO pc_users (id, username, display_name, email, source, status, last_sync_at)
		VALUES (?, ?, ?, ?, ?, 'active', NOW())
		ON DUPLICATE KEY UPDATE display_name=VALUES(display_name), email=VALUES(email), last_sync_at=NOW()`,
		id, username, displayName, email, source)
	return err
}

// --- Role functions ---

func IsUserSuperAdmin(userID string) (bool, error) {
	var count int
	err := DB.QueryRow(`SELECT COUNT(*) FROM pc_user_roles ur
		JOIN pc_roles r ON ur.role_id = r.id
		WHERE ur.user_id = ? AND r.code = 'super_admin' AND r.status = 'active'`, userID).Scan(&count)
	return count > 0, err
}

func IsUserAdmin(userID string) (bool, error) {
	var count int
	err := DB.QueryRow(`SELECT COUNT(*) FROM pc_user_roles ur
		JOIN pc_roles r ON ur.role_id = r.id
		WHERE ur.user_id = ? AND r.code IN ('super_admin', 'admin') AND r.status = 'active'`, userID).Scan(&count)
	return count > 0, err
}

func GetUserRoleIDs(userID string) ([]string, error) {
	rows, err := DB.Query(`SELECT role_id FROM pc_user_roles WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids, nil
}

func GetUserRoleCodes(userID string) ([]string, error) {
	rows, err := DB.Query(`SELECT r.code FROM pc_user_roles ur
		JOIN pc_roles r ON ur.role_id = r.id
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

// --- Permission query functions ---

func GetUserPermissions(userID, serviceCode string) (map[string]bool, error) {
	permissions := make(map[string]bool)

	// Check if super_admin (has all permissions)
	isSuperAdmin, _ := IsUserSuperAdmin(userID)
	if isSuperAdmin {
		rows, err := DB.Query(`SELECT code FROM pc_permissions WHERE service_code = ?`, serviceCode)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var code string
			rows.Scan(&code)
			permissions[code] = true
		}
		return permissions, nil
	}

	rows, err := DB.Query(`SELECT DISTINCT p.code FROM pc_permissions p
		JOIN pc_role_permissions rp ON p.id = rp.permission_id
		JOIN pc_user_roles ur ON rp.role_id = ur.role_id
		WHERE ur.user_id = ? AND p.service_code = ?`, userID, serviceCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		rows.Scan(&code)
		permissions[code] = true
	}
	return permissions, nil
}

func GetUserMenus(userID, serviceCode string) ([]map[string]interface{}, error) {
	isSuperAdmin, _ := IsUserSuperAdmin(userID)

	var rows *sql.Rows
	var err error

	if isSuperAdmin {
		rows, err = DB.Query(`SELECT id, code, name, COALESCE(parent_id,''), COALESCE(icon,''), sort_order
			FROM pc_permissions WHERE service_code = ? AND type = 'menu' ORDER BY sort_order`, serviceCode)
	} else {
		rows, err = DB.Query(`SELECT DISTINCT p.id, p.code, p.name, COALESCE(p.parent_id,''), COALESCE(p.icon,''), p.sort_order
			FROM pc_permissions p
			JOIN pc_role_permissions rp ON p.id = rp.permission_id
			JOIN pc_user_roles ur ON rp.role_id = ur.role_id
			WHERE ur.user_id = ? AND p.service_code = ? AND p.type = 'menu'
			ORDER BY p.sort_order`, userID, serviceCode)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var menus []map[string]interface{}
	for rows.Next() {
		var id, code, name, parentID, icon string
		var sortOrder int
		rows.Scan(&id, &code, &name, &parentID, &icon, &sortOrder)
		menus = append(menus, map[string]interface{}{
			"id":         id,
			"code":       code,
			"name":       name,
			"parent_id":  parentID,
			"icon":       icon,
			"sort_order": sortOrder,
		})
	}
	return menus, nil
}

func HasPermission(userID, serviceCode, permissionCode string) (bool, error) {
	isSuperAdmin, _ := IsUserSuperAdmin(userID)
	if isSuperAdmin {
		return true, nil
	}

	var count int
	err := DB.QueryRow(`SELECT COUNT(*) FROM pc_permissions p
		JOIN pc_role_permissions rp ON p.id = rp.permission_id
		JOIN pc_user_roles ur ON rp.role_id = ur.role_id
		WHERE ur.user_id = ? AND p.service_code = ? AND p.code = ?`,
		userID, serviceCode, permissionCode).Scan(&count)
	return count > 0, err
}

// --- Service functions ---

func GetServiceByAPIKey(apiKey string) (string, string, error) {
	var code, name string
	err := DB.QueryRow(`SELECT code, name FROM pc_services WHERE api_key = ? AND status = 'active'`,
		apiKey).Scan(&code, &name)
	return code, name, err
}

func GetServiceByCode(code string) (string, error) {
	var id string
	err := DB.QueryRow(`SELECT id FROM pc_services WHERE code = ? AND status = 'active'`, code).Scan(&id)
	return id, err
}

// --- Audit functions ---

func SaveAuditLog(userID, username, action, targetType, targetID, detail, ip, userAgent, status string) {
	id := GenerateID("audit")
	DB.Exec(`INSERT INTO pc_audit_logs (id, user_id, username, action, target_type, target_id, detail, ip, user_agent, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, id, userID, username, action, targetType, targetID, detail, ip, userAgent, status)
}

// --- Settings functions ---

func GetSetting(key string) (string, error) {
	var value string
	err := DB.QueryRow("SELECT setting_value FROM pc_settings WHERE setting_key = ?", key).Scan(&value)
	return value, err
}

func SetSetting(key, value, description string) error {
	_, err := DB.Exec(`INSERT INTO pc_settings (setting_key, setting_value, description)
		VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value)`, key, value, description)
	return err
}

func GetAllSettings() ([]map[string]interface{}, error) {
	rows, err := DB.Query("SELECT setting_key, setting_value, COALESCE(description,''), updated_at FROM pc_settings ORDER BY setting_key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var settings []map[string]interface{}
	for rows.Next() {
		var key, value, desc string
		var updatedAt time.Time
		rows.Scan(&key, &value, &desc, &updatedAt)
		settings = append(settings, map[string]interface{}{
			"key":         key,
			"value":       value,
			"description": desc,
			"updated_at":  updatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	return settings, nil
}

func IsLocalLoginEnabled() bool {
	// Try Redis cache first
	if val, found := cache.GetLocalLoginEnabled(); found {
		return val
	}
	// Fall back to MySQL
	val, err := GetSetting("local_login_enabled")
	if err != nil {
		return false
	}
	enabled := val == "true"
	cache.CacheLocalLoginEnabled(enabled)
	return enabled
}

// --- Session functions ---

func CreateSession(id, userID, tokenHash, username, ip, userAgent string, expiresAt time.Time) error {
	_, err := DB.Exec(`INSERT INTO pc_sessions (id, user_id, token_hash, ip, user_agent, expires_at)
		VALUES (?, ?, ?, ?, ?, ?)`, id, userID, tokenHash, ip, userAgent, expiresAt)
	if err == nil {
		cache.SetSession(tokenHash, userID, username, time.Until(expiresAt))
	}
	return err
}

func ValidateSession(tokenHash string) (string, string, error) {
	// Try Redis cache first
	if userID, username, found := cache.GetSession(tokenHash); found {
		return userID, username, nil
	}

	// Fall back to MySQL
	var userID, username string
	err := DB.QueryRow(`SELECT s.user_id, u.username FROM pc_sessions s
		JOIN pc_users u ON s.user_id = u.id
		WHERE s.token_hash = ? AND s.expires_at > NOW() AND u.status = 'active'`,
		tokenHash).Scan(&userID, &username)
	if err == nil {
		// Back-fill Redis cache
		cache.SetSession(tokenHash, userID, username, 8*time.Hour)
	}
	return userID, username, err
}

func DeleteSessionByHash(tokenHash string) error {
	cache.DeleteSession(tokenHash)
	_, err := DB.Exec("DELETE FROM pc_sessions WHERE token_hash = ?", tokenHash)
	return err
}

func DeleteUserSessions(userID string) error {
	cache.DeleteUserSessions(userID)
	_, err := DB.Exec("DELETE FROM pc_sessions WHERE user_id = ?", userID)
	return err
}

func CleanExpiredSessions() {
	result, _ := DB.Exec("DELETE FROM pc_sessions WHERE expires_at < NOW()")
	if result != nil {
		n, _ := result.RowsAffected()
		if n > 0 {
			log.Printf("[DB] Cleaned %d expired sessions", n)
		}
	}
}

// --- Password functions ---

func GetUserForLocalLogin(username string) (string, string, string, string, error) {
	var id, displayName, email, passwordHash string
	err := DB.QueryRow(`SELECT id, display_name, COALESCE(email,''), password_hash
		FROM pc_users WHERE username = ? AND source = 'local' AND status = 'active'`,
		username).Scan(&id, &displayName, &email, &passwordHash)
	return id, displayName, email, passwordHash, err
}

func SetUserPassword(userID, passwordHash string) error {
	_, err := DB.Exec("UPDATE pc_users SET password_hash = ? WHERE id = ?", passwordHash, userID)
	return err
}

// VerifyPassword compares a plaintext password with a bcrypt hash
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// HashPassword creates a bcrypt hash
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(hash), err
}

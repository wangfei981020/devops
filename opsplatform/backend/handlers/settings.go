package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"opsplatform/database"
)

// SystemSettings 系统设置结构
type SystemSettings struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// 初始化系统设置表
func init() {
	go func() {
		// 等待数据库连接
		for database.DB == nil {
			continue
		}
		// 创建系统设置表
		database.DB.Exec(`
			CREATE TABLE IF NOT EXISTS system_settings (
				setting_key VARCHAR(100) PRIMARY KEY,
				setting_value TEXT,
				updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
			)
		`)
		// 插入默认 Logo 设置
		database.DB.Exec(`
			INSERT IGNORE INTO system_settings (setting_key, setting_value) VALUES 
			('logo_type', 'text'),
			('logo_text', 'AI-CloudOps'),
			('logo_icon', 'cloud'),
			('logo_color1', '#3b82f6'),
			('logo_color2', '#8b5cf6'),
			('logo_color3', '#06b6d4'),
			('site_title', 'AI-CloudOps 运维平台')
		`)
	}()
}

// HandleGetSettings 获取系统设置
func HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	
	settings := make(map[string]string)
	
	if key != "" {
		// 获取单个设置
		var value string
		err := database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = ?`, key).Scan(&value)
		if err != nil {
			settings[key] = ""
		} else {
			settings[key] = value
		}
	} else {
		// 获取所有设置
		rows, err := database.DB.Query(`SELECT setting_key, setting_value FROM system_settings`)
		if err != nil {
			SafeError(w, "获取设置失败", http.StatusInternalServerError, err)
			return
		}
		defer rows.Close()
		
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err != nil {
				continue
			}
			settings[k] = v
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

// HandleUpdateSettings 更新系统设置
func HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var settings map[string]string
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}
	
	// 获取操作者
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	
	// 批量更新设置
	for key, value := range settings {
		_, err := database.DB.Exec(`
			INSERT INTO system_settings (setting_key, setting_value) VALUES (?, ?)
			ON DUPLICATE KEY UPDATE setting_value = ?
		`, key, value, value)
		if err != nil {
			SafeError(w, "更新设置失败", http.StatusInternalServerError, err)
			return
		}
	}
	
	// 记录审计日志
	changesJSON, _ := json.Marshal(settings)
	AddAuditLogFromRequest(r, "更新系统设置", "settings:logo", operator, "", string(changesJSON), "修改系统设置")
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "设置已保存",
	})
}

// HandleGetSecuritySettings 获取安全设置
func HandleGetSecuritySettings(w http.ResponseWriter, r *http.Request) {
	settings := map[string]interface{}{
		"login_limit_enabled":  false,
		"max_login_attempts":   5,
		"lockout_duration":     30,
		"ip_whitelist_enabled": false,
		"ip_whitelist":         "",
	}
	
	rows, err := database.DB.Query(`SELECT setting_key, setting_value FROM system_settings WHERE setting_key LIKE 'security_%'`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err == nil && v != "" {
				switch k {
				case "security_login_limit_enabled":
					settings["login_limit_enabled"] = v == "true"
				case "security_max_login_attempts":
					settings["max_login_attempts"] = v
				case "security_lockout_duration":
					settings["lockout_duration"] = v
				case "security_ip_whitelist_enabled":
					settings["ip_whitelist_enabled"] = v == "true"
				case "security_ip_whitelist":
					settings["ip_whitelist"] = v
				}
			}
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settings)
}

// HandleUpdateSecuritySettings 更新安全设置
func HandleUpdateSecuritySettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LoginLimitEnabled  bool   `json:"login_limit_enabled"`
		MaxLoginAttempts   int    `json:"max_login_attempts"`
		LockoutDuration    int    `json:"lockout_duration"`
		IPWhitelistEnabled bool   `json:"ip_whitelist_enabled"`
		IPWhitelist        string `json:"ip_whitelist"`
	}
	
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "无效的请求数据", http.StatusBadRequest)
		return
	}
	
	// 保存设置
	settings := map[string]string{
		"security_login_limit_enabled":  boolToStr(req.LoginLimitEnabled),
		"security_max_login_attempts":   intToStr(req.MaxLoginAttempts),
		"security_lockout_duration":     intToStr(req.LockoutDuration),
		"security_ip_whitelist_enabled": boolToStr(req.IPWhitelistEnabled),
		"security_ip_whitelist":         req.IPWhitelist,
	}
	
	for key, value := range settings {
		_, err := database.DB.Exec(`
			INSERT INTO system_settings (setting_key, setting_value) VALUES (?, ?)
			ON DUPLICATE KEY UPDATE setting_value = ?
		`, key, value, value)
		if err != nil {
			SafeError(w, "保存设置失败", http.StatusInternalServerError, err)
			return
		}
	}
	
	// 记录审计日志
	operator := r.Header.Get("X-Operator")
	if operator == "" {
		operator = "system"
	}
	AddAuditLogFromRequest(r, "更新安全设置", "settings:security", operator, "", "", "修改安全设置")
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "安全设置已保存",
	})
}

func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func intToStr(i int) string {
	return fmt.Sprintf("%d", i)
}

// GetSecuritySetting 获取单个安全设置
func GetSecuritySetting(key string) string {
	var value string
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = ?`, "security_"+key).Scan(&value)
	return value
}

// IsIPWhitelisted 检查 IP 是否在白名单中
func IsIPWhitelisted(ip string) bool {
	enabled := GetSecuritySetting("ip_whitelist_enabled")
	if enabled != "true" {
		return true // 未启用白名单，允许所有 IP
	}
	
	whitelist := GetSecuritySetting("ip_whitelist")
	if whitelist == "" {
		return true // 白名单为空，允许所有 IP
	}
	
	// 解析白名单
	ips := strings.Split(whitelist, "\n")
	for _, allowedIP := range ips {
		allowedIP = strings.TrimSpace(allowedIP)
		if allowedIP == "" || strings.HasPrefix(allowedIP, "#") {
			continue
		}
		if allowedIP == ip {
			return true
		}
		// 支持 CIDR 格式（简单匹配）
		if strings.Contains(allowedIP, "/") {
			// 简单的前缀匹配
			prefix := strings.Split(allowedIP, "/")[0]
			prefix = strings.TrimSuffix(prefix, ".0")
			if strings.HasPrefix(ip, prefix) {
				return true
			}
		}
	}
	
	return false
}

// HandleGetLogoConfig 获取 Logo 配置（公开接口，无需认证）
func HandleGetLogoConfig(w http.ResponseWriter, r *http.Request) {
	logoConfig := map[string]string{
		"logo_type":       "text",
		"logo_text":       "AI-CloudOps",
		"logo_icon":       "cloud",
		"logo_color1":     "#3b82f6",
		"logo_color2":     "#8b5cf6",
		"logo_color3":     "#06b6d4",
		"site_title":      "AI-CloudOps 运维平台",
		"session_timeout": "180", // 默认 3 小时
	}
	
	rows, err := database.DB.Query(`SELECT setting_key, setting_value FROM system_settings WHERE setting_key LIKE 'logo_%' OR setting_key = 'site_title' OR setting_key = 'session_timeout'`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err == nil && v != "" {
				logoConfig[k] = v
			}
		}
	}
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(logoConfig)
}

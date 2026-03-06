package handlers

import (
	"encoding/json"
	"net/http"

	"jira-center-backend/database"
	"jira-center-backend/jira"
)

// HandleGetSettings 获取系统设置
func HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings := make(map[string]string)

	rows, err := database.DB.Query(`SELECT setting_key, setting_value FROM system_settings WHERE setting_key LIKE 'jira_%'`)
	if err != nil {
		respondInternalError(w, "获取设置失败")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			// 不返回密码明文
			if k == "jira_password" {
				if v != "" {
					settings[k] = "********"
				} else {
					settings[k] = ""
				}
			} else {
				settings[k] = v
			}
		}
	}

	respondSuccess(w, settings)
}

// HandleUpdateSettings 更新 Jira 连接设置
func HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var settings map[string]string
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	for key, value := range settings {
		// 跳过密码占位符
		if key == "jira_password" && value == "********" {
			continue
		}
		database.DB.Exec(`
			INSERT INTO system_settings (setting_key, setting_value, updated_at)
			VALUES (?, ?, NOW())
			ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value), updated_at = NOW()
		`, key, value)
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "更新Jira配置", "settings", "jira", "修改Jira连接配置", GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "设置已保存"})
}

// HandleTestJiraConnection 测试 Jira 连接
func HandleTestJiraConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL      string `json:"jira_url"`
		Username string `json:"jira_username"`
		Password string `json:"jira_password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	if req.URL == "" {
		respondBadRequest(w, "Jira URL 不能为空")
		return
	}

	// 如果密码是占位符，从数据库读取
	if req.Password == "********" {
		database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'jira_password'`).Scan(&req.Password)
	}

	client := jira.NewClientFromParams(req.URL, req.Username, req.Password)
	userInfo, err := client.TestConnection()
	if err != nil {
		respondError(w, http.StatusBadGateway, "连接失败: "+err.Error())
		return
	}

	respondSuccess(w, map[string]interface{}{
		"message":  "连接成功",
		"user":     userInfo["displayName"],
		"username": userInfo["name"],
	})
}

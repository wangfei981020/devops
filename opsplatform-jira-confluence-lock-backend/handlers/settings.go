package handlers

import (
	"encoding/json"
	"net/http"

	"opsplatform-jira-confluence-lock-backend/confluence"
	"opsplatform-jira-confluence-lock-backend/database"
	"opsplatform-jira-confluence-lock-backend/jira"
)

// 密码类字段列表
var passwordKeys = map[string]bool{
	"confluence_password": true,
	"jira_password":       true,
}

// HandleGetSettings 获取所有配置
func HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings := make(map[string]string)

	rows, err := database.DB.Query(`SELECT setting_key, setting_value FROM system_settings`)
	if err != nil {
		respondInternalError(w, "获取设置失败")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			if passwordKeys[k] {
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

// HandleUpdateSettings 更新配置
func HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var settings map[string]string
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	for key, value := range settings {
		// 跳过密码占位符
		if passwordKeys[key] && value == "********" {
			continue
		}
		database.DB.Exec(`
			INSERT INTO system_settings (setting_key, setting_value, updated_at)
			VALUES (?, ?, NOW())
			ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value), updated_at = NOW()
		`, key, value)
	}

	respondSuccess(w, map[string]string{"message": "设置已保存"})
}

// HandleTestConfluence 测试 Confluence 连接
func HandleTestConfluence(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL      string `json:"confluence_url"`
		Username string `json:"confluence_username"`
		Password string `json:"confluence_password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	if req.URL == "" {
		respondBadRequest(w, "Confluence URL 不能为空")
		return
	}

	if req.Password == "********" {
		database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'confluence_password'`).Scan(&req.Password)
	}

	client := confluence.NewClientFromParams(req.URL, req.Username, req.Password)
	userInfo, err := client.TestConnection()
	if err != nil {
		respondError(w, http.StatusBadGateway, "连接失败: "+err.Error())
		return
	}

	respondSuccess(w, map[string]interface{}{
		"message":     "连接成功",
		"displayName": userInfo["displayName"],
		"username":    userInfo["username"],
	})
}

// HandleTestJira 测试 Jira 连接
func HandleTestJira(w http.ResponseWriter, r *http.Request) {
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
		"message":     "连接成功",
		"displayName": userInfo["displayName"],
		"username":    userInfo["name"],
	})
}

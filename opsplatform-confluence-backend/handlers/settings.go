package handlers

import (
	"encoding/json"
	"net/http"

	"opsplatform-confluence-backend/confluence"
	"opsplatform-confluence-backend/database"
)

// HandleGetPublicSettings 获取公开配置（所有登录用户可访问，不含敏感信息）
func HandleGetPublicSettings(w http.ResponseWriter, r *http.Request) {
	settings := make(map[string]string)

	// 从 system_settings 读取公共配置
	publicKeys := []string{"confluence_allowed_spaces", "confluence_root_page", "confluence_projects", "jira_projects", "jira_fault_issuetype"}
	for _, key := range publicKeys {
		var val string
		if err := database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = ?`, key).Scan(&val); err == nil {
			settings[key] = val
		}
	}

	// 从 service_connections 补充默认连接的配置（如果 system_settings 中没有）
	var confConfig string
	database.DB.QueryRow(
		`SELECT COALESCE(config, '{}') FROM service_connections WHERE type = 'confluence' AND status = 'active' ORDER BY is_default DESC, id ASC LIMIT 1`,
	).Scan(&confConfig)
	if confConfig != "" && confConfig != "{}" {
		var cfg map[string]string
		if json.Unmarshal([]byte(confConfig), &cfg) == nil {
			if settings["confluence_allowed_spaces"] == "" && cfg["space_key"] != "" {
				settings["confluence_allowed_spaces"] = cfg["space_key"]
			}
			if settings["confluence_root_page"] == "" && cfg["root_page"] != "" {
				settings["confluence_root_page"] = cfg["root_page"]
			}
		}
	}

	var jiraConfig string
	database.DB.QueryRow(
		`SELECT COALESCE(config, '{}') FROM service_connections WHERE type = 'jira' AND status = 'active' ORDER BY is_default DESC, id ASC LIMIT 1`,
	).Scan(&jiraConfig)
	if jiraConfig != "" && jiraConfig != "{}" {
		var cfg map[string]string
		if json.Unmarshal([]byte(jiraConfig), &cfg) == nil {
			if settings["jira_projects"] == "" && cfg["projects"] != "" {
				settings["jira_projects"] = cfg["projects"]
			}
			if settings["jira_fault_issuetype"] == "" && cfg["fault_issuetype"] != "" {
				settings["jira_fault_issuetype"] = cfg["fault_issuetype"]
			}
		}
	}

	respondSuccess(w, settings)
}

// HandleGetSettings 获取系统设置
func HandleGetSettings(w http.ResponseWriter, r *http.Request) {
	settings := make(map[string]string)

	rows, err := database.DB.Query(`SELECT setting_key, setting_value FROM system_settings WHERE setting_key LIKE 'confluence_%' OR setting_key LIKE 'jira_%' OR setting_key LIKE 'lark_%'`)
	if err != nil {
		respondInternalError(w, "获取设置失败")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err == nil {
			if k == "confluence_password" || k == "jira_password" || k == "lark_app_secret" {
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

// HandleUpdateSettings 更新 Confluence 连接设置
func HandleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var settings map[string]string
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	for key, value := range settings {
		if (key == "confluence_password" || key == "jira_password" || key == "lark_app_secret") && value == "********" {
			continue
		}
		database.DB.Exec(`
			INSERT INTO system_settings (setting_key, setting_value, updated_at)
			VALUES (?, ?, NOW())
			ON DUPLICATE KEY UPDATE setting_value = VALUES(setting_value), updated_at = NOW()
		`, key, value)
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "更新Confluence配置", "settings", "confluence", "修改Confluence连接配置", GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "设置已保存"})
}

// HandleTestConfluenceConnection 测试 Confluence 连接
func HandleTestConfluenceConnection(w http.ResponseWriter, r *http.Request) {
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
		"message":  "连接成功",
		"user":     userInfo["displayName"],
		"username": userInfo["username"],
	})
}

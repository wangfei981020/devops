package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"opsplatform-confluence-backend/confluence"
	"opsplatform-confluence-backend/database"
	"opsplatform-confluence-backend/jira"

	"github.com/gorilla/mux"
)

// HandleListConnections 获取所有连接
func HandleListConnections(w http.ResponseWriter, r *http.Request) {
	connType := r.URL.Query().Get("type")
	var rows *sql.Rows
	var err error
	if connType != "" {
		rows, err = database.DB.Query(`SELECT id, type, name, url, username, password, config, is_default, status, created_at, updated_at FROM service_connections WHERE type = ? ORDER BY is_default DESC, id ASC`, connType)
	} else {
		rows, err = database.DB.Query(`SELECT id, type, name, url, username, password, config, is_default, status, created_at, updated_at FROM service_connections ORDER BY type, is_default DESC, id ASC`)
	}
	if err != nil {
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	var conns []map[string]interface{}
	for rows.Next() {
		var id int
		var connType, name, url, username, password, config, status, createdAt, updatedAt string
		var isDefault bool
		if err := rows.Scan(&id, &connType, &name, &url, &username, &password, &config, &isDefault, &status, &createdAt, &updatedAt); err != nil {
			continue
		}
		// 密码脱敏
		maskedPwd := ""
		if password != "" {
			maskedPwd = "********"
		}
		conn := map[string]interface{}{
			"id":         id,
			"type":       connType,
			"name":       name,
			"url":        url,
			"username":   username,
			"password":   maskedPwd,
			"config":     json.RawMessage(config),
			"is_default": isDefault,
			"status":     status,
			"created_at": createdAt,
			"updated_at": updatedAt,
		}
		// config 为空时给默认值
		if config == "" || config == "null" {
			conn["config"] = json.RawMessage("{}")
		}
		conns = append(conns, conn)
	}
	if conns == nil {
		conns = []map[string]interface{}{}
	}
	respondSuccess(w, conns)
}

// HandleGetConnection 获取单个连接
func HandleGetConnection(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var connID int
	var connType, name, url, username, password, config, status, createdAt, updatedAt string
	var isDefault bool
	err := database.DB.QueryRow(`SELECT id, type, name, url, username, password, config, is_default, status, created_at, updated_at FROM service_connections WHERE id = ?`, id).
		Scan(&connID, &connType, &name, &url, &username, &password, &config, &isDefault, &status, &createdAt, &updatedAt)
	if err != nil {
		respondError(w, http.StatusNotFound, "连接不存在")
		return
	}
	maskedPwd := ""
	if password != "" {
		maskedPwd = "********"
	}
	conn := map[string]interface{}{
		"id": connID, "type": connType, "name": name, "url": url,
		"username": username, "password": maskedPwd,
		"config": json.RawMessage(config), "is_default": isDefault,
		"status": status, "created_at": createdAt, "updated_at": updatedAt,
	}
	if config == "" || config == "null" {
		conn["config"] = json.RawMessage("{}")
	}
	respondSuccess(w, conn)
}

// HandleCreateConnection 创建连接
func HandleCreateConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type      string          `json:"type"`
		Name      string          `json:"name"`
		URL       string          `json:"url"`
		Username  string          `json:"username"`
		Password  string          `json:"password"`
		Config    json.RawMessage `json:"config"`
		IsDefault bool            `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}
	if req.Type == "" || req.URL == "" {
		respondBadRequest(w, "类型和URL不能为空")
		return
	}
	if req.Name == "" {
		req.Name = req.Type + " - " + req.URL
	}

	configStr := "{}"
	if req.Config != nil {
		configStr = string(req.Config)
	}

	// 如果设为默认，先取消该类型的其他默认
	if req.IsDefault {
		database.DB.Exec(`UPDATE service_connections SET is_default = 0 WHERE type = ?`, req.Type)
	}

	result, err := database.DB.Exec(
		`INSERT INTO service_connections (type, name, url, username, password, config, is_default) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		req.Type, req.Name, req.URL, req.Username, req.Password, configStr, req.IsDefault,
	)
	if err != nil {
		respondInternalError(w, "创建失败: "+err.Error())
		return
	}

	id, _ := result.LastInsertId()
	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, fmt.Sprintf("创建%s连接", req.Type), "connection", fmt.Sprintf("%d", id), req.Name, GetClientIP(r))

	respondSuccess(w, map[string]interface{}{"id": id, "message": "创建成功"})
}

// HandleUpdateConnection 更新连接
func HandleUpdateConnection(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var req struct {
		Name      string          `json:"name"`
		URL       string          `json:"url"`
		Username  string          `json:"username"`
		Password  string          `json:"password"`
		Config    json.RawMessage `json:"config"`
		IsDefault bool            `json:"is_default"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	configStr := "{}"
	if req.Config != nil {
		configStr = string(req.Config)
	}

	// 密码处理：如果是掩码则不更新
	if req.Password == "********" {
		_, err := database.DB.Exec(
			`UPDATE service_connections SET name=?, url=?, username=?, config=?, is_default=?, updated_at=NOW() WHERE id=?`,
			req.Name, req.URL, req.Username, configStr, req.IsDefault, id,
		)
		if err != nil {
			respondInternalError(w, "更新失败")
			return
		}
	} else {
		_, err := database.DB.Exec(
			`UPDATE service_connections SET name=?, url=?, username=?, password=?, config=?, is_default=?, updated_at=NOW() WHERE id=?`,
			req.Name, req.URL, req.Username, req.Password, configStr, req.IsDefault, id,
		)
		if err != nil {
			respondInternalError(w, "更新失败")
			return
		}
	}

	// 如果设为默认，取消同类型的其他默认
	if req.IsDefault {
		var connType string
		database.DB.QueryRow(`SELECT type FROM service_connections WHERE id = ?`, id).Scan(&connType)
		database.DB.Exec(`UPDATE service_connections SET is_default = 0 WHERE type = ? AND id != ?`, connType, id)
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "更新连接配置", "connection", id, req.Name, GetClientIP(r))
	respondSuccess(w, map[string]string{"message": "更新成功"})
}

// HandleDeleteConnection 删除连接
func HandleDeleteConnection(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	var name string
	database.DB.QueryRow(`SELECT name FROM service_connections WHERE id = ?`, id).Scan(&name)

	_, err := database.DB.Exec(`DELETE FROM service_connections WHERE id = ?`, id)
	if err != nil {
		respondInternalError(w, "删除失败")
		return
	}

	_, username, _ := GetUserFromContext(r)
	AddAuditLog("", username, "删除连接", "connection", id, name, GetClientIP(r))
	respondSuccess(w, map[string]string{"message": "已删除"})
}

// HandleTestConnection 测试连接
func HandleTestConnection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       int    `json:"id"`
		Type     string `json:"type"`
		URL      string `json:"url"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	// 如果密码是掩码且有ID，从数据库读取真实密码
	if req.Password == "********" && req.ID > 0 {
		database.DB.QueryRow(`SELECT password FROM service_connections WHERE id = ?`, req.ID).Scan(&req.Password)
	}

	if req.URL == "" {
		respondBadRequest(w, "URL 不能为空")
		return
	}

	switch req.Type {
	case "confluence":
		client := confluence.NewClientFromParams(req.URL, req.Username, req.Password)
		userInfo, err := client.TestConnection()
		if err != nil {
			respondError(w, http.StatusBadGateway, "连接失败: "+err.Error())
			return
		}
		respondSuccess(w, map[string]interface{}{
			"message": "连接成功",
			"user":    userInfo["displayName"],
		})
	case "jira":
		client := jira.NewClientFromParams(req.URL, req.Username, req.Password)
		userInfo, err := client.TestConnection()
		if err != nil {
			respondError(w, http.StatusBadGateway, "连接失败: "+err.Error())
			return
		}
		respondSuccess(w, map[string]interface{}{
			"message": "连接成功",
			"user":    userInfo["displayName"],
		})
	case "grafana":
		baseURL := strings.TrimRight(req.URL, "/")
		apiToken := req.Password
		data, status, err := grafanaRequest(baseURL, apiToken, "/api/org")
		if err != nil {
			respondError(w, http.StatusBadGateway, "连接失败: "+err.Error())
			return
		}
		if status != 200 {
			respondError(w, http.StatusBadGateway, fmt.Sprintf("连接失败，Grafana 返回 %d", status))
			return
		}
		var orgInfo map[string]interface{}
		json.Unmarshal(data, &orgInfo)
		respondSuccess(w, map[string]interface{}{
			"message": "连接成功",
			"user":    orgInfo["name"],
		})
	default:
		respondBadRequest(w, "不支持的连接类型: "+req.Type)
	}
}

// GetDefaultConnection 获取默认连接（供内部使用）
func GetDefaultConnection(connType string) (url, username, password string, config string, err error) {
	err = database.DB.QueryRow(
		`SELECT url, username, password, COALESCE(config, '{}') FROM service_connections WHERE type = ? AND is_default = 1 AND status = 'active' LIMIT 1`,
		connType,
	).Scan(&url, &username, &password, &config)
	if err != nil {
		// Fallback: 取第一个 active 的
		err = database.DB.QueryRow(
			`SELECT url, username, password, COALESCE(config, '{}') FROM service_connections WHERE type = ? AND status = 'active' ORDER BY id ASC LIMIT 1`,
			connType,
		).Scan(&url, &username, &password, &config)
	}
	return
}

// GetConnectionByID 按 ID 获取连接（供内部使用）
func GetConnectionByID(id int) (connType, url, username, password, config string, err error) {
	err = database.DB.QueryRow(
		`SELECT type, url, username, password, COALESCE(config, '{}') FROM service_connections WHERE id = ?`, id,
	).Scan(&connType, &url, &username, &password, &config)
	return
}

// HandleListConnectionsPublic 公开接口 - 只返回连接名称和ID（不含敏感信息）
func HandleListConnectionsPublic(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, type, name, url, config, is_default FROM service_connections WHERE status = 'active' ORDER BY type, is_default DESC, id ASC`)
	if err != nil {
		respondInternalError(w, "查询失败")
		return
	}
	defer rows.Close()

	var conns []map[string]interface{}
	for rows.Next() {
		var id int
		var connType, name, url, config string
		var isDefault bool
		if err := rows.Scan(&id, &connType, &name, &url, &config, &isDefault); err != nil {
			continue
		}
		conn := map[string]interface{}{
			"id":         id,
			"type":       connType,
			"name":       name,
			"url":        url,
			"is_default": isDefault,
			"config":     json.RawMessage(config),
		}
		if config == "" || config == "null" {
			conn["config"] = json.RawMessage("{}")
		}
		conns = append(conns, conn)
	}
	if conns == nil {
		conns = []map[string]interface{}{}
	}
	respondSuccess(w, conns)
}

// MigrateSettingsToConnections 将旧的 system_settings 迁移到 service_connections
func MigrateSettingsToConnections() {
	// 检查是否已有连接记录
	var count int
	database.DB.QueryRow(`SELECT COUNT(*) FROM service_connections`).Scan(&count)
	if count > 0 {
		return // 已有数据，跳过迁移
	}

	// 迁移 Confluence
	var cURL, cUser, cPwd string
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'confluence_url'`).Scan(&cURL)
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'confluence_username'`).Scan(&cUser)
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'confluence_password'`).Scan(&cPwd)

	if cURL != "" {
		var spaceKey, rootPage string
		database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'confluence_allowed_spaces'`).Scan(&spaceKey)
		database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'confluence_root_page'`).Scan(&rootPage)

		configJSON, _ := json.Marshal(map[string]string{"space_key": spaceKey, "root_page": rootPage})
		database.DB.Exec(
			`INSERT INTO service_connections (type, name, url, username, password, config, is_default) VALUES (?, ?, ?, ?, ?, ?, 1)`,
			"confluence", "Confluence", cURL, cUser, cPwd, string(configJSON),
		)
	}

	// 迁移 Jira
	var jURL, jUser, jPwd string
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'jira_url'`).Scan(&jURL)
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'jira_username'`).Scan(&jUser)
	database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'jira_password'`).Scan(&jPwd)

	if jURL != "" {
		var faultType string
		database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'jira_fault_issuetype'`).Scan(&faultType)
		if faultType == "" {
			faultType = "故障"
		}
		configJSON, _ := json.Marshal(map[string]string{"fault_issuetype": faultType})
		database.DB.Exec(
			`INSERT INTO service_connections (type, name, url, username, password, config, is_default) VALUES (?, ?, ?, ?, ?, ?, 1)`,
			"jira", "Jira", jURL, jUser, jPwd, string(configJSON),
		)
	}
}

// UpdateConfluenceClient 根据连接ID创建Confluence客户端
func NewConfluenceClientByConnID(connID string) (*confluence.Client, error) {
	id, err := strconv.Atoi(connID)
	if err != nil || id <= 0 {
		return confluence.NewClient() // fallback to default
	}
	_, url, username, password, _, err := GetConnectionByID(id)
	if err != nil {
		return nil, fmt.Errorf("连接不存在")
	}
	return confluence.NewClientFromParams(url, username, password), nil
}

// NewJiraClientByConnID 根据连接ID创建Jira客户端
func NewJiraClientByConnID(connID string) (*jira.Client, error) {
	id, err := strconv.Atoi(connID)
	if err != nil || id <= 0 {
		return jira.NewClient() // fallback to default
	}
	_, url, username, password, _, err := GetConnectionByID(id)
	if err != nil {
		return nil, fmt.Errorf("连接不存在")
	}
	return &jira.Client{
		BaseURL:  url,
		Username: username,
		Password: password,
		HTTP:     &http.Client{},
	}, nil
}

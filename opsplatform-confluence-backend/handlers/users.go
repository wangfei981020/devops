package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"opsplatform-confluence-backend/database"
)

// HandleListUsers 获取用户列表
func HandleListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, username, display_name, email, role, auth_source, status, created_at FROM users ORDER BY created_at DESC`)
	if err != nil {
		respondInternalError(w, "查询用户失败")
		return
	}
	defer rows.Close()

	var users []map[string]interface{}
	for rows.Next() {
		var id, username, displayName, email, role, authSource, status, createdAt string
		if err := rows.Scan(&id, &username, &displayName, &email, &role, &authSource, &status, &createdAt); err != nil {
			continue
		}
		users = append(users, map[string]interface{}{
			"id":           id,
			"username":     username,
			"display_name": displayName,
			"email":        email,
			"role":         role,
			"auth_source":  authSource,
			"status":       status,
			"created_at":   createdAt,
		})
	}

	respondSuccess(w, users)
}

// HandleCreateUser 创建用户
func HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" || req.Password == "" {
		respondBadRequest(w, "请填写用户名和密码")
		return
	}

	if req.Role == "" {
		req.Role = "user"
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Username
	}

	hashed, err := hashPassword(req.Password)
	if err != nil {
		respondInternalError(w, "密码加密失败")
		return
	}

	id := uuid.New().String()
	_, err = database.DB.Exec(`INSERT INTO users (id, username, password, display_name, role, status, auth_source, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 'active', 'local', NOW(), NOW())`,
		id, req.Username, hashed, req.DisplayName, req.Role)
	if err != nil {
		respondBadRequest(w, "用户名已存在")
		return
	}

	_, operator, _ := GetUserFromContext(r)
	AddAuditLog("", operator, "创建用户", "user", id, "创建用户: "+req.Username, GetClientIP(r))

	respondSuccess(w, map[string]string{"id": id, "message": "用户已创建"})
}

// HandleGetAuditLogs 获取审计日志
func HandleGetAuditLogs(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`SELECT id, username, action, target_type, detail, ip, created_at FROM audit_logs ORDER BY created_at DESC LIMIT 200`)
	if err != nil {
		respondInternalError(w, "查询审计日志失败")
		return
	}
	defer rows.Close()

	var logs []map[string]interface{}
	for rows.Next() {
		var id, username, action, targetType, detail, ip, createdAt string
		if err := rows.Scan(&id, &username, &action, &targetType, &detail, &ip, &createdAt); err != nil {
			continue
		}
		logs = append(logs, map[string]interface{}{
			"id":         id,
			"username":   username,
			"action":     action,
			"target_type": targetType,
			"detail":     detail,
			"ip":         ip,
			"created_at": createdAt,
		})
	}

	respondSuccess(w, logs)
}

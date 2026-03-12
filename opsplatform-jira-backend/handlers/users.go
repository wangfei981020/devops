package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"opsplatform-jira-backend/database"
	"opsplatform-jira-backend/models"
)

// HandleGetUsers 获取用户列表
func HandleGetUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT id, username, display_name, email, role, status, auth_source, created_at
		FROM users ORDER BY created_at DESC
	`)
	if err != nil {
		respondInternalError(w, "获取用户列表失败")
		return
	}
	defer rows.Close()

	users := make([]models.User, 0)
	for rows.Next() {
		var u models.User
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Email, &u.Role, &u.Status, &u.AuthSource, &u.CreatedAt); err == nil {
			users = append(users, u)
		}
	}

	respondSuccess(w, users)
}

// HandleCreateUser 创建用户
func HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Role        string `json:"role"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	if req.Username == "" || req.Password == "" {
		respondBadRequest(w, "用户名和密码不能为空")
		return
	}

	if req.Role == "" {
		req.Role = "user"
	}

	hashed, err := hashPassword(req.Password)
	if err != nil {
		respondInternalError(w, "密码加密失败")
		return
	}

	id := uuid.New().String()
	_, err = database.DB.Exec(`
		INSERT INTO users (id, username, password, display_name, email, role, status, auth_source, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'active', 'local', NOW(), NOW())
	`, id, req.Username, hashed, req.DisplayName, req.Email, req.Role)

	if err != nil {
		respondBadRequest(w, "用户名已存在")
		return
	}

	_, operator, _ := GetUserFromContext(r)
	AddAuditLog("", operator, "创建用户", "user", id, "创建用户: "+req.Username, GetClientIP(r))

	respondSuccess(w, map[string]string{"id": id, "message": "用户已创建"})
}

// HandleUpdateUser 更新用户
func HandleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req struct {
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Role        string `json:"role"`
		Status      string `json:"status"`
		Password    string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	if req.Password != "" {
		hashed, err := hashPassword(req.Password)
		if err != nil {
			respondInternalError(w, "密码加密失败")
			return
		}
		database.DB.Exec(`UPDATE users SET password = ?, updated_at = NOW() WHERE id = ?`, hashed, id)
	}

	_, err := database.DB.Exec(`
		UPDATE users SET display_name = ?, email = ?, role = ?, status = ?, updated_at = NOW()
		WHERE id = ?
	`, req.DisplayName, req.Email, req.Role, req.Status, id)

	if err != nil {
		respondInternalError(w, "更新用户失败")
		return
	}

	_, operator, _ := GetUserFromContext(r)
	AddAuditLog("", operator, "更新用户", "user", id, "更新用户信息", GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "用户已更新"})
}

// HandleDeleteUser 删除用户
func HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var username string
	database.DB.QueryRow("SELECT username FROM users WHERE id = ?", id).Scan(&username)

	_, err := database.DB.Exec("DELETE FROM users WHERE id = ?", id)
	if err != nil {
		respondInternalError(w, "删除用户失败")
		return
	}

	_, operator, _ := GetUserFromContext(r)
	AddAuditLog("", operator, "删除用户", "user", id, "删除用户: "+username, GetClientIP(r))

	respondSuccess(w, map[string]string{"message": "用户已删除"})
}

// HandleGetAuditLogs 获取审计日志
func HandleGetAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit := r.URL.Query().Get("limit")
	if limit == "" {
		limit = "100"
	}

	rows, err := database.DB.Query(`
		SELECT id, user_id, username, action, target_type, target_id, detail, ip, created_at
		FROM audit_logs ORDER BY created_at DESC LIMIT `+limit)
	if err != nil {
		respondInternalError(w, "获取审计日志失败")
		return
	}
	defer rows.Close()

	logs := make([]models.AuditLog, 0)
	for rows.Next() {
		var l models.AuditLog
		if err := rows.Scan(&l.ID, &l.UserID, &l.Username, &l.Action, &l.TargetType, &l.TargetID, &l.Detail, &l.IP, &l.CreatedAt); err == nil {
			logs = append(logs, l)
		}
	}

	respondSuccess(w, logs)
}

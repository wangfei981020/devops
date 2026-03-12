package handlers

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"permission-center-backend/database"
)

// HandleAdminUsers handles user list and creation
// GET/POST /api/admin/users
func HandleAdminUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getUsers(w, r)
	case http.MethodPost:
		createUser(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func getUsers(w http.ResponseWriter, r *http.Request) {
	keyword := r.URL.Query().Get("keyword")
	status := r.URL.Query().Get("status")

	query := `SELECT u.id, u.username, u.display_name, COALESCE(u.email,''), u.source, u.status, u.created_at, u.updated_at,
		COALESCE(GROUP_CONCAT(r.name), '') as role_names
		FROM pc_users u
		LEFT JOIN pc_user_roles ur ON u.id = ur.user_id
		LEFT JOIN pc_roles r ON ur.role_id = r.id`

	var conditions []string
	var args []interface{}

	if keyword != "" {
		conditions = append(conditions, "(u.username LIKE ? OR u.display_name LIKE ? OR u.email LIKE ?)")
		kw := "%" + keyword + "%"
		args = append(args, kw, kw, kw)
	}
	if status != "" {
		conditions = append(conditions, "u.status = ?")
		args = append(args, status)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " GROUP BY u.id ORDER BY u.created_at DESC"

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		log.Printf("[Users] Query failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var users []map[string]interface{}
	for rows.Next() {
		var id, username, displayName, email, source, userStatus, roleNames string
		var createdAt, updatedAt time.Time
		rows.Scan(&id, &username, &displayName, &email, &source, &userStatus, &createdAt, &updatedAt, &roleNames)
		users = append(users, map[string]interface{}{
			"id":           id,
			"username":     username,
			"display_name": displayName,
			"email":        email,
			"source":       source,
			"status":       userStatus,
			"role_names":   roleNames,
			"created_at":   createdAt.Format("2006-01-02 15:04:05"),
			"updated_at":   updatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	if users == nil {
		users = []map[string]interface{}{}
	}

	jsonOK(w, users)
}

func createUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Source      string `json:"source"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	if req.Username == "" {
		jsonError(w, http.StatusBadRequest, "用户名不能为空")
		return
	}

	id := database.GenerateID("user")
	source := req.Source
	if source == "" {
		source = "manual"
	}

	var passwordHash string
	if source == "local" && req.Password != "" {
		hash, err := database.HashPassword(req.Password)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "密码加密失败")
			return
		}
		passwordHash = hash
	}

	_, err := database.DB.Exec(`INSERT INTO pc_users (id, username, display_name, email, source, password_hash, status)
		VALUES (?, ?, ?, ?, ?, ?, 'active')`, id, req.Username, req.DisplayName, req.Email, source, passwordHash)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			jsonError(w, http.StatusBadRequest, "用户名已存在")
			return
		}
		log.Printf("[Users] Create failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "创建失败")
		return
	}

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "user_create", "user", id, `{"username":"`+req.Username+`"}`, GetClientIP(r), r.UserAgent(), "success")

	jsonOK(w, map[string]interface{}{"id": id})
}

// HandleAdminUser handles single user operations
// GET/PUT/DELETE /api/admin/users/{id}
func HandleAdminUser(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	// Check if this is /api/admin/users/{id}/roles
	if len(parts) >= 6 && parts[len(parts)-1] == "roles" {
		userID := parts[len(parts)-2]
		handleUserRoles(w, r, userID)
		return
	}

	// Check if this is /api/admin/users/{id}/reset-password
	if len(parts) >= 6 && parts[len(parts)-1] == "reset-password" {
		userID := parts[len(parts)-2]
		handleResetPassword(w, r, userID)
		return
	}

	userID := parts[len(parts)-1]

	switch r.Method {
	case http.MethodGet:
		getUser(w, r, userID)
	case http.MethodPut:
		updateUser(w, r, userID)
	case http.MethodDelete:
		deleteUser(w, r, userID)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func getUser(w http.ResponseWriter, r *http.Request, userID string) {
	var id, username, displayName, email, source, status string
	var createdAt, updatedAt time.Time
	err := database.DB.QueryRow(`SELECT id, username, display_name, COALESCE(email,''), source, status, created_at, updated_at
		FROM pc_users WHERE id = ?`, userID).Scan(&id, &username, &displayName, &email, &source, &status, &createdAt, &updatedAt)
	if err != nil {
		jsonError(w, http.StatusNotFound, "用户不存在")
		return
	}

	jsonOK(w, map[string]interface{}{
		"id":           id,
		"username":     username,
		"display_name": displayName,
		"email":        email,
		"source":       source,
		"status":       status,
		"created_at":   createdAt.Format("2006-01-02 15:04:05"),
		"updated_at":   updatedAt.Format("2006-01-02 15:04:05"),
	})
}

func updateUser(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		DisplayName string `json:"display_name"`
		Email       string `json:"email"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	_, err := database.DB.Exec(`UPDATE pc_users SET display_name = ?, email = ?, status = ? WHERE id = ?`,
		req.DisplayName, req.Email, req.Status, userID)
	if err != nil {
		log.Printf("[Users] Update failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "更新失败")
		return
	}

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "user_update", "user", userID, "", GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "更新成功")
}

func deleteUser(w http.ResponseWriter, r *http.Request, userID string) {
	// Don't delete admin
	var username string
	database.DB.QueryRow("SELECT username FROM pc_users WHERE id = ?", userID).Scan(&username)
	if username == "admin" {
		jsonError(w, http.StatusForbidden, "不能删除管理员账户")
		return
	}

	// Delete user roles
	database.DB.Exec("DELETE FROM pc_user_roles WHERE user_id = ?", userID)
	// Delete user
	database.DB.Exec("DELETE FROM pc_users WHERE id = ?", userID)

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "user_delete", "user", userID, `{"username":"`+username+`"}`, GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "删除成功")
}

// handleUserRoles handles user role assignment
func handleUserRoles(w http.ResponseWriter, r *http.Request, userID string) {
	switch r.Method {
	case http.MethodGet:
		getUserRoles(w, r, userID)
	case http.MethodPut:
		updateUserRoles(w, r, userID)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func getUserRoles(w http.ResponseWriter, r *http.Request, userID string) {
	rows, err := database.DB.Query(`
		SELECT ur.id, ur.user_id, ur.role_id, r.name as role_name, ur.created_at
		FROM pc_user_roles ur
		JOIN pc_roles r ON ur.role_id = r.id
		WHERE ur.user_id = ?`, userID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var userRoles []map[string]interface{}
	for rows.Next() {
		var id, uid, roleID, roleName string
		var createdAt time.Time
		rows.Scan(&id, &uid, &roleID, &roleName, &createdAt)
		userRoles = append(userRoles, map[string]interface{}{
			"id":        id,
			"user_id":   uid,
			"role_id":   roleID,
			"role_name": roleName,
			"created_at": createdAt.Format("2006-01-02 15:04:05"),
		})
	}
	if userRoles == nil {
		userRoles = []map[string]interface{}{}
	}

	jsonOK(w, userRoles)
}

func updateUserRoles(w http.ResponseWriter, r *http.Request, userID string) {
	var roleIDs []string
	if err := json.NewDecoder(r.Body).Decode(&roleIDs); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	// Delete existing roles
	database.DB.Exec("DELETE FROM pc_user_roles WHERE user_id = ?", userID)

	// Add new roles
	for _, roleID := range roleIDs {
		database.DB.Exec(`INSERT INTO pc_user_roles (id, user_id, role_id) VALUES (?, ?, ?)`,
			database.GenerateID("ur"), userID, roleID)
	}

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "user_roles_update", "user", userID, "", GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "角色更新成功")
}

// handleResetPassword allows admin to reset a user's password
func handleResetPassword(w http.ResponseWriter, r *http.Request, userID string) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.Password == "" || len(req.Password) < 6 {
		jsonError(w, http.StatusBadRequest, "密码不能少于6位")
		return
	}

	hash, err := database.HashPassword(req.Password)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}

	if err := database.SetUserPassword(userID, hash); err != nil {
		jsonError(w, http.StatusInternalServerError, "重置密码失败")
		return
	}

	// Invalidate all sessions for this user
	database.DeleteUserSessions(userID)

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "user_reset_password", "user", userID, "", GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "密码已重置")
}

// HandleAdminUserSearch is used by /api/admin/users/search for user lookup by username
func HandleAdminUserSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	username := r.URL.Query().Get("username")
	if username == "" {
		jsonError(w, http.StatusBadRequest, "username 不能为空")
		return
	}

	var id, displayName, email string
	err := database.DB.QueryRow(`SELECT id, display_name, COALESCE(email,'') FROM pc_users WHERE username = ?`,
		username).Scan(&id, &displayName, &email)
	if err == sql.ErrNoRows {
		jsonError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}

	jsonOK(w, map[string]interface{}{
		"id":           id,
		"username":     username,
		"display_name": displayName,
		"email":        email,
	})
}

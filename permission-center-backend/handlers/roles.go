package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"permission-center-backend/database"
)

// HandleAdminRoles handles role list and creation
// GET/POST /api/admin/roles
func HandleAdminRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getRoles(w, r)
	case http.MethodPost:
		createRole(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func getRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT r.id, r.code, r.name, COALESCE(r.description,''), r.is_system, r.status, r.created_at, r.updated_at,
			(SELECT COUNT(*) FROM pc_user_roles WHERE role_id = r.id) as user_count
		FROM pc_roles r
		ORDER BY r.is_system DESC, r.created_at`)
	if err != nil {
		log.Printf("[Roles] Query failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var roles []map[string]interface{}
	for rows.Next() {
		var id, code, name, description, status string
		var isSystem, userCount int
		var createdAt, updatedAt time.Time
		rows.Scan(&id, &code, &name, &description, &isSystem, &status, &createdAt, &updatedAt, &userCount)
		roles = append(roles, map[string]interface{}{
			"id":          id,
			"code":        code,
			"name":        name,
			"description": description,
			"is_system":   isSystem == 1,
			"status":      status,
			"user_count":  userCount,
			"created_at":  createdAt.Format("2006-01-02 15:04:05"),
			"updated_at":  updatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	if roles == nil {
		roles = []map[string]interface{}{}
	}

	jsonOK(w, roles)
}

func createRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	if req.Code == "" || req.Name == "" {
		jsonError(w, http.StatusBadRequest, "角色代码和名称不能为空")
		return
	}

	id := database.GenerateID("role")
	_, err := database.DB.Exec(`INSERT INTO pc_roles (id, code, name, description, is_system, status)
		VALUES (?, ?, ?, ?, 0, 'active')`, id, req.Code, req.Name, req.Description)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			jsonError(w, http.StatusBadRequest, "角色代码已存在")
			return
		}
		log.Printf("[Roles] Create failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "创建失败")
		return
	}

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "role_create", "role", id, `{"code":"`+req.Code+`","name":"`+req.Name+`"}`, GetClientIP(r), r.UserAgent(), "success")

	jsonOK(w, map[string]interface{}{"id": id})
}

// HandleAdminRole handles single role operations
// GET/PUT/DELETE /api/admin/roles/{id}
func HandleAdminRole(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	// Check if this is /api/admin/roles/{id}/permissions
	if len(parts) >= 6 && parts[len(parts)-1] == "permissions" {
		roleID := parts[len(parts)-2]
		handleRolePermissions(w, r, roleID)
		return
	}

	roleID := parts[len(parts)-1]

	switch r.Method {
	case http.MethodPut:
		updateRole(w, r, roleID)
	case http.MethodDelete:
		deleteRole(w, r, roleID)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func updateRole(w http.ResponseWriter, r *http.Request, roleID string) {
	var isSystem int
	database.DB.QueryRow("SELECT is_system FROM pc_roles WHERE id = ?", roleID).Scan(&isSystem)
	if isSystem == 1 {
		jsonError(w, http.StatusForbidden, "系统内置角色不能修改")
		return
	}

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	_, err := database.DB.Exec(`UPDATE pc_roles SET name = ?, description = ?, status = ? WHERE id = ?`,
		req.Name, req.Description, req.Status, roleID)
	if err != nil {
		log.Printf("[Roles] Update failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "更新失败")
		return
	}

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "role_update", "role", roleID, `{"name":"`+req.Name+`"}`, GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "更新成功")
}

func deleteRole(w http.ResponseWriter, r *http.Request, roleID string) {
	var isSystem int
	database.DB.QueryRow("SELECT is_system FROM pc_roles WHERE id = ?", roleID).Scan(&isSystem)
	if isSystem == 1 {
		jsonError(w, http.StatusForbidden, "系统内置角色不能删除")
		return
	}

	var userCount int
	database.DB.QueryRow("SELECT COUNT(*) FROM pc_user_roles WHERE role_id = ?", roleID).Scan(&userCount)
	if userCount > 0 {
		jsonError(w, http.StatusBadRequest, "该角色下还有用户，请先移除用户")
		return
	}

	database.DB.Exec("DELETE FROM pc_role_permissions WHERE role_id = ?", roleID)
	database.DB.Exec("DELETE FROM pc_roles WHERE id = ?", roleID)

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "role_delete", "role", roleID, "", GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "删除成功")
}

// handleRolePermissions handles role permission assignment
func handleRolePermissions(w http.ResponseWriter, r *http.Request, roleID string) {
	switch r.Method {
	case http.MethodGet:
		getRolePermissions(w, r, roleID)
	case http.MethodPut:
		updateRolePermissions(w, r, roleID)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func getRolePermissions(w http.ResponseWriter, r *http.Request, roleID string) {
	rows, err := database.DB.Query("SELECT permission_id FROM pc_role_permissions WHERE role_id = ?", roleID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	permIDs := make(map[string]bool)
	for rows.Next() {
		var permID string
		rows.Scan(&permID)
		permIDs[permID] = true
	}

	jsonOK(w, permIDs)
}

func updateRolePermissions(w http.ResponseWriter, r *http.Request, roleID string) {
	var code string
	database.DB.QueryRow("SELECT code FROM pc_roles WHERE id = ?", roleID).Scan(&code)
	if code == "super_admin" {
		jsonError(w, http.StatusForbidden, "超级管理员权限不能修改")
		return
	}

	var permissionIDs []string
	if err := json.NewDecoder(r.Body).Decode(&permissionIDs); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	database.DB.Exec("DELETE FROM pc_role_permissions WHERE role_id = ?", roleID)

	for _, permID := range permissionIDs {
		database.DB.Exec(`INSERT INTO pc_role_permissions (id, role_id, permission_id) VALUES (?, ?, ?)`,
			database.GenerateID("rp"), roleID, permID)
	}

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "role_permissions_update", "role", roleID, "", GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "权限更新成功")
}

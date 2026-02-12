package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"opsplatform/database"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ========== 数据结构 ==========

// Role 角色
type Role struct {
	ID          string `json:"id"`
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	IsSystem    bool   `json:"is_system"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	UserCount   int    `json:"user_count,omitempty"`
}

// Permission 权限
type Permission struct {
	ID          string       `json:"id"`
	Code        string       `json:"code"`
	Name        string       `json:"name"`
	Type        string       `json:"type"`
	Resource    string       `json:"resource,omitempty"`
	ParentID    string       `json:"parent_id,omitempty"`
	Icon        string       `json:"icon,omitempty"`
	SortOrder   int          `json:"sort_order"`
	Description string       `json:"description,omitempty"`
	CreatedAt   string       `json:"created_at"`
	Children    []Permission `json:"children,omitempty"`
	Checked     bool         `json:"checked,omitempty"`
}

// UserRole 用户角色关联
type UserRole struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	RoleID    string `json:"role_id"`
	RoleName  string `json:"role_name,omitempty"`
	CreatedAt string `json:"created_at"`
}

// ========== 角色管理 ==========

// HandleRoles 处理角色列表
func HandleRoles(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getRoles(w, r)
	case http.MethodPost:
		createRole(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT r.id, r.code, r.name, r.description, r.is_system, r.status, r.created_at, r.updated_at,
			   (SELECT COUNT(*) FROM user_roles WHERE role_id = r.id) as user_count
		FROM roles r
		ORDER BY r.is_system DESC, r.created_at
	`)
	if err != nil {
		log.Printf("查询角色失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var roles []Role
	for rows.Next() {
		var role Role
		var createdAt, updatedAt time.Time
		var isSystem int
		err := rows.Scan(&role.ID, &role.Code, &role.Name, &role.Description, &isSystem, &role.Status, &createdAt, &updatedAt, &role.UserCount)
		if err != nil {
			log.Printf("扫描角色失败: %v", err)
			continue
		}
		role.IsSystem = isSystem == 1
		role.CreatedAt = createdAt.Format("2006-01-02 15:04:05")
		role.UpdatedAt = updatedAt.Format("2006-01-02 15:04:05")
		roles = append(roles, role)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(roles)
}

func createRole(w http.ResponseWriter, r *http.Request) {
	var role Role
	if err := json.NewDecoder(r.Body).Decode(&role); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if role.Code == "" || role.Name == "" {
		http.Error(w, "角色代码和名称不能为空", http.StatusBadRequest)
		return
	}

	role.ID = uuid.New().String()
	role.Status = "active"

	_, err := database.DB.Exec(`
		INSERT INTO roles (id, code, name, description, is_system, status)
		VALUES (?, ?, ?, ?, 0, ?)
	`, role.ID, role.Code, role.Name, role.Description, role.Status)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			http.Error(w, "角色代码已存在", http.StatusBadRequest)
			return
		}
		log.Printf("创建角色失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	operator := r.Header.Get("X-Operator")
	AddAuditLogFromRequest(r, "role_create", "role:"+role.ID, operator, "", "", `{"code":"`+role.Code+`","name":"`+role.Name+`"}`)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"id":      role.ID,
	})
}

// HandleRole 处理单个角色
func HandleRole(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	roleID := parts[len(parts)-1]

	switch r.Method {
	case http.MethodGet:
		getRole(w, r, roleID)
	case http.MethodPut:
		updateRole(w, r, roleID)
	case http.MethodDelete:
		deleteRole(w, r, roleID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getRole(w http.ResponseWriter, r *http.Request, roleID string) {
	var role Role
	var createdAt, updatedAt time.Time
	var isSystem int
	err := database.DB.QueryRow(`
		SELECT id, code, name, description, is_system, status, created_at, updated_at
		FROM roles WHERE id = ?
	`, roleID).Scan(&role.ID, &role.Code, &role.Name, &role.Description, &isSystem, &role.Status, &createdAt, &updatedAt)
	if err != nil {
		http.Error(w, "角色不存在", http.StatusNotFound)
		return
	}
	role.IsSystem = isSystem == 1
	role.CreatedAt = createdAt.Format("2006-01-02 15:04:05")
	role.UpdatedAt = updatedAt.Format("2006-01-02 15:04:05")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(role)
}

func updateRole(w http.ResponseWriter, r *http.Request, roleID string) {
	// 检查是否是系统角色
	var isSystem int
	database.DB.QueryRow("SELECT is_system FROM roles WHERE id = ?", roleID).Scan(&isSystem)
	if isSystem == 1 {
		http.Error(w, "系统内置角色不能修改", http.StatusForbidden)
		return
	}

	var role Role
	if err := json.NewDecoder(r.Body).Decode(&role); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	_, err := database.DB.Exec(`
		UPDATE roles SET name = ?, description = ?, status = ? WHERE id = ?
	`, role.Name, role.Description, role.Status, roleID)
	if err != nil {
		log.Printf("更新角色失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	operator := r.Header.Get("X-Operator")
	AddAuditLogFromRequest(r, "role_update", "role:"+roleID, operator, "", "", `{"name":"`+role.Name+`"}`)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func deleteRole(w http.ResponseWriter, r *http.Request, roleID string) {
	// 检查是否是系统角色
	var isSystem int
	database.DB.QueryRow("SELECT is_system FROM roles WHERE id = ?", roleID).Scan(&isSystem)
	if isSystem == 1 {
		http.Error(w, "系统内置角色不能删除", http.StatusForbidden)
		return
	}

	// 检查是否有用户使用此角色
	var userCount int
	database.DB.QueryRow("SELECT COUNT(*) FROM user_roles WHERE role_id = ?", roleID).Scan(&userCount)
	if userCount > 0 {
		http.Error(w, "该角色下还有用户，请先移除用户", http.StatusBadRequest)
		return
	}

	// 删除角色权限关联
	database.DB.Exec("DELETE FROM role_permissions WHERE role_id = ?", roleID)
	// 删除角色
	database.DB.Exec("DELETE FROM roles WHERE id = ?", roleID)

	operator := r.Header.Get("X-Operator")
	AddAuditLogFromRequest(r, "role_delete", "role:"+roleID, operator, "", "", "")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// ========== 角色权限管理 ==========

// HandleRolePermissions 处理角色权限
func HandleRolePermissions(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")
	// /api/roles/{id}/permissions
	if len(parts) < 5 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	roleID := parts[len(parts)-2]

	switch r.Method {
	case http.MethodGet:
		getRolePermissions(w, r, roleID)
	case http.MethodPut:
		updateRolePermissions(w, r, roleID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getRolePermissions(w http.ResponseWriter, r *http.Request, roleID string) {
	// 获取角色已分配的权限 ID
	rows, err := database.DB.Query("SELECT permission_id FROM role_permissions WHERE role_id = ?", roleID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	permIDs := make(map[string]bool)
	for rows.Next() {
		var permID string
		rows.Scan(&permID)
		permIDs[permID] = true
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(permIDs)
}

func updateRolePermissions(w http.ResponseWriter, r *http.Request, roleID string) {
	// 检查是否是系统角色（超级管理员不能修改权限）
	var code string
	database.DB.QueryRow("SELECT code FROM roles WHERE id = ?", roleID).Scan(&code)
	if code == "super_admin" {
		http.Error(w, "超级管理员权限不能修改", http.StatusForbidden)
		return
	}

	var permissionIDs []string
	if err := json.NewDecoder(r.Body).Decode(&permissionIDs); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 删除原有权限
	database.DB.Exec("DELETE FROM role_permissions WHERE role_id = ?", roleID)

	// 添加新权限
	for _, permID := range permissionIDs {
		database.DB.Exec(`
			INSERT INTO role_permissions (id, role_id, permission_id) VALUES (?, ?, ?)
		`, uuid.New().String(), roleID, permID)
	}

	operator := r.Header.Get("X-Operator")
	AddAuditLogFromRequest(r, "role_permissions_update", "role:"+roleID, operator, "", "", `{"count":`+string(rune(len(permissionIDs)))+`}`)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// ========== 权限管理 ==========

// HandlePermissions 处理权限列表
func HandlePermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 获取权限类型筛选
	permType := r.URL.Query().Get("type")

	query := `SELECT id, code, name, type, resource, parent_id, icon, sort_order, description, created_at FROM permissions`
	args := []interface{}{}
	if permType != "" {
		query += " WHERE type = ?"
		args = append(args, permType)
	}
	query += " ORDER BY sort_order, created_at"

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		log.Printf("查询权限失败: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var permissions []Permission
	for rows.Next() {
		var perm Permission
		var createdAt time.Time
		err := rows.Scan(&perm.ID, &perm.Code, &perm.Name, &perm.Type, &perm.Resource, &perm.ParentID, &perm.Icon, &perm.SortOrder, &perm.Description, &createdAt)
		if err != nil {
			continue
		}
		perm.CreatedAt = createdAt.Format("2006-01-02 15:04:05")
		permissions = append(permissions, perm)
	}

	// 注意：前端权限列表页面需要平铺的数据，不再构建树形结构
	// 树形结构由前端根据 parent_id 自行处理显示

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(permissions)
}

func buildPermissionTree(permissions []Permission) []Permission {
	permMap := make(map[string]*Permission)
	var roots []*Permission

	// 第一遍：建立映射，初始化 Children
	for i := range permissions {
		permissions[i].Children = []Permission{}
		permMap[permissions[i].ID] = &permissions[i]
	}

	// 第二遍：构建树，把子节点挂到父节点
	for i := range permissions {
		perm := &permissions[i]
		if perm.ParentID == "" {
			roots = append(roots, perm)
		} else if parent, ok := permMap[perm.ParentID]; ok {
			parent.Children = append(parent.Children, *perm)
		} else {
			roots = append(roots, perm)
		}
	}

	// 转换为值切片返回
	result := make([]Permission, len(roots))
	for i, r := range roots {
		result[i] = *r
	}
	return result
}

// HandlePermission 处理单个权限
func HandlePermission(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")
	if len(parts) < 4 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	permID := parts[len(parts)-1]

	switch r.Method {
	case http.MethodPost:
		createPermission(w, r)
	case http.MethodPut:
		updatePermission(w, r, permID)
	case http.MethodDelete:
		deletePermission(w, r, permID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func createPermission(w http.ResponseWriter, r *http.Request) {
	var perm Permission
	if err := json.NewDecoder(r.Body).Decode(&perm); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	perm.ID = uuid.New().String()

	_, err := database.DB.Exec(`
		INSERT INTO permissions (id, code, name, type, resource, parent_id, icon, sort_order, description)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, perm.ID, perm.Code, perm.Name, perm.Type, perm.Resource, perm.ParentID, perm.Icon, perm.SortOrder, perm.Description)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			http.Error(w, "权限代码已存在", http.StatusBadRequest)
			return
		}
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"id":      perm.ID,
	})
}

func updatePermission(w http.ResponseWriter, r *http.Request, permID string) {
	var perm Permission
	if err := json.NewDecoder(r.Body).Decode(&perm); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	_, err := database.DB.Exec(`
		UPDATE permissions SET name = ?, resource = ?, icon = ?, sort_order = ?, description = ? WHERE id = ?
	`, perm.Name, perm.Resource, perm.Icon, perm.SortOrder, perm.Description, permID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

func deletePermission(w http.ResponseWriter, r *http.Request, permID string) {
	// 删除权限关联
	database.DB.Exec("DELETE FROM role_permissions WHERE permission_id = ?", permID)
	// 删除权限
	database.DB.Exec("DELETE FROM permissions WHERE id = ?", permID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// ========== 用户角色管理 ==========

// HandleUserRoles 处理用户角色
func HandleUserRoles(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	parts := strings.Split(path, "/")
	// /api/users/{id}/roles
	if len(parts) < 5 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	userID := parts[len(parts)-2]

	switch r.Method {
	case http.MethodGet:
		getUserRoles(w, r, userID)
	case http.MethodPut:
		updateUserRoles(w, r, userID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func getUserRoles(w http.ResponseWriter, r *http.Request, userID string) {
	rows, err := database.DB.Query(`
		SELECT ur.id, ur.user_id, ur.role_id, r.name as role_name, ur.created_at
		FROM user_roles ur
		JOIN roles r ON ur.role_id = r.id
		WHERE ur.user_id = ?
	`, userID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var userRoles []UserRole
	for rows.Next() {
		var ur UserRole
		var createdAt time.Time
		rows.Scan(&ur.ID, &ur.UserID, &ur.RoleID, &ur.RoleName, &createdAt)
		ur.CreatedAt = createdAt.Format("2006-01-02 15:04:05")
		userRoles = append(userRoles, ur)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userRoles)
}

func updateUserRoles(w http.ResponseWriter, r *http.Request, userID string) {
	var roleIDs []string
	if err := json.NewDecoder(r.Body).Decode(&roleIDs); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 删除原有角色
	database.DB.Exec("DELETE FROM user_roles WHERE user_id = ?", userID)

	// 添加新角色
	for _, roleID := range roleIDs {
		database.DB.Exec(`
			INSERT INTO user_roles (id, user_id, role_id) VALUES (?, ?, ?)
		`, uuid.New().String(), userID, roleID)
	}

	operator := r.Header.Get("X-Operator")
	AddAuditLogFromRequest(r, "user_roles_update", "user:"+userID, operator, "", "", "")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
}

// ========== 获取当前用户权限 ==========

// HandleMyPermissions 获取当前用户的权限
func HandleMyPermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	userID := r.Header.Get("X-Operator")
	if userID == "" {
		userID = "admin"
	}

	// 获取用户的所有角色
	roleRows, err := database.DB.Query("SELECT role_id FROM user_roles WHERE user_id = ?", userID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer roleRows.Close()

	var roleIDs []string
	for roleRows.Next() {
		var roleID string
		roleRows.Scan(&roleID)
		roleIDs = append(roleIDs, roleID)
	}

	// 如果用户没有分配角色，默认给予只读权限
	if len(roleIDs) == 0 {
		roleIDs = append(roleIDs, "role_viewer")
	}

	// 获取所有角色的权限
	permissions := make(map[string]bool)
	for _, roleID := range roleIDs {
		permRows, _ := database.DB.Query(`
			SELECT p.code FROM permissions p
			JOIN role_permissions rp ON p.id = rp.permission_id
			WHERE rp.role_id = ?
		`, roleID)
		if permRows != nil {
			for permRows.Next() {
				var code string
				permRows.Scan(&code)
				permissions[code] = true
			}
			permRows.Close()
		}
	}

	// 获取菜单权限（用于前端渲染菜单）
	var menus []Permission
	menuRows, _ := database.DB.Query(`
		SELECT DISTINCT p.id, p.code, p.name, p.resource, p.parent_id, p.icon, p.sort_order
		FROM permissions p
		JOIN role_permissions rp ON p.id = rp.permission_id
		WHERE rp.role_id IN (SELECT role_id FROM user_roles WHERE user_id = ?)
		AND p.type = 'menu'
		ORDER BY p.sort_order
	`, userID)
	if menuRows != nil {
		for menuRows.Next() {
			var menu Permission
			menuRows.Scan(&menu.ID, &menu.Code, &menu.Name, &menu.Resource, &menu.ParentID, &menu.Icon, &menu.SortOrder)
			menus = append(menus, menu)
		}
		menuRows.Close()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"permissions": permissions,
		"menus":       buildPermissionTree(menus),
		"roles":       roleIDs,
	})
}

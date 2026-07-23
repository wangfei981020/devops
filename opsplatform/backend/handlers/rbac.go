package handlers

import (
	"database/sql"
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
	// Source: manual=管理员手动创建, sso=SSO app_roles 自动创建
	Source string `json:"source"`
	// SSORemovedAt: 该组已在 SSO 侧移除的时间; 记录不删除, 仅作备注
	SSORemovedAt string `json:"sso_removed_at,omitempty"`
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
	// Source: manual=手动分配, sso=SSO app_roles 同步
	Source string `json:"source"`
	// SSORemovedAt: SSO 侧已不再下发该组的时间; 关联不删除, 仅备注
	SSORemovedAt string `json:"sso_removed_at,omitempty"`
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
			   (SELECT COUNT(*) FROM user_roles ur JOIN users u ON ur.user_id = u.id
				WHERE ur.role_id = r.id AND ur.sso_removed_at IS NULL) as user_count,
			   COALESCE(r.source, 'manual') as source
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
		err := rows.Scan(&role.ID, &role.Code, &role.Name, &role.Description, &isSystem, &role.Status, &createdAt, &updatedAt, &role.UserCount, &role.Source)
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

	// 检查是否有用户使用此角色（只算真实在职有效成员，孤儿/软删除不算）
	var userCount int
	database.DB.QueryRow(`SELECT COUNT(*) FROM user_roles ur JOIN users u ON ur.user_id = u.id
		WHERE ur.role_id = ? AND ur.sso_removed_at IS NULL`, roleID).Scan(&userCount)
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

// HandleRoleMembers GET /api/roles/{id}/members —— 列出某角色下的成员（仅在职有效成员，
// 已删用户的孤儿行、SSO 已移除的软删除行都不算，口径与角色列表人数一致）。
func HandleRoleMembers(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/") // /api/roles/{id}/members
	if len(parts) < 5 {
		http.Error(w, "Invalid request", http.StatusBadRequest)
		return
	}
	roleID := parts[len(parts)-2]
	rows, err := database.DB.Query(`
		SELECT u.username, COALESCE(u.display_name, ''), COALESCE(ur.source, 'manual'), ur.created_at
		FROM user_roles ur JOIN users u ON ur.user_id = u.id
		WHERE ur.role_id = ? AND ur.sso_removed_at IS NULL
		ORDER BY ur.created_at`, roleID)
	if err != nil {
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type member struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Source      string `json:"source"`
		JoinedAt    string `json:"joined_at"`
	}
	members := []member{}
	for rows.Next() {
		var m member
		var createdAt time.Time
		if rows.Scan(&m.Username, &m.DisplayName, &m.Source, &createdAt) != nil {
			continue
		}
		m.JoinedAt = createdAt.Format("2006-01-02 15:04:05")
		members = append(members, m)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(members)
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
		var resource, parentID, icon, description sql.NullString
		err := rows.Scan(&perm.ID, &perm.Code, &perm.Name, &perm.Type, &resource, &parentID, &icon, &perm.SortOrder, &description, &createdAt)
		if err != nil {
			log.Printf("扫描权限行失败: %v", err)
			continue
		}
		perm.Resource = resource.String
		perm.ParentID = parentID.String
		perm.Icon = icon.String
		perm.Description = description.String
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
		SELECT ur.id, ur.user_id, ur.role_id, r.name as role_name, ur.created_at,
			   COALESCE(ur.source, 'manual') as source, ur.sso_removed_at
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
		var ssoRemovedAt sql.NullTime
		rows.Scan(&ur.ID, &ur.UserID, &ur.RoleID, &ur.RoleName, &createdAt, &ur.Source, &ssoRemovedAt)
		ur.CreatedAt = createdAt.Format("2006-01-02 15:04:05")
		if ssoRemovedAt.Valid {
			ur.SSORemovedAt = ssoRemovedAt.Time.Format("2006-01-02 15:04:05")
		}
		userRoles = append(userRoles, ur)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(userRoles)
}

// SSORoleItem 查角色接口的单条角色
type SSORoleItem struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	Source       string `json:"source"`                   // manual=手动配置, sso=SSO 同步
	SSORemovedAt string `json:"sso_removed_at,omitempty"` // 非空=该组已在 SSO 侧移除
}

// SSORolesResponse 查角色接口返回体
type SSORolesResponse struct {
	UserID      string        `json:"user_id"`
	Username    string        `json:"username"`
	DisplayName string        `json:"display_name"`
	Email       string        `json:"email"`
	AuthSource  string        `json:"auth_source"` // local / sso
	RoleCount   int           `json:"role_count"`
	Roles       []SSORoleItem `json:"roles"`
	Note        string        `json:"note,omitempty"` // 零角色时的说明
}

// HandleQueryUserRoles 按用户名/邮箱查询用户的全部角色(含来源、SSO 移除状态)
// 常驻只读接口, 供 curl 排查 SSO 角色同步用。走 protected 组, 支持 X-API-Key 或 JWT。
//
//	GET /api/user-roles?username=cesar
//	GET /api/user-roles?email=cesar@solidleisure.com
func HandleQueryUserRoles(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	email := strings.TrimSpace(r.URL.Query().Get("email"))
	if username == "" && email == "" {
		http.Error(w, "请提供 username 或 email 参数", http.StatusBadRequest)
		return
	}

	// 定位用户。username 精确匹配；也支持 email。SSO 用户名可能带 _sso_ 后缀,
	// 所以额外允许「username 是前缀」的模糊匹配,方便直接查 cesar 而不用记全后缀
	var resp SSORolesResponse
	var err error
	if username != "" {
		err = database.DB.QueryRow(`
			SELECT id, username, display_name, COALESCE(email,''), COALESCE(auth_source,'local')
			FROM users WHERE username = ? OR username LIKE CONCAT(?, '\_sso\_%') ORDER BY username LIMIT 1
		`, username, username).Scan(&resp.UserID, &resp.Username, &resp.DisplayName, &resp.Email, &resp.AuthSource)
	} else {
		err = database.DB.QueryRow(`
			SELECT id, username, display_name, COALESCE(email,''), COALESCE(auth_source,'local')
			FROM users WHERE email = ? LIMIT 1
		`, email).Scan(&resp.UserID, &resp.Username, &resp.DisplayName, &resp.Email, &resp.AuthSource)
	}
	if err == sql.ErrNoRows {
		http.Error(w, "未找到该用户", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "查询失败", http.StatusInternalServerError)
		return
	}

	// 查全部角色, 手动的排 SSO 前面
	rows, qErr := database.DB.Query(`
		SELECT r.code, r.name, COALESCE(ur.source,'manual'), ur.sso_removed_at
		FROM user_roles ur JOIN roles r ON ur.role_id = r.id
		WHERE ur.user_id = ?
		ORDER BY (COALESCE(ur.source,'manual') = 'sso'), r.name
	`, resp.UserID)
	if qErr != nil {
		http.Error(w, "查询角色失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	resp.Roles = []SSORoleItem{}
	for rows.Next() {
		var it SSORoleItem
		var removed sql.NullTime
		if err := rows.Scan(&it.Code, &it.Name, &it.Source, &removed); err != nil {
			continue
		}
		if removed.Valid {
			it.SSORemovedAt = removed.Time.Format("2006-01-02 15:04:05")
		}
		resp.Roles = append(resp.Roles, it)
	}
	resp.RoleCount = len(resp.Roles)
	if resp.RoleCount == 0 {
		resp.Note = "该用户未分配任何角色，当前仅有欢迎页权限"
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(resp)
}

func updateUserRoles(w http.ResponseWriter, r *http.Request, userID string) {
	var roleIDs []string
	if err := json.NewDecoder(r.Body).Decode(&roleIDs); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// 删除原有角色
	database.DB.Exec("DELETE FROM user_roles WHERE user_id = ?", userID)

	// 添加新角色。
	// 注意语义：管理员在界面上保存过一次之后，这些关联一律变成 source='manual'，
	// 即「管理员接管」——后续 SSO 同步不再动它们，SSO 移除也不会再打标记。
	// 这是刻意的：手动配置优先级高于 SSO 同步。
	for _, roleID := range roleIDs {
		database.DB.Exec(`
			INSERT INTO user_roles (id, user_id, role_id, source) VALUES (?, ?, ?, 'manual')
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

	username := r.Header.Get("X-Operator")
	log.Printf("[权限调试] X-Operator header (username): '%s'", username)
	if username == "" {
		username = "admin"
		log.Printf("[权限调试] X-Operator为空，使用默认值: %s", username)
	}

	// 通过用户名查找用户 ID
	var userID string
	err := database.DB.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&userID)
	if err != nil {
		log.Printf("[权限调试] 根据用户名 %s 查找用户ID失败: %v", username, err)
		// 如果找不到用户，返回空权限
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"permissions": map[string]bool{},
			"menus":       []Permission{},
			"roles":       []string{},
		})
		return
	}
	log.Printf("[权限调试] 用户名 %s 对应的用户ID: %s", username, userID)

	// 获取用户的所有角色
	roleRows, err := database.DB.Query("SELECT role_id FROM user_roles WHERE user_id = ?", userID)
	if err != nil {
		log.Printf("[权限调试] 查询用户角色失败: %v", err)
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
	log.Printf("[权限调试] 用户 %s 的角色IDs: %v", userID, roleIDs)

	// 安全修复：无角色用户无任何权限（不再默认给予 role_viewer）
	if len(roleIDs) == 0 {
		log.Printf("[权限调试] 用户 %s 没有角色，返回空权限", userID)
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"permissions": []string{},
			"menus":       []Permission{},
		})
		return
	}

	// 获取所有角色的权限
	permissions := make(map[string]bool)
	for _, roleID := range roleIDs {
		permRows, err := database.DB.Query(`
			SELECT p.code FROM permissions p
			JOIN role_permissions rp ON p.id = rp.permission_id
			WHERE rp.role_id = ?
		`, roleID)
		if err != nil {
			log.Printf("[权限调试] 查询角色 %s 的权限失败: %v", roleID, err)
			continue
		}
		count := 0
		for permRows.Next() {
			var code string
			permRows.Scan(&code)
			permissions[code] = true
			count++
		}
		permRows.Close()
		log.Printf("[权限调试] 角色 %s 共有 %d 个权限", roleID, count)
	}
	log.Printf("[权限调试] 用户 %s 最终权限: %v", username, permissions)

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

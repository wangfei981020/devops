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

// HandleAdminPermissions handles permission list and creation
// GET/POST /api/admin/permissions
func HandleAdminPermissions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getPermissions(w, r)
	case http.MethodPost:
		createPermission(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func getPermissions(w http.ResponseWriter, r *http.Request) {
	serviceCode := r.URL.Query().Get("service_code")
	permType := r.URL.Query().Get("type")

	query := `SELECT id, service_code, code, name, type, COALESCE(resource,''), COALESCE(parent_id,''), COALESCE(icon,''), sort_order, COALESCE(description,''), created_at
		FROM pc_permissions`

	var conditions []string
	var args []interface{}

	if serviceCode != "" {
		conditions = append(conditions, "service_code = ?")
		args = append(args, serviceCode)
	}
	if permType != "" {
		conditions = append(conditions, "type = ?")
		args = append(args, permType)
	}
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY sort_order, created_at"

	rows, err := database.DB.Query(query, args...)
	if err != nil {
		log.Printf("[Permissions] Query failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var permissions []map[string]interface{}
	for rows.Next() {
		var id, svcCode, code, name, pType, resource, parentID, icon, description string
		var sortOrder int
		var createdAt time.Time
		rows.Scan(&id, &svcCode, &code, &name, &pType, &resource, &parentID, &icon, &sortOrder, &description, &createdAt)
		permissions = append(permissions, map[string]interface{}{
			"id":           id,
			"service_code": svcCode,
			"code":         code,
			"name":         name,
			"type":         pType,
			"resource":     resource,
			"parent_id":    parentID,
			"icon":         icon,
			"sort_order":   sortOrder,
			"description":  description,
			"created_at":   createdAt.Format("2006-01-02 15:04:05"),
		})
	}
	if permissions == nil {
		permissions = []map[string]interface{}{}
	}

	jsonOK(w, permissions)
}

func createPermission(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ServiceCode string `json:"service_code"`
		Code        string `json:"code"`
		Name        string `json:"name"`
		Type        string `json:"type"`
		Resource    string `json:"resource"`
		ParentID    string `json:"parent_id"`
		Icon        string `json:"icon"`
		SortOrder   int    `json:"sort_order"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	if req.ServiceCode == "" || req.Code == "" || req.Name == "" || req.Type == "" {
		jsonError(w, http.StatusBadRequest, "service_code, code, name, type 不能为空")
		return
	}

	// Validate service exists
	if _, err := database.GetServiceByCode(req.ServiceCode); err == sql.ErrNoRows {
		jsonError(w, http.StatusBadRequest, "服务不存在: "+req.ServiceCode)
		return
	}

	id := database.GenerateID("perm")
	_, err := database.DB.Exec(`INSERT INTO pc_permissions (id, service_code, code, name, type, resource, parent_id, icon, sort_order, description)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, req.ServiceCode, req.Code, req.Name, req.Type, req.Resource, req.ParentID, req.Icon, req.SortOrder, req.Description)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			jsonError(w, http.StatusBadRequest, "该服务下权限代码已存在")
			return
		}
		log.Printf("[Permissions] Create failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "创建失败")
		return
	}

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "permission_create", "permission", id,
		`{"service_code":"`+req.ServiceCode+`","code":"`+req.Code+`"}`, GetClientIP(r), r.UserAgent(), "success")

	jsonOK(w, map[string]interface{}{"id": id})
}

// HandleAdminPermission handles single permission operations
// PUT/DELETE /api/admin/permissions/{id}
func HandleAdminPermission(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	permID := parts[len(parts)-1]

	switch r.Method {
	case http.MethodPut:
		updatePermission(w, r, permID)
	case http.MethodDelete:
		deletePermission(w, r, permID)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func updatePermission(w http.ResponseWriter, r *http.Request, permID string) {
	var req struct {
		Name        string `json:"name"`
		Resource    string `json:"resource"`
		Icon        string `json:"icon"`
		SortOrder   int    `json:"sort_order"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	_, err := database.DB.Exec(`UPDATE pc_permissions SET name = ?, resource = ?, icon = ?, sort_order = ?, description = ? WHERE id = ?`,
		req.Name, req.Resource, req.Icon, req.SortOrder, req.Description, permID)
	if err != nil {
		log.Printf("[Permissions] Update failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "更新失败")
		return
	}

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "permission_update", "permission", permID, "", GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "更新成功")
}

func deletePermission(w http.ResponseWriter, r *http.Request, permID string) {
	// Delete role-permission associations
	database.DB.Exec("DELETE FROM pc_role_permissions WHERE permission_id = ?", permID)
	// Delete child permissions
	database.DB.Exec("DELETE FROM pc_permissions WHERE parent_id = ?", permID)
	// Delete permission
	database.DB.Exec("DELETE FROM pc_permissions WHERE id = ?", permID)

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "permission_delete", "permission", permID, "", GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "删除成功")
}

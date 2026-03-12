package handlers

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"permission-center-backend/database"
)

// HandleAdminServices handles service list and creation
// GET/POST /api/admin/services
func HandleAdminServices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getServices(w, r)
	case http.MethodPost:
		createService(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func getServices(w http.ResponseWriter, r *http.Request) {
	rows, err := database.DB.Query(`
		SELECT s.id, s.code, s.name, COALESCE(s.description,''), COALESCE(s.home_url,''), COALESCE(s.icon,''), s.status, s.api_key, s.created_at, s.updated_at,
			(SELECT COUNT(*) FROM pc_permissions WHERE service_code = s.code) as perm_count
		FROM pc_services s
		ORDER BY s.created_at`)
	if err != nil {
		log.Printf("[Services] Query failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()

	var services []map[string]interface{}
	for rows.Next() {
		var id, code, name, description, homeURL, icon, status, apiKey string
		var permCount int
		var createdAt, updatedAt time.Time
		rows.Scan(&id, &code, &name, &description, &homeURL, &icon, &status, &apiKey, &createdAt, &updatedAt, &permCount)
		services = append(services, map[string]interface{}{
			"id":          id,
			"code":        code,
			"name":        name,
			"description": description,
			"home_url":    homeURL,
			"icon":        icon,
			"status":      status,
			"api_key":     apiKey,
			"perm_count":  permCount,
			"created_at":  createdAt.Format("2006-01-02 15:04:05"),
			"updated_at":  updatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	if services == nil {
		services = []map[string]interface{}{}
	}

	jsonOK(w, services)
}

func createService(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
		HomeURL     string `json:"home_url"`
		Icon        string `json:"icon"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	if req.Code == "" || req.Name == "" {
		jsonError(w, http.StatusBadRequest, "服务代码和名称不能为空")
		return
	}

	id := database.GenerateID("svc")
	apiKey := database.GenerateID("ak") + "-" + database.GenerateID("key")

	_, err := database.DB.Exec(`INSERT INTO pc_services (id, code, name, description, home_url, icon, status, api_key)
		VALUES (?, ?, ?, ?, ?, ?, 'active', ?)`, id, req.Code, req.Name, req.Description, req.HomeURL, req.Icon, apiKey)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate") {
			jsonError(w, http.StatusBadRequest, "服务代码已存在")
			return
		}
		log.Printf("[Services] Create failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "创建失败")
		return
	}

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "service_create", "service", id,
		`{"code":"`+req.Code+`","name":"`+req.Name+`"}`, GetClientIP(r), r.UserAgent(), "success")

	jsonOK(w, map[string]interface{}{"id": id, "api_key": apiKey})
}

// HandleAdminService handles single service operations
// PUT/DELETE /api/admin/services/{id}
func HandleAdminService(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	serviceID := parts[len(parts)-1]

	switch r.Method {
	case http.MethodPut:
		updateService(w, r, serviceID)
	case http.MethodDelete:
		deleteService(w, r, serviceID)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func updateService(w http.ResponseWriter, r *http.Request, serviceID string) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		HomeURL     string `json:"home_url"`
		Icon        string `json:"icon"`
		Status      string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	_, err := database.DB.Exec(`UPDATE pc_services SET name = ?, description = ?, home_url = ?, icon = ?, status = ? WHERE id = ?`,
		req.Name, req.Description, req.HomeURL, req.Icon, req.Status, serviceID)
	if err != nil {
		log.Printf("[Services] Update failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "更新失败")
		return
	}

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "service_update", "service", serviceID, "", GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "更新成功")
}

func deleteService(w http.ResponseWriter, r *http.Request, serviceID string) {
	// Get service code before deleting
	var code string
	database.DB.QueryRow("SELECT code FROM pc_services WHERE id = ?", serviceID).Scan(&code)

	if code == "opsplatform" {
		jsonError(w, http.StatusForbidden, "不能删除默认服务")
		return
	}

	// Delete associated permissions and role-permissions
	database.DB.Exec(`DELETE rp FROM pc_role_permissions rp
		JOIN pc_permissions p ON rp.permission_id = p.id
		WHERE p.service_code = ?`, code)
	database.DB.Exec("DELETE FROM pc_permissions WHERE service_code = ?", code)
	database.DB.Exec("DELETE FROM pc_services WHERE id = ?", serviceID)

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "service_delete", "service", serviceID,
		`{"code":"`+code+`"}`, GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "删除成功")
}

// HandleAdminServiceRegenKey regenerates API key for a service
// POST /api/admin/services/{id}/regen-key
func HandleAdminServiceRegenKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 6 {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	serviceID := parts[len(parts)-2]

	newAPIKey := database.GenerateID("ak") + "-" + database.GenerateID("key")
	_, err := database.DB.Exec(`UPDATE pc_services SET api_key = ? WHERE id = ?`, newAPIKey, serviceID)
	if err != nil {
		log.Printf("[Services] RegenKey failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "重新生成失败")
		return
	}

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "service_regen_key", "service", serviceID, "", GetClientIP(r), r.UserAgent(), "success")

	jsonOK(w, map[string]interface{}{"api_key": newAPIKey})
}

package handlers

import (
	"encoding/json"
	"log"
	"net/http"

	"permission-center-backend/database"
	"permission-center-backend/models"
)

// HandleVerify returns user permissions and menus for a service
// POST /api/verify (API Key auth)
func HandleVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req models.VerifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	if req.UserID == "" || req.ServiceCode == "" {
		jsonError(w, http.StatusBadRequest, "user_id 和 service_code 不能为空")
		return
	}

	// Get user permissions
	permissions, err := database.GetUserPermissions(req.UserID, req.ServiceCode)
	if err != nil {
		log.Printf("[Verify] GetUserPermissions failed: %v", err)
		permissions = make(map[string]bool)
	}

	// Get user menus
	menuList, err := database.GetUserMenus(req.UserID, req.ServiceCode)
	if err != nil {
		log.Printf("[Verify] GetUserMenus failed: %v", err)
		menuList = []map[string]interface{}{}
	}

	// Build menu tree
	menus := buildMenuTree(menuList)

	// Get user roles
	roleIDs, _ := database.GetUserRoleIDs(req.UserID)
	if roleIDs == nil {
		roleIDs = []string{}
	}

	jsonOK(w, models.VerifyResponse{
		Permissions: permissions,
		Menus:       menus,
		Roles:       roleIDs,
	})
}

// HandleCheck checks if a user has a specific permission
// POST /api/check (API Key auth)
func HandleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req models.CheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	if req.UserID == "" || req.ServiceCode == "" || req.PermissionCode == "" {
		jsonError(w, http.StatusBadRequest, "user_id, service_code 和 permission_code 不能为空")
		return
	}

	allowed, err := database.HasPermission(req.UserID, req.ServiceCode, req.PermissionCode)
	if err != nil {
		log.Printf("[Check] HasPermission failed: %v", err)
		allowed = false
	}

	jsonOK(w, models.CheckResponse{Allowed: allowed})
}

// HandleSyncUser syncs user from SSO or other service
// POST /api/sync/user (API Key auth)
func HandleSyncUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req models.SyncUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	if req.UserID == "" || req.Username == "" {
		jsonError(w, http.StatusBadRequest, "user_id 和 username 不能为空")
		return
	}

	source := req.Source
	if source == "" {
		source = "sso"
	}

	if err := database.SyncUser(req.UserID, req.Username, req.DisplayName, req.Email, source); err != nil {
		log.Printf("[SyncUser] Failed: %v", err)
		jsonError(w, http.StatusInternalServerError, "同步用户失败")
		return
	}

	jsonSuccess(w, "用户同步成功")
}

// HandleMyPermissions returns current user's permissions for a service
// GET /api/my/permissions?service_code=xxx (SSO token auth)
func HandleMyPermissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID := GetUserID(r)
	serviceCode := r.URL.Query().Get("service_code")
	if serviceCode == "" {
		serviceCode = "opsplatform"
	}

	permissions, err := database.GetUserPermissions(userID, serviceCode)
	if err != nil {
		log.Printf("[MyPermissions] GetUserPermissions failed: %v", err)
		permissions = make(map[string]bool)
	}

	menuList, err := database.GetUserMenus(userID, serviceCode)
	if err != nil {
		log.Printf("[MyPermissions] GetUserMenus failed: %v", err)
		menuList = []map[string]interface{}{}
	}

	menus := buildMenuTree(menuList)

	roleIDs, _ := database.GetUserRoleIDs(userID)
	if roleIDs == nil {
		roleIDs = []string{}
	}

	jsonOK(w, map[string]interface{}{
		"permissions": permissions,
		"menus":       menus,
		"roles":       roleIDs,
	})
}

// buildMenuTree converts flat menu list to tree structure
func buildMenuTree(menuList []map[string]interface{}) []models.Permission {
	if len(menuList) == 0 {
		return []models.Permission{}
	}

	permMap := make(map[string]*models.Permission)
	var roots []*models.Permission

	// First pass: create all nodes
	for _, m := range menuList {
		perm := &models.Permission{
			ID:        m["id"].(string),
			Code:      m["code"].(string),
			Name:      m["name"].(string),
			ParentID:  m["parent_id"].(string),
			Icon:      m["icon"].(string),
			SortOrder: m["sort_order"].(int),
			Type:      "menu",
			Children:  []models.Permission{},
		}
		permMap[perm.ID] = perm
	}

	// Second pass: build tree
	for _, perm := range permMap {
		if perm.ParentID == "" {
			roots = append(roots, perm)
		} else if parent, ok := permMap[perm.ParentID]; ok {
			parent.Children = append(parent.Children, *perm)
		} else {
			roots = append(roots, perm)
		}
	}

	result := make([]models.Permission, len(roots))
	for i, r := range roots {
		result[i] = *r
	}
	return result
}

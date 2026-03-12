package handlers

import (
	"encoding/json"
	"net/http"

	"permission-center-backend/cache"
	"permission-center-backend/database"
)

// HandleAdminSettings handles settings list and update
// GET/PUT /api/admin/settings
func HandleAdminSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		getSettings(w, r)
	case http.MethodPut:
		updateSetting(w, r)
	default:
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
	}
}

func getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := database.GetAllSettings()
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "查询设置失败")
		return
	}
	if settings == nil {
		settings = []map[string]interface{}{}
	}
	jsonOK(w, settings)
}

func updateSetting(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.Key == "" {
		jsonError(w, http.StatusBadRequest, "设置键不能为空")
		return
	}

	// Whitelist of allowed settings
	allowedKeys := map[string]bool{
		"local_login_enabled": true,
	}
	if !allowedKeys[req.Key] {
		jsonError(w, http.StatusBadRequest, "不支持修改该设置项")
		return
	}

	if err := database.SetSetting(req.Key, req.Value, ""); err != nil {
		jsonError(w, http.StatusInternalServerError, "更新设置失败")
		return
	}

	// Invalidate Redis cache for this setting
	cache.InvalidateSettingCache(req.Key)

	operatorID := GetUserID(r)
	operator := GetUsername(r)
	database.SaveAuditLog(operatorID, operator, "setting_update", "setting", req.Key,
		req.Key+"="+req.Value, GetClientIP(r), r.UserAgent(), "success")

	jsonSuccess(w, "设置已更新")
}

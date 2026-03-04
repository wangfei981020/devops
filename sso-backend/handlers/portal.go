package handlers

import (
	"net/http"

	"sso-backend/database"
)

// HandlePortalServices returns services the current user can access
func HandlePortalServices(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		jsonError(w, http.StatusUnauthorized, "未登录")
		return
	}

	services, err := database.GetUserPortalServices(userID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "获取服务列表失败")
		return
	}

	if services == nil {
		services = []database.PortalServiceRow{}
	}

	jsonOK(w, services)
}

// HandlePortalProfile returns current user profile
func HandlePortalProfile(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		jsonError(w, http.StatusUnauthorized, "未登录")
		return
	}

	user, err := database.GetUserByID(userID)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "获取用户信息失败")
		return
	}

	roles, _ := database.GetUserRoleCodes(user.ID)
	isAdmin, _ := database.IsUserAdmin(user.ID)

	jsonOK(w, map[string]interface{}{
		"id":           user.ID,
		"username":     user.Username,
		"display_name": user.DisplayName,
		"email":        user.Email,
		"phone":        user.Phone,
		"avatar":       user.Avatar,
		"language":     user.Language,
		"roles":        roles,
		"is_admin":     isAdmin,
	})
}

package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"permission-center-backend/database"
)

// HandleLoginSettings returns login configuration (public, no auth)
// GET /api/auth/login-settings
func HandleLoginSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	jsonOK(w, map[string]interface{}{
		"local_login_enabled": database.IsLocalLoginEnabled(),
	})
}

// HandleLogin handles local user login
// POST /api/auth/login
func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	if !database.IsLocalLoginEnabled() {
		jsonError(w, http.StatusForbidden, "本地登录未启用")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求格式")
		return
	}
	if req.Username == "" || req.Password == "" {
		jsonError(w, http.StatusBadRequest, "用户名和密码不能为空")
		return
	}

	ip := GetClientIP(r)

	// Get local user with password hash
	userID, displayName, email, passwordHash, err := database.GetUserForLocalLogin(req.Username)
	if err != nil {
		database.SaveAuditLog("", req.Username, "login_failed", "user", "", "用户不存在或非本地用户", ip, r.UserAgent(), "failure")
		jsonError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if passwordHash == "" {
		jsonError(w, http.StatusUnauthorized, "该用户未设置密码")
		return
	}

	// Verify password
	if !database.VerifyPassword(passwordHash, req.Password) {
		database.SaveAuditLog(userID, req.Username, "login_failed", "user", userID, "密码错误", ip, r.UserAgent(), "failure")
		jsonError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	// Generate session token
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	sessionToken := base64.RawURLEncoding.EncodeToString(tokenBytes)
	tokenHash := database.HashToken(sessionToken)
	sessionID := database.GenerateID("sess")
	expiresAt := time.Now().Add(8 * time.Hour)

	if err := database.CreateSession(sessionID, userID, tokenHash, req.Username, ip, r.UserAgent(), expiresAt); err != nil {
		log.Printf("[Auth] Failed to create session: %v", err)
		jsonError(w, http.StatusInternalServerError, "创建会话失败")
		return
	}

	database.SaveAuditLog(userID, req.Username, "local_login", "user", userID, "本地登录成功", ip, r.UserAgent(), "success")
	log.Printf("[Auth] Local login success: %s (%s)", req.Username, userID)

	jsonOK(w, map[string]interface{}{
		"token": sessionToken,
		"user": map[string]interface{}{
			"id":           userID,
			"username":     req.Username,
			"display_name": displayName,
			"email":        email,
		},
	})
}

// HandleLogout handles logout for local sessions
// POST /api/auth/logout
func HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	token := getBearerToken(r)
	if token != "" {
		tokenHash := database.HashToken(token)
		database.DeleteSessionByHash(tokenHash)
	}

	userID := GetUserID(r)
	username := GetUsername(r)
	if userID != "" {
		database.SaveAuditLog(userID, username, "logout", "user", userID, "登出", GetClientIP(r), r.UserAgent(), "success")
	}

	jsonSuccess(w, "已登出")
}

// HandleChangePassword allows a logged-in local user to change their password
// POST /api/auth/change-password
func HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	userID := GetUserID(r)
	username := GetUsername(r)

	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if req.NewPassword == "" || len(req.NewPassword) < 6 {
		jsonError(w, http.StatusBadRequest, "新密码不能少于6位")
		return
	}

	// Get current password hash
	_, _, _, passwordHash, err := database.GetUserForLocalLogin(username)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "该用户不支持密码修改")
		return
	}

	// Verify old password (if has existing password)
	if passwordHash != "" {
		if !database.VerifyPassword(passwordHash, req.OldPassword) {
			jsonError(w, http.StatusUnauthorized, "原密码错误")
			return
		}
	}

	// Hash new password
	newHash, err := database.HashPassword(req.NewPassword)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "密码加密失败")
		return
	}

	if err := database.SetUserPassword(userID, newHash); err != nil {
		jsonError(w, http.StatusInternalServerError, "修改密码失败")
		return
	}

	database.SaveAuditLog(userID, username, "change_password", "user", userID, "", GetClientIP(r), r.UserAgent(), "success")
	jsonSuccess(w, "密码修改成功")
}

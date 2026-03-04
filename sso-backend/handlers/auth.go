package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"sso-backend/database"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	UserID      string   `json:"user_id"`
	Username    string   `json:"username"`
	DisplayName string   `json:"display_name"`
	Email       string   `json:"email"`
	Roles       []string `json:"roles"`
	IsAdmin     bool     `json:"is_admin"`
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求格式")
		return
	}

	if req.Username == "" || req.Password == "" {
		jsonError(w, http.StatusBadRequest, "用户名和密码不能为空")
		return
	}

	ip := GetClientIP(r)

	// Get user
	user, err := database.GetUserByUsername(req.Username)
	if err != nil {
		database.SaveAuditLog("", req.Username, "login_failed", "user", "", "用户不存在", ip, r.UserAgent(), "failure")
		jsonError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	if user.Status != "active" {
		database.SaveAuditLog(user.ID, user.Username, "login_failed", "user", user.ID, "账户已禁用", ip, r.UserAgent(), "failure")
		jsonError(w, http.StatusForbidden, "账户已被禁用")
		return
	}

	if user.AuthSource != "local" {
		jsonError(w, http.StatusBadRequest, "该用户需要通过SSO外部登录")
		return
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		database.SaveAuditLog(user.ID, user.Username, "login_failed", "user", user.ID, "密码错误", ip, r.UserAgent(), "failure")
		jsonError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	// Create session
	sessionToken := generateSessionToken()
	tokenHash := database.HashToken(sessionToken)
	sessionID := database.GenerateID("sess")
	expiresAt := time.Now().Add(time.Hour)

	if err := database.CreateSession(sessionID, user.ID, tokenHash, ip, r.UserAgent(), expiresAt); err != nil {
		log.Printf("[Auth] Failed to create session: %v", err)
		jsonError(w, http.StatusInternalServerError, "创建会话失败")
		return
	}

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "sso_session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   3600,
	})

	// Update last login
	database.UpdateUserLastLogin(user.ID, ip)

	// Get roles
	roles, _ := database.GetUserRoleCodes(user.ID)
	isAdmin, _ := database.IsUserAdmin(user.ID)

	// Audit log
	database.SaveAuditLog(user.ID, user.Username, "login", "user", user.ID, "登录成功", ip, r.UserAgent(), "success")

	log.Printf("[Auth] Login success: %s (%s)", user.Username, user.ID)

	jsonOK(w, LoginResponse{
		UserID:      user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Roles:       roles,
		IsAdmin:     isAdmin,
	})
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	token := ""
	if cookie, err := r.Cookie("sso_session"); err == nil {
		token = cookie.Value
	}

	if token != "" {
		tokenHash := database.HashToken(token)
		database.DeleteSessionByHash(tokenHash)
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "sso_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})

	userID := GetUserID(r)
	username := GetUsername(r)
	if userID != "" {
		database.SaveAuditLog(userID, username, "logout", "user", userID, "登出", GetClientIP(r), r.UserAgent(), "success")
	}

	jsonSuccess(w, "已登出")
}

func HandleSessionCheck(w http.ResponseWriter, r *http.Request) {
	userID := GetUserID(r)
	if userID == "" {
		jsonError(w, http.StatusUnauthorized, "未登录")
		return
	}

	user, err := database.GetUserByID(userID)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, "用户不存在")
		return
	}

	roles, _ := database.GetUserRoleCodes(user.ID)
	isAdmin, _ := database.IsUserAdmin(user.ID)

	jsonOK(w, LoginResponse{
		UserID:      user.ID,
		Username:    user.Username,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Roles:       roles,
		IsAdmin:     isAdmin,
	})
}

func generateSessionToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

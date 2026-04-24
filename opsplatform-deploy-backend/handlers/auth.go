package handlers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
)

type contextKey string

const (
	ctxUserID      contextKey = "userID"
	ctxUsername    contextKey = "username"
	ctxUserRole    contextKey = "userRole"
	ctxPermissions contextKey = "permissions"
)

type JWTClaims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// POST /api/login
func HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if !DecodeJSON(w, r, &req) {
		return
	}
	var id int
	var username, passwordHash, role, displayName string
	err := database.DB.QueryRow(
		"SELECT id, username, password_hash, role, IFNULL(display_name,'') FROM users WHERE username = ? AND auth_source = 'local' AND status = 1",
		req.Username).Scan(&id, &username, &passwordHash, &role, &displayName)
	if err != nil {
		JSONError(w, 40100, "用户名或密码错误")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		JSONError(w, 40100, "用户名或密码错误")
		return
	}
	token, err := generateToken(id, username, role)
	if err != nil {
		JSONError(w, 50000, "生成令牌失败")
		return
	}
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	expiresAt := time.Now().Add(time.Duration(Cfg.SessionTimeout) * time.Minute)
	database.DB.Exec("INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, ?)", id, tokenHash, expiresAt)

	http.SetCookie(w, &http.Cookie{
		Name:     "deploy_auth_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   Cfg.CookieSecure,
		MaxAge:   Cfg.SessionTimeout * 60,
		SameSite: parseSameSite(Cfg.CookieSameSite),
	})
	JSONSuccess(w, map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":           id,
			"username":     username,
			"display_name": displayName,
			"role":         role,
			"auth_source":  "local",
		},
	})
}

// POST /api/logout
func HandleLogout(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token != "" {
		tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
		database.DB.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	}
	http.SetCookie(w, &http.Cookie{Name: "deploy_auth_token", Value: "", Path: "/", MaxAge: -1})
	JSONSuccess(w, nil)
}

// POST /api/portal-auth  SSO 入口
func HandlePortalAuth(w http.ResponseWriter, r *http.Request) {
	if Cfg.PortalAPIURL == "" {
		JSONError(w, 40001, "Portal 认证未配置")
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		JSONError(w, 40001, "无效的 Token")
		return
	}

	portalToken := req.Token
	if Cfg.AppPortalSecret != "" {
		decrypted, err := aesDecrypt(req.Token, Cfg.AppPortalSecret)
		if err != nil {
			log.Printf("[PortalAuth] 解密失败: %v", err)
			JSONError(w, 40100, "Token 解密失败")
			return
		}
		parts := strings.SplitN(decrypted, "|", 2)
		if len(parts) != 2 {
			JSONError(w, 40100, "Token 格式无效")
			return
		}
		portalToken = parts[0]
		ts, _ := strconv.ParseInt(parts[1], 10, 64)
		if time.Since(time.Unix(ts, 0)) > 30*time.Second {
			JSONError(w, 40100, "Token 已过期，请重新登录")
			return
		}
	}

	client := &http.Client{Timeout: 10 * time.Second}
	portalReq, _ := http.NewRequest("GET", Cfg.PortalAPIURL+"/api/users/me", nil)
	portalReq.Header.Set("Authorization", "Bearer "+portalToken)
	resp, err := client.Do(portalReq)
	if err != nil {
		JSONError(w, 50002, "Portal 验证失败: "+err.Error())
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var username, role, displayName string
	var portalWrapped struct {
		Code int `json:"code"`
		Data struct {
			Username    string `json:"username"`
			DisplayName string `json:"display_name"`
			Role        string `json:"role"`
		} `json:"data"`
	}
	var portalDirect struct {
		Username    string `json:"username"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
	}
	if err := json.Unmarshal(body, &portalWrapped); err == nil && portalWrapped.Data.Username != "" {
		username = portalWrapped.Data.Username
		displayName = portalWrapped.Data.DisplayName
		role = portalWrapped.Data.Role
	} else if err := json.Unmarshal(body, &portalDirect); err == nil && portalDirect.Username != "" {
		username = portalDirect.Username
		displayName = portalDirect.DisplayName
		role = portalDirect.Role
	} else {
		log.Printf("[PortalAuth] 无法解析 Portal 响应: %s", string(body))
		JSONError(w, 40100, "Portal 认证失败")
		return
	}
	if role == "" {
		role = "user"
	}
	if role == "admin" || role == "super_admin" || role == "超级管理员" {
		role = "admin"
	}
	if displayName == "" {
		displayName = username
	}

	var userID int
	err = database.DB.QueryRow("SELECT id FROM users WHERE username = ? AND auth_source = 'portal'", username).Scan(&userID)
	if err != nil {
		res, err := database.DB.Exec(
			"INSERT INTO users (username, display_name, role, auth_source, status) VALUES (?, ?, ?, 'portal', 1)",
			username, displayName, role)
		if err != nil {
			JSONError(w, 50000, "创建用户失败: "+err.Error())
			return
		}
		id, _ := res.LastInsertId()
		userID = int(id)
	} else {
		database.DB.Exec("UPDATE users SET role = ?, display_name = ? WHERE id = ?", role, displayName, userID)
	}
	encToken, _ := crypto.Encrypt(portalToken)
	database.DB.Exec("UPDATE users SET portal_token = ? WHERE id = ?", encToken, userID)

	permissions := fetchPortalPermissions(Cfg.PortalAPIURL, portalToken, username)
	if role != "admin" && permissions != nil && !permissions["menu:deploy_center"] {
		JSONError(w, 40300, "您没有发布中心的访问权限，请联系管理员")
		return
	}
	// 应用级访问控制：调运维平台 external-apps/my，确认用户的角色关联了 deploy_center 应用
	// （截图那个"发布中心-角色权限"对话框对应的数据）。管理员在运维平台 UI 配了哪些角色能访问，
	// 这里强制校验；不通过就拒登录，哪怕 portal_token 本身有效。
	// admin 跳过（本地 admin 账号走 local login 不到这里；如果 portal 的 admin 也能豁免）
	if role != "admin" && !portalUserCanAccessApp(Cfg.PortalAPIURL, portalToken, "deploy_center") {
		JSONError(w, 40300, "您所在的角色未被授予访问发布中心的权限")
		return
	}

	token, _ := generateToken(userID, username, role)
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	expiresAt := time.Now().Add(time.Duration(Cfg.SessionTimeout) * time.Minute)
	// 缓存权限到 session，AuthMiddleware 时能直接拿，不用每次回头查 portal
	permJSON, _ := json.Marshal(permissions)
	database.DB.Exec(
		"INSERT INTO sessions (user_id, token_hash, expires_at, permissions) VALUES (?, ?, ?, ?)",
		userID, tokenHash, expiresAt, string(permJSON))

	http.SetCookie(w, &http.Cookie{
		Name:     "deploy_auth_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   Cfg.CookieSecure,
		MaxAge:   Cfg.SessionTimeout * 60,
		SameSite: parseSameSite(Cfg.CookieSameSite),
	})
	JSONSuccess(w, map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":           userID,
			"username":     username,
			"display_name": displayName,
			"role":         role,
			"auth_source":  "portal",
		},
		"permissions": permissions,
	})
}

// GET /api/users/me
func HandleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(ctxUserID).(int)
	username := r.Context().Value(ctxUsername).(string)
	role := r.Context().Value(ctxUserRole).(string)

	var displayName, authSource string
	database.DB.QueryRow("SELECT IFNULL(display_name,''), IFNULL(auth_source,'local') FROM users WHERE id = ?", userID).
		Scan(&displayName, &authSource)

	JSONSuccess(w, map[string]interface{}{
		"id":           userID,
		"username":     username,
		"display_name": displayName,
		"role":         role,
		"auth_source":  authSource,
	})
}

// GET /api/refresh-permissions
func HandleRefreshPermissions(w http.ResponseWriter, r *http.Request) {
	if Cfg.PortalAPIURL == "" {
		JSONSuccess(w, map[string]interface{}{"permissions": map[string]bool{}})
		return
	}
	userID := r.Context().Value(ctxUserID).(int)
	username := r.Context().Value(ctxUsername).(string)
	role := r.Context().Value(ctxUserRole).(string)

	var authSource string
	database.DB.QueryRow("SELECT IFNULL(auth_source,'local') FROM users WHERE id = ?", userID).Scan(&authSource)

	if authSource != "portal" {
		JSONSuccess(w, map[string]interface{}{"permissions": map[string]bool{}, "role": role, "auth_source": authSource})
		return
	}
	permissions := fetchPortalPermissionsInternal(Cfg.PortalAPIURL, username)
	JSONSuccess(w, map[string]interface{}{
		"permissions": permissions,
		"role":        role,
		"auth_source": authSource,
	})
}

// portalUserCanAccessApp 调运维平台 /api/external-apps/my 看用户的角色是否授权了指定 app_key。
//
//	portalToken 代表用户身份；运维平台那边按 token 解析用户 → 查 user_roles → role_external_apps。
//	运维平台 UI「发布中心-角色权限」对话框勾的就是这张表。
//	没关联 → 返回 false → 发布中心 portal-auth 直接拒绝登录。
func portalUserCanAccessApp(portalURL, portalToken, appKey string) bool {
	u := strings.TrimRight(portalURL, "/") + "/api/external-apps/my"
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+portalToken)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[AppAccess] fetch external-apps/my failed: %v", err)
		// 运维平台临时不可用时不拦截，避免 portal 挂了连带发布中心也登不了
		return true
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	// 兼容 {code,data:[...]} 和 [...] 两种格式
	var wrapped struct {
		Data []struct {
			AppKey string `json:"app_key"`
		} `json:"data"`
	}
	var direct []struct {
		AppKey string `json:"app_key"`
	}
	list := wrapped.Data
	if err := json.Unmarshal(raw, &wrapped); err != nil || len(wrapped.Data) == 0 {
		if err2 := json.Unmarshal(raw, &direct); err2 == nil {
			list = direct
		}
	}
	for _, a := range list {
		if a.AppKey == appKey {
			return true
		}
	}
	return false
}

// fetchPortalPermissions: 用用户 token 调 portal 拉权限
func fetchPortalPermissions(portalURL, portalToken, username string) map[string]bool {
	permURL := strings.TrimRight(portalURL, "/") + "/api/my/permissions"
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", permURL, nil)
	req.Header.Set("Authorization", "Bearer "+portalToken)
	req.Header.Set("X-Operator", username)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[PortalAuth] 获取权限失败: %v", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("[PortalAuth] 获取权限 HTTP %d", resp.StatusCode)
		return nil
	}
	return parseDeployCenterPerms(resp.Body)
}

// fetchPortalPermissionsInternal: 内网服务间调用
func fetchPortalPermissionsInternal(portalURL, username string) map[string]bool {
	permURL := strings.TrimRight(portalURL, "/") + "/my/permissions"
	client := &http.Client{Timeout: 5 * time.Second}
	req, _ := http.NewRequest("GET", permURL, nil)
	req.Header.Set("X-Operator", username)
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[RefreshPerms] %v", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	return parseDeployCenterPerms(resp.Body)
}

func parseDeployCenterPerms(body io.Reader) map[string]bool {
	var result struct {
		Permissions map[string]bool `json:"permissions"`
	}
	raw, _ := io.ReadAll(body)
	// 兼容 {code,data:{permissions}} 和 {permissions} 两种包装
	var wrapped struct {
		Data struct {
			Permissions map[string]bool `json:"permissions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Data.Permissions != nil {
		result.Permissions = wrapped.Data.Permissions
	} else {
		_ = json.Unmarshal(raw, &result)
	}

	out := make(map[string]bool)
	for code, granted := range result.Permissions {
		if !granted {
			continue
		}
		if code == "menu:deploy_center" ||
			strings.HasPrefix(code, "menu:deploy_center_") ||
			strings.HasPrefix(code, "deploy_center:") {
			out[code] = true
		}
	}
	return out
}

// AuthMiddleware: 校验 JWT + session
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			JSONError(w, 40100, "未授权")
			return
		}
		claims, err := parseToken(token)
		if err != nil {
			JSONError(w, 40100, "令牌无效或已过期")
			return
		}
		tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
		var permJSON sql.NullString
		err = database.DB.QueryRow(
			"SELECT permissions FROM sessions WHERE token_hash = ? AND expires_at > NOW()",
			tokenHash).Scan(&permJSON)
		if err != nil {
			JSONError(w, 40100, "会话已过期")
			return
		}
		perms := map[string]bool{}
		if permJSON.Valid && permJSON.String != "" {
			_ = json.Unmarshal([]byte(permJSON.String), &perms)
		}
		ctx := context.WithValue(r.Context(), ctxUserID, claims.UserID)
		ctx = context.WithValue(ctx, ctxUsername, claims.Username)
		ctx = context.WithValue(ctx, ctxUserRole, claims.Role)
		ctx = context.WithValue(ctx, ctxPermissions, perms)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AdminMiddleware: 仅限 role=admin
func AdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role, _ := r.Context().Value(ctxUserRole).(string)
		if role != "admin" {
			JSONError(w, 40300, "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// UsernameFromCtx 从 request context 取当前登录用户名（给 deploy operator 用）
func UsernameFromCtx(r *http.Request) string {
	if v, ok := r.Context().Value(ctxUsername).(string); ok && v != "" {
		return v
	}
	return ""
}

// RoleFromCtx 从 request context 取当前用户角色
func RoleFromCtx(r *http.Request) string {
	if v, ok := r.Context().Value(ctxUserRole).(string); ok {
		return v
	}
	return ""
}

// HasButton 判断当前用户是否拥有某个按钮权限。
//
//	admin 无条件放行；其他用户看 session.permissions 缓存（portal-auth 时从运维平台拉好的）。
//	code 约定用短码，如 "submit_uat"、"manage_projects"；会自动补全前缀 "deploy_center:"。
func HasButton(r *http.Request, code string) bool {
	if RoleFromCtx(r) == "admin" {
		return true
	}
	perms, _ := r.Context().Value(ctxPermissions).(map[string]bool)
	if perms == nil {
		return false
	}
	return perms["deploy_center:"+code]
}

// RequireButton 中间件：admin 或 deploy_center:<code> 为 true 才放行。
//
//	用在 admin/子 router 替代 AdminMiddleware。新的检查语义是
//	"admin OR 该按钮权限"，portal 用户也能通过单个按钮勾选获得权限。
func RequireButton(code string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasButton(r, code) {
				JSONError(w, 40300, "需要「"+code+"」权限")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// IsAdmin 判断当前请求是否来自 admin 角色
func IsAdmin(r *http.Request) bool {
	return RoleFromCtx(r) == "admin"
}

func generateToken(userID int, username, role string) (string, error) {
	claims := JWTClaims{
		UserID: userID, Username: username, Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(Cfg.SessionTimeout) * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(Cfg.JWTSecret))
}

func parseToken(tokenStr string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &JWTClaims{}, func(t *jwt.Token) (interface{}, error) {
		return []byte(Cfg.JWTSecret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}
	return claims, nil
}

// extractToken 仅认 Authorization: Bearer <token>
// 不接受 cookie 和 ?token= query 参数（避免 token 出现在日志、Referer、history）
func extractToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func parseSameSite(s string) http.SameSite {
	switch strings.ToLower(s) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}

// 定期清理过期 session
func StartSessionCleaner() {
	go func() {
		for {
			time.Sleep(30 * time.Minute)
			if database.DB == nil {
				continue
			}
			if res, err := database.DB.Exec("DELETE FROM sessions WHERE expires_at < NOW()"); err == nil {
				if n, _ := res.RowsAffected(); n > 0 {
					log.Printf("[Auth] 清理过期会话 %d 条", n)
				}
			}
		}
	}()
}

func aesDecrypt(encrypted, secret string) (string, error) {
	key := sha256.Sum256([]byte(secret))
	data, err := base64.URLEncoding.DecodeString(encrypted)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := data[:nonceSize], data[nonceSize:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

var _ = sql.ErrNoRows // 防止未使用

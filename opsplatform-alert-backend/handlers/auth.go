package handlers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
	"opsplatform-alert-backend/config"
	"opsplatform-alert-backend/database"
)

var cfg *config.Config

func SetConfig(c *config.Config) {
	cfg = c
}

type contextKey string

const (
	contextUserID   contextKey = "userID"
	contextUsername  contextKey = "username"
	contextUserRole contextKey = "userRole"
)

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type JWTClaims struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}

	var id int
	var username, passwordHash, role string
	err := database.DB.QueryRow("SELECT id, username, password_hash, role FROM users WHERE username = ? AND auth_source = 'local' AND status = 1",
		req.Username).Scan(&id, &username, &passwordHash, &role)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		jsonError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	token, err := generateToken(id, username, role)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "生成令牌失败")
		return
	}

	// Save session
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	expiresAt := time.Now().Add(time.Duration(cfg.SessionTimeout) * time.Minute)
	database.DB.Exec("INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, ?)",
		id, tokenHash, expiresAt)

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "alert_auth_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		MaxAge:   cfg.SessionTimeout * 60,
		SameSite: parseSameSite(cfg.CookieSameSite),
	})

	SaveAuditLog(r, "login", "user", username, "本地登录")
	jsonSuccess(w, map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":       id,
			"username": username,
			"role":     role,
		},
	})
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token != "" {
		tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
		database.DB.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "alert_auth_token",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	jsonSuccess(w, nil)
}

// HandlePortalAuth handles SSO auth from ops platform
func HandlePortalAuth(w http.ResponseWriter, r *http.Request) {
	if cfg.PortalAPIURL == "" {
		jsonError(w, http.StatusBadRequest, "Portal认证未配置")
		return
	}

	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		jsonError(w, http.StatusBadRequest, "无效的Token")
		return
	}

	// Try to decrypt token if APP_PORTAL_SECRET is set
	portalToken := req.Token
	secret := os.Getenv("APP_PORTAL_SECRET")
	if secret != "" {
		decrypted, err := aesDecrypt(req.Token, secret)
		if err != nil {
			log.Printf("[PortalAuth] 解密失败: %v", err)
			jsonError(w, http.StatusUnauthorized, "Token解密失败")
			return
		}
		// Parse: token|timestamp
		parts := strings.SplitN(decrypted, "|", 2)
		if len(parts) != 2 {
			jsonError(w, http.StatusUnauthorized, "Token格式无效")
			return
		}
		portalToken = parts[0]
		ts, _ := strconv.ParseInt(parts[1], 10, 64)
		if time.Since(time.Unix(ts, 0)) > 30*time.Second {
			log.Printf("[PortalAuth] Token已过期: %ds ago", time.Now().Unix()-ts)
			jsonError(w, http.StatusUnauthorized, "Token已过期，请重新登录")
			return
		}
		log.Printf("[PortalAuth] Token解密成功，时效OK")
	}

	// Verify with portal
	client := &http.Client{Timeout: 10 * time.Second}
	portalReq, _ := http.NewRequest("GET", cfg.PortalAPIURL+"/api/users/me", nil)
	portalReq.Header.Set("Authorization", "Bearer "+portalToken)
	resp, err := client.Do(portalReq)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "Portal验证失败")
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Parse portal response - supports both {code,data} wrapper and direct user object
	var username, role string
	var portalWrapped struct {
		Code int `json:"code"`
		Data struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"data"`
	}
	var portalDirect struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}

	if err := json.Unmarshal(body, &portalWrapped); err == nil && portalWrapped.Data.Username != "" {
		username = portalWrapped.Data.Username
		role = portalWrapped.Data.Role
	} else if err := json.Unmarshal(body, &portalDirect); err == nil && portalDirect.Username != "" {
		username = portalDirect.Username
		role = portalDirect.Role
	} else {
		log.Printf("[PortalAuth] 无法解析Portal响应: %s", string(body))
		jsonError(w, http.StatusUnauthorized, "Portal认证失败")
		return
	}

	log.Printf("[PortalAuth] 用户: %s, 角色: %s", username, role)
	if role == "" {
		role = "user"
	}

	// Normalize role
	if role == "admin" || role == "super_admin" || role == "超级管理员" {
		role = "admin"
	}

	// Find or create portal user (separate from local users)
	var userID int
	err = database.DB.QueryRow("SELECT id FROM users WHERE username = ? AND auth_source = 'portal'", username).Scan(&userID)
	if err != nil {
		// Create new portal user
		result, err := database.DB.Exec("INSERT INTO users (username, display_name, role, auth_source, status) VALUES (?, ?, ?, 'portal', 1)",
			username, username, role)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "创建用户失败")
			return
		}
		id, _ := result.LastInsertId()
		userID = int(id)
	} else {
		// Update existing portal user's role
		database.DB.Exec("UPDATE users SET role = ? WHERE id = ?", role, userID)
	}

	// Save portal token for permission refresh
	database.DB.Exec("UPDATE users SET portal_token = ? WHERE id = ?", portalToken, userID)

	// Fetch permissions from portal
	permissions := fetchPortalPermissions(cfg.PortalAPIURL, portalToken, username)

	// Check if user has access to alert platform (admin bypass)
	if role != "admin" && permissions != nil && !permissions["menu:alert"] {
		log.Printf("[PortalAuth] 用户 %s 无告警平台访问权限", username)
		jsonError(w, http.StatusForbidden, "您没有告警平台的访问权限，请联系管理员")
		return
	}

	token, _ := generateToken(userID, username, role)

	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	expiresAt := time.Now().Add(time.Duration(cfg.SessionTimeout) * time.Minute)
	database.DB.Exec("INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, ?)",
		userID, tokenHash, expiresAt)

	http.SetCookie(w, &http.Cookie{
		Name:     "alert_auth_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		MaxAge:   cfg.SessionTimeout * 60,
		SameSite: parseSameSite(cfg.CookieSameSite),
	})

	SaveAuditLog(r, "portal_login", "user", username, "SSO登录")
	jsonSuccess(w, map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":          userID,
			"username":    username,
			"role":        role,
			"auth_source": "portal",
		},
		"permissions": permissions,
	})
}

// fetchPortalPermissions fetches alert-related permissions from portal
func fetchPortalPermissions(portalURL, portalToken, username string) map[string]bool {
	permURL := strings.TrimRight(portalURL, "/") + "/api/my/permissions"
	client := &http.Client{Timeout: 5 * time.Second}
	permReq, _ := http.NewRequest("GET", permURL, nil)
	permReq.Header.Set("Authorization", "Bearer "+portalToken)
	permReq.Header.Set("X-Operator", username)

	resp, err := client.Do(permReq)
	if err != nil {
		log.Printf("[PortalAuth] 获取权限失败: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[PortalAuth] 获取权限返回 HTTP %d", resp.StatusCode)
		return nil
	}

	var result struct {
		Permissions map[string]bool `json:"permissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[PortalAuth] 解析权限失败: %v", err)
		return nil
	}

	// Filter alert-related permissions (menu:alert, menu:alert_* and alert:*)
	alertPerms := make(map[string]bool)
	for code, granted := range result.Permissions {
		if granted && (code == "menu:alert" || strings.HasPrefix(code, "menu:alert_") || strings.HasPrefix(code, "alert:")) {
			alertPerms[code] = true
		}
	}
	log.Printf("[PortalAuth] Alert permissions for %s: %v", username, alertPerms)
	return alertPerms
}

func HandleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(contextUserID).(int)
	username := r.Context().Value(contextUsername).(string)
	role := r.Context().Value(contextUserRole).(string)

	jsonSuccess(w, map[string]interface{}{
		"id":       userID,
		"username": username,
		"role":     role,
	})
}

// HandleRefreshPermissions re-fetches permissions from portal for the current user
func HandleRefreshPermissions(w http.ResponseWriter, r *http.Request) {
	if cfg.PortalAPIURL == "" {
		jsonSuccess(w, map[string]interface{}{"permissions": map[string]bool{}})
		return
	}

	userID := r.Context().Value(contextUserID).(int)
	username := r.Context().Value(contextUsername).(string)
	role := r.Context().Value(contextUserRole).(string)

	var authSource string
	database.DB.QueryRow("SELECT COALESCE(auth_source,'local') FROM users WHERE id = ?",
		userID).Scan(&authSource)

	if authSource != "portal" {
		jsonSuccess(w, map[string]interface{}{"permissions": map[string]bool{}, "role": role, "auth_source": authSource})
		return
	}

	// 用该用户存档的 portal token 调运维平台。
	//
	//	原先这里走的是 fetchPortalPermissionsInternal → {portal}/my/permissions，
	//	注释写着"依赖内网信任"，但运维平台**根本没有这条路由**（它挂在 /api 下，
	//	实测 404）；而 /api/my/permissions 认的是用户身份，光带 X-Operator 头是 401。
	//	两条路都不通，于是这个函数一直返回 nil，前端 `if (data?.permissions)`
	//	见到 null 就跳过更新——刷新权限静默无效，管理员撤了权限用户毫无感知。
	//	（DEPLOY-002 同源缺陷）
	permissions := fetchPortalPermissionsForUser(cfg.PortalAPIURL, userID, username)
	if permissions == nil {
		// 拉不到时**不下发 permissions 字段**，让前端保持现有权限不动，
		// 只用 stale 标记告诉它"这次没刷成"。
		// 下发空 map 是更糟的选择：前端 `if (data?.permissions)` 对 {} 为真，
		// 会把用户权限清空——把一次网络抖动变成一次误降权。
		log.Printf("[RefreshPerms] %s 拉取失败，前端将保持现有权限", username)
		jsonSuccess(w, map[string]interface{}{
			"stale":       true,
			"role":        role,
			"auth_source": authSource,
		})
		return
	}

	jsonSuccess(w, map[string]interface{}{
		"permissions": permissions,
		"role":        role,
		"auth_source": authSource,
	})
}

// fetchPortalPermissionsForUser 用该用户存档的 portal token 拉最新权限。
//
//	token 在 portal-auth 成功时就写进 users.portal_token 了，只是从没被读过。
//	⚠️ 它目前是**明文**存的（发布中心是加密存的），另行记录待修。
func fetchPortalPermissionsForUser(portalURL string, userID int, username string) map[string]bool {
	var tok string
	if err := database.DB.QueryRow(`SELECT COALESCE(portal_token,'') FROM users WHERE id = ?`, userID).Scan(&tok); err != nil {
		log.Printf("[RefreshPerms] 读 portal_token 失败 user=%s: %v", username, err)
		return nil
	}
	if tok == "" {
		log.Printf("[RefreshPerms] user=%s 没有存档的 portal token，需重新登录一次", username)
		return nil
	}
	return fetchPortalPermissions(portalURL, tok, username)
}


// AuthMiddleware validates JWT tokens
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			jsonError(w, http.StatusUnauthorized, "未授权")
			return
		}

		claims, err := parseToken(token)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "令牌无效或已过期")
			return
		}

		// Check session exists
		tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
		var count int
		database.DB.QueryRow("SELECT COUNT(*) FROM sessions WHERE token_hash = ? AND expires_at > NOW()",
			tokenHash).Scan(&count)
		if count == 0 {
			jsonError(w, http.StatusUnauthorized, "会话已过期")
			return
		}

		ctx := context.WithValue(r.Context(), contextUserID, claims.UserID)
		ctx = context.WithValue(ctx, contextUsername, claims.Username)
		ctx = context.WithValue(ctx, contextUserRole, claims.Role)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AdminMiddleware checks admin role
func AdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := r.Context().Value(contextUserRole).(string)
		if role != "admin" {
			jsonError(w, http.StatusForbidden, "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func generateToken(userID int, username, role string) (string, error) {
	claims := JWTClaims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(cfg.SessionTimeout) * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.JWTSecret))
}

func parseToken(tokenStr string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(cfg.JWTSecret), nil
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

func extractToken(r *http.Request) string {
	// From cookie
	if cookie, err := r.Cookie("alert_auth_token"); err == nil {
		return cookie.Value
	}
	// From Authorization header
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// From query param
	if t := r.URL.Query().Get("token"); t != "" {
		return t
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

func init() {
	// Clean expired sessions periodically
	go func() {
		for {
			time.Sleep(30 * time.Minute)
			if database.DB != nil {
				result, err := database.DB.Exec("DELETE FROM sessions WHERE expires_at < NOW()")
				if err == nil {
					count, _ := result.RowsAffected()
					if count > 0 {
						log.Printf("[Auth] Cleaned %d expired sessions", count)
					}
				}
			}
		}
	}()
}

// aesDecrypt decrypts AES-256-GCM encrypted base64 string
func aesDecrypt(encrypted, secret string) (string, error) {
	key := sha256.Sum256([]byte(secret))

	data, err := base64.URLEncoding.DecodeString(encrypted)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
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

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}

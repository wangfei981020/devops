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
	"opsplatform-probe-backend/config"
	"opsplatform-probe-backend/database"
)

var cfg *config.Config

func SetConfig(c *config.Config) {
	cfg = c
	// 安全加固: JWT_SECRET 默认值在生产环境不允许
	if c.JWTSecret == "" || c.JWTSecret == "dev-secret-key-change-in-production" {
		log.Fatal("[SECURITY] JWT_SECRET 未配置或仍为默认值, 请通过环境变量注入强随机值后重新启动")
	}
	if len(c.JWTSecret) < 16 {
		log.Fatal("[SECURITY] JWT_SECRET 过短 (最少 16 字符), 拒绝启动")
	}
}

func GetConfig() *config.Config { return cfg }

// maskToken returns a safe prefix of a token for logging.
func maskToken(t string) string {
	if len(t) <= 8 {
		return "***"
	}
	return t[:6] + "***"
}

type contextKey string

const (
	contextUserID   contextKey = "userID"
	contextUsername contextKey = "username"
	contextUserRole contextKey = "userRole"
)

const cookieName = "probe_auth_token"

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
	if !checkLoginLimit(w, r) {
		return
	}
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "无效的请求")
		return
	}
	if isLoginLocked(req.Username) {
		jsonError(w, http.StatusTooManyRequests, "账号因连续登录失败已被临时锁定，请 15 分钟后重试")
		return
	}
	var id int
	var username, passwordHash, role string
	err := database.DB.QueryRow("SELECT id, username, password_hash, role FROM users WHERE username = ? AND auth_source = 'local' AND status = 1",
		req.Username).Scan(&id, &username, &passwordHash, &role)
	if err != nil {
		markLoginFailure(req.Username)
		jsonError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		markLoginFailure(req.Username)
		jsonError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	clearLoginFailure(req.Username)

	token, err := generateToken(id, username, role)
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "生成令牌失败")
		return
	}
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	expiresAt := time.Now().Add(time.Duration(cfg.SessionTimeout) * time.Minute)
	database.DB.Exec("INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, ?)", id, tokenHash, expiresAt)

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
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
		"user":  map[string]interface{}{"id": id, "username": username, "role": role},
	})
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	token := extractToken(r)
	if token != "" {
		tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
		database.DB.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
	jsonSuccess(w, nil)
}

// HandlePortalAuth handles SSO auth from ops platform (same scheme as alert backend)
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

	portalToken := req.Token
	secret := os.Getenv("APP_PORTAL_SECRET")
	if secret != "" {
		decrypted, err := aesDecrypt(req.Token, secret)
		if err != nil {
			log.Printf("[PortalAuth] 解密失败: %v", err)
			jsonError(w, http.StatusUnauthorized, "Token解密失败")
			return
		}
		parts := strings.SplitN(decrypted, "|", 2)
		if len(parts) != 2 {
			jsonError(w, http.StatusUnauthorized, "Token格式无效")
			return
		}
		portalToken = parts[0]
		ts, _ := strconv.ParseInt(parts[1], 10, 64)
		if time.Since(time.Unix(ts, 0)) > 30*time.Second {
			jsonError(w, http.StatusUnauthorized, "Token已过期，请重新登录")
			return
		}
	}

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
		jsonError(w, http.StatusUnauthorized, "Portal认证失败")
		return
	}
	if role == "" {
		role = "user"
	}
	if role == "admin" || role == "super_admin" || role == "超级管理员" {
		role = "admin"
	}

	var userID int
	err = database.DB.QueryRow("SELECT id FROM users WHERE username = ? AND auth_source = 'portal'", username).Scan(&userID)
	if err != nil {
		result, err2 := database.DB.Exec("INSERT INTO users (username, display_name, role, auth_source, status) VALUES (?, ?, ?, 'portal', 1)",
			username, username, role)
		if err2 != nil {
			jsonError(w, http.StatusInternalServerError, "创建用户失败")
			return
		}
		id, _ := result.LastInsertId()
		userID = int(id)
	} else {
		database.DB.Exec("UPDATE users SET role = ? WHERE id = ?", role, userID)
	}
	database.DB.Exec("UPDATE users SET portal_token = ? WHERE id = ?", portalToken, userID)

	permissions := fetchPortalPermissions(cfg.PortalAPIURL, portalToken, username)
	if role != "admin" && permissions != nil && !permissions["menu:probe"] {
		jsonError(w, http.StatusForbidden, "您没有探测平台的访问权限，请联系管理员")
		return
	}

	token, _ := generateToken(userID, username, role)
	tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
	expiresAt := time.Now().Add(time.Duration(cfg.SessionTimeout) * time.Minute)
	database.DB.Exec("INSERT INTO sessions (user_id, token_hash, expires_at) VALUES (?, ?, ?)", userID, tokenHash, expiresAt)

	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   cfg.CookieSecure,
		MaxAge:   cfg.SessionTimeout * 60,
		SameSite: parseSameSite(cfg.CookieSameSite),
	})

	SaveAuditLog(r, "portal_login", "user", username, "SSO登录")
	jsonSuccess(w, map[string]interface{}{
		"token":       token,
		"user":        map[string]interface{}{"id": userID, "username": username, "role": role, "auth_source": "portal"},
		"permissions": permissions,
	})
}

func fetchPortalPermissions(portalURL, portalToken, username string) map[string]bool {
	permURL := strings.TrimRight(portalURL, "/") + "/api/my/permissions"
	client := &http.Client{Timeout: 5 * time.Second}
	permReq, _ := http.NewRequest("GET", permURL, nil)
	permReq.Header.Set("Authorization", "Bearer "+portalToken)
	permReq.Header.Set("X-Operator", username)
	resp, err := client.Do(permReq)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var result struct {
		Permissions map[string]bool `json:"permissions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}
	probePerms := make(map[string]bool)
	for code, granted := range result.Permissions {
		if granted && (code == "menu:probe" || strings.HasPrefix(code, "menu:probe_") || strings.HasPrefix(code, "probe:")) {
			probePerms[code] = true
		}
	}
	return probePerms
}

func HandleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	jsonSuccess(w, map[string]interface{}{
		"id":       r.Context().Value(contextUserID),
		"username": r.Context().Value(contextUsername),
		"role":     r.Context().Value(contextUserRole),
	})
}

func HandleRefreshPermissions(w http.ResponseWriter, r *http.Request) {
	if cfg.PortalAPIURL == "" {
		jsonSuccess(w, map[string]interface{}{"permissions": map[string]bool{}})
		return
	}
	userID := r.Context().Value(contextUserID).(int)
	username := r.Context().Value(contextUsername).(string)
	role := r.Context().Value(contextUserRole).(string)

	var authSource string
	database.DB.QueryRow("SELECT COALESCE(auth_source,'local') FROM users WHERE id = ?", userID).Scan(&authSource)
	if authSource != "portal" {
		jsonSuccess(w, map[string]interface{}{"permissions": map[string]bool{}, "role": role, "auth_source": authSource})
		return
	}

	permURL := strings.TrimRight(cfg.PortalAPIURL, "/") + "/my/permissions"
	client := &http.Client{Timeout: 5 * time.Second}
	permReq, _ := http.NewRequest("GET", permURL, nil)
	permReq.Header.Set("X-Operator", username)
	resp, err := client.Do(permReq)
	if err != nil {
		jsonSuccess(w, map[string]interface{}{"permissions": map[string]bool{}, "role": role, "auth_source": authSource})
		return
	}
	defer resp.Body.Close()
	var result struct {
		Permissions map[string]bool `json:"permissions"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	probePerms := make(map[string]bool)
	for code, granted := range result.Permissions {
		if granted && (code == "menu:probe" || strings.HasPrefix(code, "menu:probe_") || strings.HasPrefix(code, "probe:")) {
			probePerms[code] = true
		}
	}
	jsonSuccess(w, map[string]interface{}{"permissions": probePerms, "role": role, "auth_source": authSource})
}

// AuthMiddleware validates user login (for human users)
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
		tokenHash := fmt.Sprintf("%x", sha256.Sum256([]byte(token)))
		var count int
		database.DB.QueryRow("SELECT COUNT(*) FROM sessions WHERE token_hash = ? AND expires_at > NOW()", tokenHash).Scan(&count)
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
		UserID: userID, Username: username, Role: role,
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
	if cookie, err := r.Cookie(cookieName); err == nil {
		return cookie.Value
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
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
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func init() {
	go func() {
		for {
			time.Sleep(30 * time.Minute)
			if database.DB != nil {
				database.DB.Exec("DELETE FROM sessions WHERE expires_at < NOW()")
			}
		}
	}()
}

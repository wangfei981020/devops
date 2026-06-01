package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"opsplatform/database"
)

// ===== 安全错误处理 =====

// SafeError 安全的错误响应（不暴露内部错误详情）
func SafeError(w http.ResponseWriter, userMsg string, statusCode int, internalErr error) {
	if internalErr != nil {
		// 只在日志中记录详细错误
		log.Printf("[ERROR] %s: %v", userMsg, internalErr)
	}
	http.Error(w, userMsg, statusCode)
}

// ===== 输入验证 =====

// MaxLengths 定义各字段的最大长度
var MaxLengths = map[string]int{
	"username":      64,
	"password":      128,
	"display_name":  128,
	"project":       255,
	"module":        255,
	"vid":           255,
	"ip":            64,
	"port":          32,
	"connection_id": 128,
	"domain_name":   255,
	"url":           512,
	"description":   1024,
	"remark":        1024,
}

// ValidateLength 验证字符串长度
func ValidateLength(field, value string) bool {
	maxLen, exists := MaxLengths[field]
	if !exists {
		maxLen = 255 // 默认最大长度
	}
	return utf8.RuneCountInString(value) <= maxLen
}

// ValidateLengths 批量验证字段长度
func ValidateLengths(fields map[string]string) (bool, string) {
	for field, value := range fields {
		if !ValidateLength(field, value) {
			maxLen := MaxLengths[field]
			if maxLen == 0 {
				maxLen = 255
			}
			return false, field + " 超过最大长度限制 (" + string(rune(maxLen)) + " 字符)"
		}
	}
	return true, ""
}

// ===== JWT 密钥管理（强制环境变量） =====

var jwtSecret []byte
var jwtSecretOnce sync.Once
var jwtSecretError error

// 会话超时配置
var sessionTimeout time.Duration
var sessionTimeoutOnce sync.Once

// 会话超时选项（分钟）
const (
	SessionTimeout30Min  = 30
	SessionTimeout1Hour  = 60
	SessionTimeout3Hours = 180
	SessionTimeout24Hour = 1440
)

func getJWTSecret() ([]byte, error) {
	jwtSecretOnce.Do(func() {
		// 强制从环境变量读取密钥
		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			jwtSecretError = fmt.Errorf("JWT_SECRET 环境变量未设置，请设置至少 32 字符的密钥")
			// 生成随机密钥用于开发环境（不推荐生产使用）
			if os.Getenv("DEV_MODE") == "true" {
				b := make([]byte, 32)
				rand.Read(b)
				jwtSecret = b
				log.Println("⚠️  警告: JWT_SECRET 未设置，使用随机生成密钥（仅限开发环境）")
				jwtSecretError = nil
			}
		} else if len(secret) < 32 {
			jwtSecretError = fmt.Errorf("JWT_SECRET 长度不足，需要至少 32 字符")
		} else {
			jwtSecret = []byte(secret)
		}
	})
	return jwtSecret, jwtSecretError
}

// GetSessionTimeout 获取会话超时时间（优先从数据库读取，否则使用环境变量）
func GetSessionTimeout() time.Duration {
	// 先尝试从数据库读取
	if database.DB != nil {
		var value string
		err := database.DB.QueryRow(`SELECT setting_value FROM system_settings WHERE setting_key = 'session_timeout'`).Scan(&value)
		if err == nil && value != "" {
			minutes, err := strconv.Atoi(value)
			if err == nil && minutes > 0 {
				return time.Duration(minutes) * time.Minute
			}
		}
	}
	
	// 否则从环境变量读取
	sessionTimeoutOnce.Do(func() {
		timeoutStr := os.Getenv("SESSION_TIMEOUT")
		if timeoutStr == "" {
			sessionTimeout = 30 * time.Minute
		} else {
			minutes, err := strconv.Atoi(timeoutStr)
			if err != nil || minutes <= 0 {
				sessionTimeout = 30 * time.Minute
			} else {
				sessionTimeout = time.Duration(minutes) * time.Minute
			}
		}
		log.Printf("默认会话超时设置: %v", sessionTimeout)
	})
	return sessionTimeout
}

// ===== 会话管理 =====

// 活跃会话存储（用于检测无操作超时）
type sessionInfo struct {
	UserID     string
	Username   string
	Role       string
	LastActive time.Time
	ExpiresAt  time.Time
}

// hashToken 对 token 进行 hash（不在数据库中存储明文 token）
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// UpdateSessionActivity 更新会话活动时间（优先 Redis）
func UpdateSessionActivity(token string) {
	tokenHash := hashToken(token)
	timeout := GetSessionTimeout()

	if database.RedisEnabled {
		key := "session:" + tokenHash
		database.RedisExpire(key, timeout)
		return
	}

	database.DB.Exec(`
		UPDATE sessions SET expires_at = DATE_ADD(NOW(), INTERVAL ? MINUTE) 
		WHERE token_hash = ? AND expires_at > NOW()
	`, int(timeout.Minutes()), tokenHash)
}

// IsSessionActive 检查会话是否活跃（优先 Redis）
func IsSessionActive(token string) bool {
	tokenHash := hashToken(token)

	if database.RedisEnabled {
		key := "session:" + tokenHash
		exists, err := database.RedisExists(key)
		return err == nil && exists
	}

	var expiresAt time.Time
	err := database.DB.QueryRow(`
		SELECT expires_at FROM sessions WHERE token_hash = ?
	`, tokenHash).Scan(&expiresAt)

	if err != nil {
		return false
	}

	return time.Now().Before(expiresAt)
}

// RemoveSession 移除会话（优先 Redis）
func RemoveSession(token string) {
	tokenHash := hashToken(token)

	if database.RedisEnabled {
		database.RedisDelete("session:" + tokenHash)
		return
	}

	database.DB.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash)
}

// CreateSession 创建新会话（优先 Redis）
func CreateSession(token, userID, username, role string, expiresAt time.Time) {
	tokenHash := hashToken(token)
	ttl := time.Until(expiresAt)

	if database.RedisEnabled {
		key := "session:" + tokenHash
		database.RedisHSet(key, "user_id", userID, "username", username, "role", role)
		database.RedisExpire(key, ttl)
		return
	}

	sessionID := uuid.New().String()
	database.DB.Exec(`
		INSERT INTO sessions (id, user_id, token_hash, expires_at) VALUES (?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE expires_at = VALUES(expires_at)
	`, sessionID, userID, tokenHash, expiresAt)
}

// Claims JWT 声明
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// Cookie 名称
const (
	AuthCookieName = "auth_token"
	CookiePath     = "/"
)

// GenerateToken 生成 JWT token
func GenerateToken(userID, username, role string) (string, time.Time, error) {
	secret, err := getJWTSecret()
	if err != nil {
		return "", time.Time{}, err
	}

	// 使用配置的会话超时时间
	timeout := GetSessionTimeout()
	expiresAt := time.Now().Add(timeout)

	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "opsplatform",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secret)
	if err != nil {
		return "", time.Time{}, err
	}

	log.Printf("[AUTH-DEBUG] 生成token: user=%s secret_len=%d token_last20='...%s'", username, len(secret), tokenString[max(0, len(tokenString)-20):])

	// 创建会话记录
	CreateSession(tokenString, userID, username, role, expiresAt)

	return tokenString, expiresAt, nil
}

// ValidateToken 验证 JWT token
func ValidateToken(tokenString string) (*Claims, error) {
	secret, err := getJWTSecret()
	if err != nil {
		return nil, err
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return secret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}

// SetAuthCookie 设置认证 Cookie（HttpOnly）
func SetAuthCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	// 默认禁用 Secure（开发环境使用 HTTP），生产环境设置 COOKIE_SECURE=true
	secure := os.Getenv("COOKIE_SECURE") == "true"
	sameSite := http.SameSiteLaxMode // 使用 Lax 以支持跳转
	if os.Getenv("COOKIE_SAMESITE") == "strict" {
		sameSite = http.SameSiteStrictMode
	}

	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    token,
		Path:     CookiePath,
		Expires:  expiresAt,
		HttpOnly: true,           // 防止 XSS 攻击
		Secure:   secure,         // 生产环境启用 HTTPS
		SameSite: sameSite,       // 防止 CSRF
	})
}

// ClearAuthCookie 清除认证 Cookie
func ClearAuthCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    "",
		Path:     CookiePath,
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
	})
}

// GetTokenFromRequest 从请求中获取 Token（优先 Header，其次 Cookie）
func GetTokenFromRequest(r *http.Request) string {
	// 优先从 Authorization Header 获取（前端 axios 每次从 localStorage 读取最新 token）
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			return parts[1]
		}
	}

	// 后备：从 HttpOnly Cookie 获取（SSO/fetch with credentials 场景）
	cookie, err := r.Cookie(AuthCookieName)
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	return ""
}

// AuthMiddleware JWT 认证中间件（支持 Cookie 和 Header）
// 注意：为支持多 Pod 部署，只验证 JWT token，不依赖内存会话
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 跳过 OPTIONS 请求
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		token := GetTokenFromRequest(r)
		if token == "" {
			authHeader := r.Header.Get("Authorization")
			cookie, _ := r.Cookie(AuthCookieName)
			cookieVal := ""
			if cookie != nil {
				cookieVal = cookie.Value[:min(20, len(cookie.Value))]
			}
			log.Printf("[AUTH-DEBUG] 401 无token: path=%s auth_header='%s' cookie='%s...'", r.URL.Path, authHeader, cookieVal)
			http.Error(w, "未提供认证令牌", http.StatusUnauthorized)
			return
		}

		// 验证 JWT token（JWT 本身包含过期时间，无需内存会话）
		claims, err := ValidateToken(token)
		if err != nil {
			log.Printf("[AUTH-DEBUG] 401 token无效: path=%s token_last20='...%s' err=%v", r.URL.Path, token[max(0, len(token)-20):], err)
			ClearAuthCookie(w)
			http.Error(w, "无效的认证令牌", http.StatusUnauthorized)
			return
		}

		// 可选：更新内存会话（如果存在）
		UpdateSessionActivity(token)

		// 将用户信息存入 context
		ctx := context.WithValue(r.Context(), contextKeyUserID, claims.UserID)
		ctx = context.WithValue(ctx, contextKeyUsername, claims.Username)
		ctx = context.WithValue(ctx, contextKeyRole, claims.Role)
		ctx = context.WithValue(ctx, contextKeyToken, token)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Context Key 类型安全
type contextKey string

const (
	contextKeyUserID   contextKey = "user_id"
	contextKeyUsername contextKey = "username"
	contextKeyRole     contextKey = "role"
	contextKeyToken    contextKey = "token"
)

// AdminOnlyMiddleware 仅管理员可访问的中间件
func AdminOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := r.Context().Value(contextKeyRole)
		roleStr := ""
		if role != nil {
			roleStr = role.(string)
		}
		if roleStr != "admin" && roleStr != "super_admin" {
			http.Error(w, "权限不足，需要管理员权限", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetUserFromContext 从 context 获取用户信息
func GetUserFromContext(r *http.Request) (userID, username, role string) {
	if v := r.Context().Value(contextKeyUserID); v != nil {
		userID = v.(string)
	}
	if v := r.Context().Value(contextKeyUsername); v != nil {
		username = v.(string)
	}
	if v := r.Context().Value(contextKeyRole); v != nil {
		role = v.(string)
	}
	return
}

// UserHasPermission 检查用户是否拥有指定权限（超级管理员/管理员默认放行）
func UserHasPermission(username, role, permissionCode string) (bool, error) {
	// 兼容旧角色体系：admin/super_admin 直接拥有全部权限
	if role == "super_admin" || role == "admin" || role == "超级管理员" {
		return true, nil
	}
	if username == "" {
		fmt.Printf("[DEBUG] UserHasPermission: username为空, 返回false\n")
		return false, nil
	}

	// 兼容新 RBAC：角色 code 为 super_admin/admin 时也放行
	var adminRoleCount int
	if err := database.DB.QueryRow(`
		SELECT COUNT(*)
		FROM users u
		JOIN user_roles ur ON ur.user_id = u.id
		JOIN roles r ON r.id = ur.role_id
		WHERE u.username = ? AND r.code IN ('super_admin', 'admin')
	`, username).Scan(&adminRoleCount); err != nil {
		fmt.Printf("[DEBUG] UserHasPermission: 查询admin角色出错: %v\n", err)
		return false, err
	}
	if adminRoleCount > 0 {
		return true, nil
	}

	var count int
	err := database.DB.QueryRow(`
		SELECT COUNT(*)
		FROM users u
		JOIN user_roles ur ON ur.user_id = u.id
		JOIN role_permissions rp ON rp.role_id = ur.role_id
		JOIN permissions p ON p.id = rp.permission_id
		WHERE u.username = ? AND p.code = ?
	`, username, permissionCode).Scan(&count)
	if err != nil {
		fmt.Printf("[DEBUG] UserHasPermission: 查询权限出错: username=%s, code=%s, err=%v\n", username, permissionCode, err)
		return false, err
	}
	fmt.Printf("[DEBUG] UserHasPermission: username=%s, permissionCode=%s, count=%d\n", username, permissionCode, count)
	return count > 0, nil
}

// HasAnyPermission 检查请求用户是否拥有任一指定权限
func HasAnyPermission(r *http.Request, permissionCodes ...string) bool {
	_, username, role := GetUserFromContext(r)
	for _, code := range permissionCodes {
		ok, err := UserHasPermission(username, role, code)
		if err == nil && ok {
			return true
		}
	}
	return false
}

// HandleLogout 处理登出请求
func HandleLogout(w http.ResponseWriter, r *http.Request) {
	token := GetTokenFromRequest(r)
	if token != "" {
		RemoveSession(token)
	}
	ClearAuthCookie(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"message": "已退出登录"})
}

// HandleGetSessionInfo 获取当前会话信息
func HandleGetSessionInfo(w http.ResponseWriter, r *http.Request) {
	token := GetTokenFromRequest(r)
	tokenHash := hashToken(token)

	var userID string
	var expiresAt time.Time
	err := database.DB.QueryRow(`
		SELECT user_id, expires_at FROM sessions WHERE token_hash = ?
	`, tokenHash).Scan(&userID, &expiresAt)

	if err != nil {
		http.Error(w, "会话不存在", http.StatusUnauthorized)
		return
	}

	timeout := GetSessionTimeout()
	remaining := time.Until(expiresAt)
	if remaining < 0 {
		remaining = 0
	}

	// 从 JWT 获取用户信息
	claims, _ := ValidateToken(token)
	username := ""
	role := ""
	if claims != nil {
		username = claims.Username
		role = claims.Role
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id":           userID,
		"username":          username,
		"role":              role,
		"expires_at":        expiresAt.Format(time.RFC3339),
		"remaining_seconds": int(remaining.Seconds()),
		"timeout_minutes":   int(timeout.Minutes()),
	})
}

// HandleRefreshSession 刷新会话
func HandleRefreshSession(w http.ResponseWriter, r *http.Request) {
	token := GetTokenFromRequest(r)
	tokenHash := hashToken(token)

	var userID string
	err := database.DB.QueryRow(`
		SELECT user_id FROM sessions WHERE token_hash = ? AND expires_at > NOW()
	`, tokenHash).Scan(&userID)

	if err != nil {
		http.Error(w, "会话不存在", http.StatusUnauthorized)
		return
	}

	// 从 JWT 获取用户信息
	claims, _ := ValidateToken(token)
	if claims == nil {
		http.Error(w, "会话无效", http.StatusUnauthorized)
		return
	}

	// 生成新 Token
	newToken, expiresAt, err := GenerateToken(claims.UserID, claims.Username, claims.Role)
	if err != nil {
		http.Error(w, "刷新会话失败", http.StatusInternalServerError)
		return
	}

	// 移除旧会话
	RemoveSession(token)

	// 创建新会话
	CreateSession(newToken, claims.UserID, claims.Username, claims.Role, expiresAt)

	// 设置新 Cookie
	SetAuthCookie(w, newToken, expiresAt)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":    "会话已刷新",
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

// HandleGetPortalToken 返回当前用户的 JWT token（供外部应用 portal 认证使用）
func HandleGetPortalToken(w http.ResponseWriter, r *http.Request) {
	token := GetTokenFromRequest(r)
	if token == "" {
		http.Error(w, "未认证", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}

// ===== 登录速率限制 =====

type loginAttempt struct {
	count     int
	lastReset time.Time
	lockedAt  time.Time
}

var (
	loginAttempts = make(map[string]*loginAttempt)
	loginMutex    sync.RWMutex
	maxAttempts   = 5
	lockDuration  = 15 * time.Minute
	resetDuration = 10 * time.Minute
)

// CheckLoginRateLimit 检查登录速率限制（优先使用 Redis）
func CheckLoginRateLimit(ip string) (bool, time.Duration) {
	if database.RedisEnabled {
		return checkLoginRateLimitRedis(ip)
	}
	return checkLoginRateLimitMemory(ip)
}

func checkLoginRateLimitRedis(ip string) (bool, time.Duration) {
	lockKey := "login:lock:" + ip
	
	ttl, err := database.RedisTTL(lockKey)
	if err == nil && ttl > 0 {
		return false, ttl
	}
	
	return true, 0
}

func checkLoginRateLimitMemory(ip string) (bool, time.Duration) {
	loginMutex.Lock()
	defer loginMutex.Unlock()

	attempt, exists := loginAttempts[ip]
	if !exists {
		loginAttempts[ip] = &loginAttempt{count: 0, lastReset: time.Now()}
		return true, 0
	}

	if !attempt.lockedAt.IsZero() {
		remaining := lockDuration - time.Since(attempt.lockedAt)
		if remaining > 0 {
			return false, remaining
		}
		attempt.lockedAt = time.Time{}
		attempt.count = 0
		attempt.lastReset = time.Now()
	}

	if time.Since(attempt.lastReset) > resetDuration {
		attempt.count = 0
		attempt.lastReset = time.Now()
	}

	return true, 0
}

// RecordLoginAttempt 记录登录尝试（优先使用 Redis）
func RecordLoginAttempt(ip string, success bool) {
	if database.RedisEnabled {
		recordLoginAttemptRedis(ip, success)
		return
	}
	recordLoginAttemptMemory(ip, success)
}

func recordLoginAttemptRedis(ip string, success bool) {
	attemptKey := "login:attempts:" + ip
	lockKey := "login:lock:" + ip

	if success {
		database.RedisDelete(attemptKey)
		database.RedisDelete(lockKey)
		return
	}

	count, _ := database.RedisIncr(attemptKey)
	database.RedisExpire(attemptKey, resetDuration)

	if int(count) >= maxAttempts {
		database.RedisSet(lockKey, "1", lockDuration)
		database.RedisDelete(attemptKey)
	}
}

func recordLoginAttemptMemory(ip string, success bool) {
	loginMutex.Lock()
	defer loginMutex.Unlock()

	attempt, exists := loginAttempts[ip]
	if !exists {
		attempt = &loginAttempt{count: 0, lastReset: time.Now()}
		loginAttempts[ip] = attempt
	}

	if success {
		attempt.count = 0
		attempt.lastReset = time.Now()
	} else {
		attempt.count++
		if attempt.count >= maxAttempts {
			attempt.lockedAt = time.Now()
		}
	}
}

// ===== CSRF 保护（优先 Redis，降级到数据库） =====

// GenerateCSRFToken 生成 CSRF token（优先 Redis）
func GenerateCSRFToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)

	if database.RedisEnabled {
		key := "csrf:" + token
		database.RedisSet(key, "1", 1*time.Hour)
		return token
	}

	expiresAt := time.Now().Add(1 * time.Hour)
	database.DB.Exec(`
		INSERT INTO csrf_tokens (token, expires_at) VALUES (?, ?)
	`, token, expiresAt)

	return token
}

// ValidateCSRFToken 验证 CSRF token（优先 Redis）
func ValidateCSRFToken(token string) bool {
	if database.RedisEnabled {
		key := "csrf:" + token
		exists, err := database.RedisExists(key)
		if err != nil || !exists {
			return false
		}
		database.RedisDelete(key)
		return true
	}

	var expiresAt time.Time
	err := database.DB.QueryRow(`
		SELECT expires_at FROM csrf_tokens WHERE token = ?
	`, token).Scan(&expiresAt)

	if err != nil || time.Now().After(expiresAt) {
		return false
	}

	database.DB.Exec("DELETE FROM csrf_tokens WHERE token = ?", token)

	return true
}

// HandleGetCSRFToken 获取 CSRF token
func HandleGetCSRFToken(w http.ResponseWriter, r *http.Request) {
	token := GenerateCSRFToken()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"csrf_token": token})
}

// CSRFMiddleware CSRF 中间件（默认启用，DISABLE_CSRF=true 禁用）
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只检查修改类请求
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
			// 跳过登录、登出和 CSRF token 获取
			if strings.HasSuffix(r.URL.Path, "/login") ||
				strings.HasSuffix(r.URL.Path, "/logout") ||
				strings.HasSuffix(r.URL.Path, "/csrf-token") ||
				strings.HasSuffix(r.URL.Path, "/mfa/verify") ||
				strings.HasSuffix(r.URL.Path, "/mfa/bind") {
				next.ServeHTTP(w, r)
				return
			}

			// CSRF 检查（默认启用，DISABLE_CSRF=true 禁用）
			if os.Getenv("DISABLE_CSRF") != "true" {
				token := r.Header.Get("X-CSRF-Token")
				if token == "" || !ValidateCSRFToken(token) {
					http.Error(w, "无效的 CSRF Token", http.StatusForbidden)
					return
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

// 清理过期的登录尝试记录、会话和 CSRF token
func init() {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		for range ticker.C {
			// 清理登录尝试
			loginMutex.Lock()
			for ip, attempt := range loginAttempts {
				if time.Since(attempt.lastReset) > 30*time.Minute {
					delete(loginAttempts, ip)
				}
			}
			loginMutex.Unlock()

			// 清理过期会话（从数据库）
			database.DB.Exec("DELETE FROM sessions WHERE expires_at < NOW()")

			// 清理过期 CSRF tokens（从数据库）
			database.DB.Exec("DELETE FROM csrf_tokens WHERE expires_at < NOW()")

			// 清理 API 速率限制记录
			apiRateMutex.Lock()
			for key, record := range apiRateLimits {
				if time.Since(record.WindowStart) > time.Minute {
					delete(apiRateLimits, key)
				}
			}
			apiRateMutex.Unlock()
		}
	}()
}

// ===== 安全响应头中间件 =====

// SecurityHeadersMiddleware 添加安全响应头
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 防止点击劫持
		w.Header().Set("X-Frame-Options", "DENY")
		
		// 防止 MIME 类型嗅探
		w.Header().Set("X-Content-Type-Options", "nosniff")
		
		// XSS 防护（现代浏览器）
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		
		// 内容安全策略（CSP）- 已加固
		// 注意：Vue.js 运行时模板编译需要 unsafe-eval；迁移到 SFC 预编译后可移除
		// unsafe-inline 仅用于内联样式；已移除外部字体源
		csp := "default-src 'self'; " +
			"script-src 'self' 'unsafe-eval'; " +
			"style-src 'self' 'unsafe-inline'; " +
			"font-src 'self' data:; " +
			"img-src 'self' data: blob:; " +
			"connect-src 'self'; " +
			"frame-ancestors 'none'; " +
			"base-uri 'self'; " +
			"form-action 'self'"
		w.Header().Set("Content-Security-Policy", csp)
		
		// 引用策略
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		
		// 权限策略（禁用不需要的浏览器功能）
		w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		
		// 缓存控制（API 响应不缓存）
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
			w.Header().Set("Pragma", "no-cache")
		}
		
		next.ServeHTTP(w, r)
	})
}

// ===== API 速率限制 =====

// apiRateRecord API 速率限制记录
type apiRateRecord struct {
	Count       int
	WindowStart time.Time
}

var (
	apiRateLimits = make(map[string]*apiRateRecord) // IP:Path -> record
	apiRateMutex  sync.RWMutex
)

// API 速率限制配置
var apiRateLimitConfig = map[string]int{
	"/api/login":                  10,  // 登录：每分钟10次
	"/api/mfa/verify":             10,  // MFA验证：每分钟10次
	"/api/users":                  60,  // 用户API：每分钟60次
	"/api/records":                120, // 记录API：每分钟120次
	"/api/domains":                120, // 域名API：每分钟120次
	"/api/audit":                  60,  // 审计API：每分钟60次
	"/api/storage/presign/batch":  500, // v750: 预签名批量接口，列表加载时单页可能触发多次
	"default":                     100, // 默认：每分钟100次
}

// RateLimitMiddleware API 速率限制中间件（优先使用 Redis）
func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := GetClientIP(r)
		path := r.URL.Path
		limit := apiRateLimitConfig["default"]
		
		for configPath, configLimit := range apiRateLimitConfig {
			if configPath != "default" && strings.HasPrefix(path, configPath) {
				limit = configLimit
				break
			}
		}
		
		var count int
		var allowed bool

		if database.RedisEnabled {
			count, allowed = checkRateLimitRedis(ip, path, limit)
		} else {
			count, allowed = checkRateLimitMemory(ip, path, limit)
		}

		if !allowed {
			w.Header().Set("Retry-After", "60")
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
			w.Header().Set("X-RateLimit-Remaining", "0")
			http.Error(w, "请求过于频繁，请稍后再试", http.StatusTooManyRequests)
			log.Printf("[RATE_LIMIT] IP=%s Path=%s Count=%d Limit=%d", ip, path, count, limit)
			return
		}

		remaining := limit - count
		if remaining < 0 {
			remaining = 0
		}
		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		
		next.ServeHTTP(w, r)
	})
}

func checkRateLimitRedis(ip, path string, limit int) (int, bool) {
	key := "rate:" + ip + ":" + path
	
	count, err := database.RedisIncr(key)
	if err != nil {
		return 0, true
	}
	
	if count == 1 {
		database.RedisExpire(key, time.Minute)
	}
	
	return int(count), int(count) <= limit
}

func checkRateLimitMemory(ip, path string, limit int) (int, bool) {
	key := ip + ":" + path
	
	apiRateMutex.Lock()
	defer apiRateMutex.Unlock()
	
	record, exists := apiRateLimits[key]
	now := time.Now()
	
	if !exists || now.Sub(record.WindowStart) > time.Minute {
		apiRateLimits[key] = &apiRateRecord{
			Count:       1,
			WindowStart: now,
		}
		return 1, true
	}
	
	record.Count++
	return record.Count, record.Count <= limit
}

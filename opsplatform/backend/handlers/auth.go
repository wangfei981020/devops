package handlers

import (
	"context"
	"crypto/rand"
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

var (
	activeSessions = make(map[string]*sessionInfo) // token -> session
	sessionMutex   sync.RWMutex
)

// UpdateSessionActivity 更新会话活动时间
func UpdateSessionActivity(token string) {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	if session, exists := activeSessions[token]; exists {
		session.LastActive = time.Now()
	}
}

// IsSessionActive 检查会话是否活跃
func IsSessionActive(token string) bool {
	sessionMutex.RLock()
	defer sessionMutex.RUnlock()

	session, exists := activeSessions[token]
	if !exists {
		return false
	}

	timeout := GetSessionTimeout()
	// 检查无操作超时
	if time.Since(session.LastActive) > timeout {
		return false
	}
	// 检查绝对过期时间
	if time.Now().After(session.ExpiresAt) {
		return false
	}
	return true
}

// RemoveSession 移除会话
func RemoveSession(token string) {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	delete(activeSessions, token)
}

// CreateSession 创建新会话
func CreateSession(token, userID, username, role string, expiresAt time.Time) {
	sessionMutex.Lock()
	defer sessionMutex.Unlock()
	activeSessions[token] = &sessionInfo{
		UserID:     userID,
		Username:   username,
		Role:       role,
		LastActive: time.Now(),
		ExpiresAt:  expiresAt,
	}
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

// GetTokenFromRequest 从请求中获取 Token（优先 Cookie，其次 Header）
func GetTokenFromRequest(r *http.Request) string {
	// 优先从 HttpOnly Cookie 获取
	cookie, err := r.Cookie(AuthCookieName)
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// 兼容：从 Authorization Header 获取
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			return parts[1]
		}
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
			http.Error(w, "未提供认证令牌", http.StatusUnauthorized)
			return
		}

		// 验证 JWT token（JWT 本身包含过期时间，无需内存会话）
		claims, err := ValidateToken(token)
		if err != nil {
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
		if role == nil || role.(string) != "admin" {
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
	
	sessionMutex.RLock()
	session, exists := activeSessions[token]
	sessionMutex.RUnlock()

	if !exists {
		http.Error(w, "会话不存在", http.StatusUnauthorized)
		return
	}

	timeout := GetSessionTimeout()
	remaining := timeout - time.Since(session.LastActive)
	if remaining < 0 {
		remaining = 0
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user_id":          session.UserID,
		"username":         session.Username,
		"role":             session.Role,
		"last_active":      session.LastActive.Format(time.RFC3339),
		"expires_at":       session.ExpiresAt.Format(time.RFC3339),
		"remaining_seconds": int(remaining.Seconds()),
		"timeout_minutes":  int(timeout.Minutes()),
	})
}

// HandleRefreshSession 刷新会话
func HandleRefreshSession(w http.ResponseWriter, r *http.Request) {
	token := GetTokenFromRequest(r)
	
	sessionMutex.RLock()
	session, exists := activeSessions[token]
	sessionMutex.RUnlock()

	if !exists {
		http.Error(w, "会话不存在", http.StatusUnauthorized)
		return
	}

	// 生成新 Token
	newToken, expiresAt, err := GenerateToken(session.UserID, session.Username, session.Role)
	if err != nil {
		http.Error(w, "刷新会话失败", http.StatusInternalServerError)
		return
	}

	// 移除旧会话
	RemoveSession(token)

	// 设置新 Cookie
	SetAuthCookie(w, newToken, expiresAt)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":    "会话已刷新",
		"expires_at": expiresAt.Format(time.RFC3339),
	})
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

// CheckLoginRateLimit 检查登录速率限制
func CheckLoginRateLimit(ip string) (bool, time.Duration) {
	loginMutex.Lock()
	defer loginMutex.Unlock()

	attempt, exists := loginAttempts[ip]
	if !exists {
		loginAttempts[ip] = &loginAttempt{count: 0, lastReset: time.Now()}
		return true, 0
	}

	// 如果被锁定，检查是否解锁
	if !attempt.lockedAt.IsZero() {
		remaining := lockDuration - time.Since(attempt.lockedAt)
		if remaining > 0 {
			return false, remaining
		}
		// 解锁
		attempt.lockedAt = time.Time{}
		attempt.count = 0
		attempt.lastReset = time.Now()
	}

	// 重置计数器
	if time.Since(attempt.lastReset) > resetDuration {
		attempt.count = 0
		attempt.lastReset = time.Now()
	}

	return true, 0
}

// RecordLoginAttempt 记录登录尝试
func RecordLoginAttempt(ip string, success bool) {
	loginMutex.Lock()
	defer loginMutex.Unlock()

	attempt, exists := loginAttempts[ip]
	if !exists {
		attempt = &loginAttempt{count: 0, lastReset: time.Now()}
		loginAttempts[ip] = attempt
	}

	if success {
		// 登录成功，重置计数
		attempt.count = 0
		attempt.lastReset = time.Now()
	} else {
		// 登录失败，增加计数
		attempt.count++
		if attempt.count >= maxAttempts {
			attempt.lockedAt = time.Now()
		}
	}
}

// ===== CSRF 保护（默认启用） =====

var (
	csrfTokens = make(map[string]time.Time)
	csrfMutex  sync.RWMutex
)

// GenerateCSRFToken 生成 CSRF token
func GenerateCSRFToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	token := hex.EncodeToString(b)

	csrfMutex.Lock()
	csrfTokens[token] = time.Now().Add(1 * time.Hour)
	csrfMutex.Unlock()

	return token
}

// ValidateCSRFToken 验证 CSRF token
func ValidateCSRFToken(token string) bool {
	csrfMutex.RLock()
	expiry, exists := csrfTokens[token]
	csrfMutex.RUnlock()

	if !exists || time.Now().After(expiry) {
		return false
	}

	// 使用后删除（单次使用）
	csrfMutex.Lock()
	delete(csrfTokens, token)
	csrfMutex.Unlock()

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

			// 清理过期会话
			timeout := GetSessionTimeout()
			sessionMutex.Lock()
			for token, session := range activeSessions {
				// 检查无操作超时或绝对过期
				if time.Since(session.LastActive) > timeout || time.Now().After(session.ExpiresAt) {
					delete(activeSessions, token)
				}
			}
			sessionMutex.Unlock()

			// 清理 CSRF tokens
			csrfMutex.Lock()
			for token, expiry := range csrfTokens {
				if time.Now().After(expiry) {
					delete(csrfTokens, token)
				}
			}
			csrfMutex.Unlock()

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
		
		// 内容安全策略（CSP）
		// 允许同源资源、内联样式和脚本（Vue.js 需要）、Google Fonts
		csp := "default-src 'self'; " +
			"script-src 'self' 'unsafe-inline' 'unsafe-eval'; " +
			"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
			"font-src 'self' https://fonts.gstatic.com data:; " +
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
	"/api/login":         10,  // 登录：每分钟10次
	"/api/mfa/verify":    10,  // MFA验证：每分钟10次
	"/api/users":         60,  // 用户API：每分钟60次
	"/api/records":       120, // 记录API：每分钟120次
	"/api/domains":       120, // 域名API：每分钟120次
	"/api/audit":         60,  // 审计API：每分钟60次
	"default":            100, // 默认：每分钟100次
}

// RateLimitMiddleware API 速率限制中间件
func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 获取客户端 IP
		ip := GetClientIP(r)
		
		// 获取路径的速率限制
		path := r.URL.Path
		limit := apiRateLimitConfig["default"]
		
		// 匹配特定路径的限制
		for configPath, configLimit := range apiRateLimitConfig {
			if configPath != "default" && strings.HasPrefix(path, configPath) {
				limit = configLimit
				break
			}
		}
		
		// 生成限制键
		key := ip + ":" + path
		
		apiRateMutex.Lock()
		record, exists := apiRateLimits[key]
		now := time.Now()
		
		if !exists || now.Sub(record.WindowStart) > time.Minute {
			// 新窗口
			apiRateLimits[key] = &apiRateRecord{
				Count:       1,
				WindowStart: now,
			}
			apiRateMutex.Unlock()
		} else {
			record.Count++
			if record.Count > limit {
				apiRateMutex.Unlock()
				// 返回 429 Too Many Requests
				w.Header().Set("Retry-After", "60")
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
				w.Header().Set("X-RateLimit-Remaining", "0")
				http.Error(w, "请求过于频繁，请稍后再试", http.StatusTooManyRequests)
				log.Printf("[RATE_LIMIT] IP=%s Path=%s Count=%d Limit=%d", ip, path, record.Count, limit)
				return
			}
			apiRateMutex.Unlock()
		}
		
		// 添加速率限制响应头
		apiRateMutex.RLock()
		if rec, ok := apiRateLimits[key]; ok {
			remaining := limit - rec.Count
			if remaining < 0 {
				remaining = 0
			}
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", limit))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))
		}
		apiRateMutex.RUnlock()
		
		next.ServeHTTP(w, r)
	})
}

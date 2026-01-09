package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
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

// JWT 密钥（从环境变量读取，如果没有则使用默认值）
var jwtSecret []byte
var jwtSecretOnce sync.Once

func getJWTSecret() []byte {
	jwtSecretOnce.Do(func() {
		// 从环境变量读取密钥
		secret := os.Getenv("JWT_SECRET")
		if secret != "" {
			jwtSecret = []byte(secret)
		} else {
			// 使用固定默认密钥（生产环境应该设置环境变量）
			jwtSecret = []byte("opsplatform-jwt-secret-key-2026")
		}
	})
	return jwtSecret
}

// Claims JWT 声明
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// GenerateToken 生成 JWT token
func GenerateToken(userID, username, role string) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "opsplatform",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(getJWTSecret())
}

// ValidateToken 验证 JWT token
func ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return getJWTSecret(), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}

// AuthMiddleware JWT 认证中间件
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 跳过 OPTIONS 请求
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		// 从 Authorization header 获取 token
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "未提供认证令牌", http.StatusUnauthorized)
			return
		}

		// Bearer token 格式
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "无效的认证格式", http.StatusUnauthorized)
			return
		}

		claims, err := ValidateToken(parts[1])
		if err != nil {
			http.Error(w, "无效的认证令牌", http.StatusUnauthorized)
			return
		}

		// 将用户信息存入 context
		ctx := context.WithValue(r.Context(), "user_id", claims.UserID)
		ctx = context.WithValue(ctx, "username", claims.Username)
		ctx = context.WithValue(ctx, "role", claims.Role)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AdminOnlyMiddleware 仅管理员可访问的中间件
func AdminOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := r.Context().Value("role")
		if role == nil || role.(string) != "admin" {
			http.Error(w, "权限不足，需要管理员权限", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// GetUserFromContext 从 context 获取用户信息
func GetUserFromContext(r *http.Request) (userID, username, role string) {
	if v := r.Context().Value("user_id"); v != nil {
		userID = v.(string)
	}
	if v := r.Context().Value("username"); v != nil {
		username = v.(string)
	}
	if v := r.Context().Value("role"); v != nil {
		role = v.(string)
	}
	return
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

// ===== CSRF 保护 =====

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

// CSRFMiddleware CSRF 中间件
func CSRFMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只检查修改类请求
		if r.Method == "POST" || r.Method == "PUT" || r.Method == "DELETE" {
			// 跳过登录和 CSRF token 获取
			if strings.HasSuffix(r.URL.Path, "/login") ||
				strings.HasSuffix(r.URL.Path, "/csrf-token") ||
				strings.HasSuffix(r.URL.Path, "/mfa/verify") ||
				strings.HasSuffix(r.URL.Path, "/mfa/bind") {
				next.ServeHTTP(w, r)
				return
			}

			// CSRF 检查（通过环境变量 ENABLE_CSRF=true 启用）
			if os.Getenv("ENABLE_CSRF") == "true" {
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

// 清理过期的登录尝试记录和 CSRF token
func init() {
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		for range ticker.C {
			// 清理登录尝试
			loginMutex.Lock()
			for ip, attempt := range loginAttempts {
				if time.Since(attempt.lastReset) > 30*time.Minute {
					delete(loginAttempts, ip)
				}
			}
			loginMutex.Unlock()

			// 清理 CSRF tokens
			csrfMutex.Lock()
			for token, expiry := range csrfTokens {
				if time.Now().After(expiry) {
					delete(csrfTokens, token)
				}
			}
			csrfMutex.Unlock()
		}
	}()
}

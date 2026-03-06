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

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"jira-center-backend/database"
	"jira-center-backend/models"
)

// ===== JWT 密钥管理 =====

var jwtSecret []byte
var jwtSecretOnce sync.Once
var jwtSecretError error

var sessionTimeout time.Duration
var sessionTimeoutOnce sync.Once

func getJWTSecret() ([]byte, error) {
	jwtSecretOnce.Do(func() {
		secret := os.Getenv("JWT_SECRET")
		if secret == "" {
			jwtSecretError = fmt.Errorf("JWT_SECRET 环境变量未设置")
			if os.Getenv("DEV_MODE") == "true" {
				b := make([]byte, 32)
				rand.Read(b)
				jwtSecret = b
				log.Println("警告: JWT_SECRET 未设置，使用随机生成密钥（仅限开发环境）")
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

// GetSessionTimeout 获取会话超时时间
func GetSessionTimeout() time.Duration {
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

	sessionTimeoutOnce.Do(func() {
		timeoutStr := os.Getenv("SESSION_TIMEOUT")
		if timeoutStr == "" {
			sessionTimeout = 180 * time.Minute
		} else {
			minutes, err := strconv.Atoi(timeoutStr)
			if err != nil || minutes <= 0 {
				sessionTimeout = 180 * time.Minute
			} else {
				sessionTimeout = time.Duration(minutes) * time.Minute
			}
		}
	})
	return sessionTimeout
}

// ===== 会话管理 =====

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func checkPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// UpdateSessionActivity 更新会话活动时间
func UpdateSessionActivity(token string) {
	tokenHash := hashToken(token)
	timeout := GetSessionTimeout()

	if database.RedisEnabled {
		database.RedisExpire("session:"+tokenHash, timeout)
		return
	}

	database.DB.Exec(`
		UPDATE sessions SET expires_at = DATE_ADD(NOW(), INTERVAL ? MINUTE)
		WHERE token_hash = ? AND expires_at > NOW()
	`, int(timeout.Minutes()), tokenHash)
}

// CreateSession 创建新会话
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

// RemoveSession 移除会话
func RemoveSession(token string) {
	tokenHash := hashToken(token)
	if database.RedisEnabled {
		database.RedisDelete("session:" + tokenHash)
		return
	}
	database.DB.Exec("DELETE FROM sessions WHERE token_hash = ?", tokenHash)
}

// ===== JWT =====

// Claims JWT 声明
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

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

	timeout := GetSessionTimeout()
	expiresAt := time.Now().Add(timeout)

	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "jira-center",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(secret)
	if err != nil {
		return "", time.Time{}, err
	}

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

// ===== Cookie 管理 =====

// SetAuthCookie 设置认证 Cookie
func SetAuthCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	secure := os.Getenv("COOKIE_SECURE") == "true"
	sameSite := http.SameSiteLaxMode
	if os.Getenv("COOKIE_SAMESITE") == "strict" {
		sameSite = http.SameSiteStrictMode
	}

	http.SetCookie(w, &http.Cookie{
		Name:     AuthCookieName,
		Value:    token,
		Path:     CookiePath,
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
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

// GetTokenFromRequest 从请求中获取 Token
func GetTokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(AuthCookieName)
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}

	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			return parts[1]
		}
	}

	return ""
}

// ===== 中间件 =====

type contextKey string

const (
	contextKeyUserID   contextKey = "user_id"
	contextKeyUsername contextKey = "username"
	contextKeyRole     contextKey = "role"
	contextKeyToken    contextKey = "token"
)

// AuthMiddleware JWT 认证中间件
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			next.ServeHTTP(w, r)
			return
		}

		token := GetTokenFromRequest(r)
		if token == "" {
			http.Error(w, "未提供认证令牌", http.StatusUnauthorized)
			return
		}

		claims, err := ValidateToken(token)
		if err != nil {
			ClearAuthCookie(w)
			http.Error(w, "无效的认证令牌", http.StatusUnauthorized)
			return
		}

		UpdateSessionActivity(token)

		ctx := context.WithValue(r.Context(), contextKeyUserID, claims.UserID)
		ctx = context.WithValue(ctx, contextKeyUsername, claims.Username)
		ctx = context.WithValue(ctx, contextKeyRole, claims.Role)
		ctx = context.WithValue(ctx, contextKeyToken, token)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AdminOnlyMiddleware 仅管理员可访问
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

// GetClientIP 获取客户端 IP
func GetClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	parts := strings.Split(r.RemoteAddr, ":")
	if len(parts) > 0 {
		return parts[0]
	}
	return r.RemoteAddr
}

// ===== 登录/登出处理 =====

// HandleLogin 用户登录
func HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondBadRequest(w, "无效的请求数据")
		return
	}

	var user models.User
	err := database.DB.QueryRow(`
		SELECT id, username, password, display_name, email, role, status
		FROM users WHERE username = ?
	`, req.Username).Scan(&user.ID, &user.Username, &user.Password, &user.DisplayName, &user.Email, &user.Role, &user.Status)

	if err != nil {
		respondUnauthorized(w, "用户名或密码错误")
		return
	}

	if user.Status != "active" {
		respondForbidden(w, "账户已被禁用")
		return
	}

	if !checkPassword(user.Password, req.Password) {
		respondUnauthorized(w, "用户名或密码错误")
		return
	}

	token, expiresAt, err := GenerateToken(user.ID, user.Username, user.Role)
	if err != nil {
		respondInternalError(w, "生成令牌失败")
		return
	}

	SetAuthCookie(w, token, expiresAt)

	// 记录审计日志
	AddAuditLog(user.ID, user.Username, "login", "user", user.ID, "用户登录", GetClientIP(r))

	user.Password = ""
	respondSuccess(w, map[string]interface{}{
		"user":  user,
		"token": token,
	})
}

// HandleLogout 用户登出
func HandleLogout(w http.ResponseWriter, r *http.Request) {
	token := GetTokenFromRequest(r)
	if token != "" {
		RemoveSession(token)
	}
	ClearAuthCookie(w)
	respondSuccess(w, map[string]string{"message": "已登出"})
}

// HandleGetCurrentUser 获取当前用户信息
func HandleGetCurrentUser(w http.ResponseWriter, r *http.Request) {
	userID, _, _ := GetUserFromContext(r)

	var user models.User
	err := database.DB.QueryRow(`
		SELECT id, username, display_name, email, role, status, auth_source, created_at
		FROM users WHERE id = ?
	`, userID).Scan(&user.ID, &user.Username, &user.DisplayName, &user.Email, &user.Role, &user.Status, &user.AuthSource, &user.CreatedAt)

	if err != nil {
		respondUnauthorized(w, "用户不存在")
		return
	}

	respondSuccess(w, user)
}

// HandleRefreshSession 刷新会话
func HandleRefreshSession(w http.ResponseWriter, r *http.Request) {
	userID, username, role := GetUserFromContext(r)

	oldToken := GetTokenFromRequest(r)
	if oldToken != "" {
		RemoveSession(oldToken)
	}

	token, expiresAt, err := GenerateToken(userID, username, role)
	if err != nil {
		respondInternalError(w, "刷新会话失败")
		return
	}

	SetAuthCookie(w, token, expiresAt)
	respondSuccess(w, map[string]interface{}{
		"token":      token,
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

// InitDefaultAdmin 初始化默认管理员
func InitDefaultAdmin() error {
	var count int
	database.DB.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if count > 0 {
		return nil
	}

	password, err := hashPassword("admin123")
	if err != nil {
		return err
	}

	id := uuid.New().String()
	_, err = database.DB.Exec(`
		INSERT INTO users (id, username, password, display_name, role, status, auth_source, created_at, updated_at)
		VALUES (?, 'admin', ?, '管理员', 'admin', 'active', 'local', NOW(), NOW())
	`, id, password)

	if err != nil {
		return err
	}

	log.Println("默认管理员已创建: admin / admin123")
	return nil
}

// AddAuditLog 添加审计日志
func AddAuditLog(userID, username, action, targetType, targetID, detail, ip string) {
	id := uuid.New().String()
	database.DB.Exec(`
		INSERT INTO audit_logs (id, user_id, username, action, target_type, target_id, detail, ip, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NOW())
	`, id, userID, username, action, targetType, targetID, detail, ip)
}

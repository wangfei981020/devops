package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
	err := database.DB.QueryRow("SELECT id, username, password_hash, role FROM users WHERE username = ? AND status = 1",
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

	// Verify with portal
	client := &http.Client{Timeout: 10 * time.Second}
	portalReq, _ := http.NewRequest("GET", cfg.PortalAPIURL+"/api/users/me", nil)
	portalReq.Header.Set("Authorization", "Bearer "+req.Token)
	resp, err := client.Do(portalReq)
	if err != nil {
		jsonError(w, http.StatusBadGateway, "Portal验证失败")
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var portalResp struct {
		Code int `json:"code"`
		Data struct {
			ID       int    `json:"id"`
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &portalResp); err != nil || portalResp.Code != 0 {
		jsonError(w, http.StatusUnauthorized, "Portal认证失败")
		return
	}

	// Create or update local user
	username := portalResp.Data.Username
	role := portalResp.Data.Role
	if role == "" {
		role = "user"
	}

	var userID int
	err = database.DB.QueryRow("SELECT id FROM users WHERE username = ?", username).Scan(&userID)
	if err != nil {
		result, err := database.DB.Exec("INSERT INTO users (username, display_name, role, status) VALUES (?, ?, ?, 1)",
			username, username, role)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "创建用户失败")
			return
		}
		id, _ := result.LastInsertId()
		userID = int(id)
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

	jsonSuccess(w, map[string]interface{}{
		"token": token,
		"user": map[string]interface{}{
			"id":       userID,
			"username": username,
			"role":     role,
		},
	})
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

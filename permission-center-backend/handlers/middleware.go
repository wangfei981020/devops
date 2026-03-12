package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"permission-center-backend/config"
	"permission-center-backend/database"
)

type contextKey string

const (
	ContextKeyUserID   contextKey = "userID"
	ContextKeyUsername contextKey = "username"
)

// CORSMiddleware handles CORS
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key, X-Operator")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SSOAuthMiddleware validates tokens: tries local session first, then SSO token
func SSOAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := getBearerToken(r)
		if token == "" {
			jsonError(w, http.StatusUnauthorized, "未登录")
			return
		}

		// Try local session first
		tokenHash := database.HashToken(token)
		userID, username, err := database.ValidateSession(tokenHash)
		if err == nil && userID != "" {
			ctx := context.WithValue(r.Context(), ContextKeyUserID, userID)
			ctx = context.WithValue(ctx, ContextKeyUsername, username)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Fall back to SSO token validation
		userID, username, err = validateSSOToken(token)
		if err != nil {
			log.Printf("[Auth] Token validation failed: %v", err)
			jsonError(w, http.StatusUnauthorized, "认证失败")
			return
		}

		// Auto-sync user to permission center
		database.SyncUser(userID, username, username, "", "sso")

		ctx := context.WithValue(r.Context(), ContextKeyUserID, userID)
		ctx = context.WithValue(ctx, ContextKeyUsername, username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AdminMiddleware checks if user is admin in permission center
func AdminMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := GetUserID(r)
		if userID == "" {
			jsonError(w, http.StatusUnauthorized, "未登录")
			return
		}

		isAdmin, err := database.IsUserAdmin(userID)
		if err != nil || !isAdmin {
			jsonError(w, http.StatusForbidden, "需要管理员权限")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// APIKeyMiddleware validates service API key
func APIKeyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			jsonError(w, http.StatusUnauthorized, "缺少 API Key")
			return
		}

		serviceCode, _, err := database.GetServiceByAPIKey(apiKey)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "无效的 API Key")
			return
		}

		// Store service code in context for downstream handlers
		ctx := context.WithValue(r.Context(), contextKey("serviceCode"), serviceCode)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// LoggingMiddleware logs requests
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[%s] %s %s %v", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}

// validateSSOToken calls SSO /userinfo endpoint to validate Bearer token
func validateSSOToken(token string) (string, string, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", config.SSOUserinfoURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("create request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("SSO request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("SSO returned %d: %s", resp.StatusCode, string(body))
	}

	var userInfo struct {
		Sub               string `json:"sub"`
		PreferredUsername  string `json:"preferred_username"`
		Name              string `json:"name"`
		Email             string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&userInfo); err != nil {
		return "", "", fmt.Errorf("decode userinfo failed: %w", err)
	}

	if userInfo.Sub == "" {
		return "", "", fmt.Errorf("empty user ID from SSO")
	}

	username := userInfo.PreferredUsername
	if username == "" {
		username = userInfo.Name
	}

	return userInfo.Sub, username, nil
}

func getBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

// GetUserID extracts user ID from context
func GetUserID(r *http.Request) string {
	if v := r.Context().Value(ContextKeyUserID); v != nil {
		return v.(string)
	}
	return ""
}

// GetUsername extracts username from context
func GetUsername(r *http.Request) string {
	if v := r.Context().Value(ContextKeyUsername); v != nil {
		return v.(string)
	}
	return ""
}

// GetServiceCode extracts service code from context (set by APIKeyMiddleware)
func GetServiceCode(r *http.Request) string {
	if v := r.Context().Value(contextKey("serviceCode")); v != nil {
		return v.(string)
	}
	return ""
}

// GetClientIP returns real client IP
func GetClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return xri
	}
	parts := strings.Split(r.RemoteAddr, ":")
	if len(parts) > 0 {
		return parts[0]
	}
	return r.RemoteAddr
}

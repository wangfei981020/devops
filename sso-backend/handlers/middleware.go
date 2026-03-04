package handlers

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"sso-backend/database"
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
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SSOAuthMiddleware checks SSO session cookie
func SSOAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := getSessionToken(r)
		if token == "" {
			jsonError(w, http.StatusUnauthorized, "未登录")
			return
		}

		tokenHash := hashToken(token)
		userID, username, err := database.ValidateSession(tokenHash)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "会话已过期")
			return
		}

		ctx := context.WithValue(r.Context(), ContextKeyUserID, userID)
		ctx = context.WithValue(ctx, ContextKeyUsername, username)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// AdminMiddleware checks if user is admin
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

// LoggingMiddleware logs requests
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[%s] %s %s %v", r.Method, r.URL.Path, r.RemoteAddr, time.Since(start))
	})
}

func getSessionToken(r *http.Request) string {
	// From cookie
	cookie, err := r.Cookie("sso_session")
	if err == nil && cookie.Value != "" {
		return cookie.Value
	}
	// From header
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h)
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

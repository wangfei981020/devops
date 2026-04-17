package handlers

import (
	"log"
	"net/http"
	"strings"
	"time"
)

func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[HTTP] %s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

// CORSMiddleware 接受一个或多个允许的 Origin (逗号分隔)。
// 若配置为空则不下发任何 CORS 头 (同源请求, 最安全)。
// 若配置为 "*", 允许所有 (仅用于开发)。
func CORSMiddleware(origins string) func(http.Handler) http.Handler {
	allowed := map[string]bool{}
	wildcard := false
	for _, o := range strings.Split(origins, ",") {
		o = strings.TrimSpace(o)
		if o == "*" {
			wildcard = true
		} else if o != "" {
			allowed[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && (wildcard || allowed[origin]) {
				if wildcard {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				}
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// BodySizeLimitMiddleware caps request body to defaultMax bytes (file upload routes use a larger limit by parsing manually).
func BodySizeLimitMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip multipart upload (versions/upload) — handled by ParseMultipartForm with its own limit.
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
				r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func SecurityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		next.ServeHTTP(w, r)
	})
}

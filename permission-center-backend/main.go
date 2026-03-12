package main

import (
	"log"
	"net/http"
	"time"

	"permission-center-backend/cache"
	"permission-center-backend/config"
	"permission-center-backend/database"
	"permission-center-backend/handlers"
)

func main() {
	log.Println("=== Permission Center Starting ===")

	// Initialize database and Redis
	database.InitDB()
	cache.InitRedis()

	// Setup routes
	mux := http.NewServeMux()

	// Health check (public)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	// --- Auth API (public, no auth) ---
	mux.HandleFunc("/api/auth/login", handlers.HandleLogin)
	mux.HandleFunc("/api/auth/login-settings", handlers.HandleLoginSettings)

	// --- Auth API (requires auth) ---
	mux.Handle("/api/auth/logout", handlers.SSOAuthMiddleware(http.HandlerFunc(handlers.HandleLogout)))
	mux.Handle("/api/auth/change-password", handlers.SSOAuthMiddleware(http.HandlerFunc(handlers.HandleChangePassword)))

	// --- Service-to-service API (API Key auth) ---
	mux.Handle("/api/verify", handlers.APIKeyMiddleware(http.HandlerFunc(handlers.HandleVerify)))
	mux.Handle("/api/check", handlers.APIKeyMiddleware(http.HandlerFunc(handlers.HandleCheck)))
	mux.Handle("/api/sync/user", handlers.APIKeyMiddleware(http.HandlerFunc(handlers.HandleSyncUser)))

	// --- Current user API (SSO token auth) ---
	mux.Handle("/api/my/permissions", handlers.SSOAuthMiddleware(http.HandlerFunc(handlers.HandleMyPermissions)))

	// --- Admin API (SSO token + admin role) ---
	adminAuth := func(h http.Handler) http.Handler {
		return handlers.SSOAuthMiddleware(handlers.AdminMiddleware(h))
	}

	// Users
	mux.Handle("/api/admin/users", adminAuth(http.HandlerFunc(handlers.HandleAdminUsers)))
	mux.Handle("/api/admin/users/search", adminAuth(http.HandlerFunc(handlers.HandleAdminUserSearch)))
	mux.Handle("/api/admin/users/", adminAuth(http.HandlerFunc(handlers.HandleAdminUser)))

	// Roles
	mux.Handle("/api/admin/roles", adminAuth(http.HandlerFunc(handlers.HandleAdminRoles)))
	mux.Handle("/api/admin/roles/", adminAuth(http.HandlerFunc(handlers.HandleAdminRole)))

	// Permissions
	mux.Handle("/api/admin/permissions", adminAuth(http.HandlerFunc(handlers.HandleAdminPermissions)))
	mux.Handle("/api/admin/permissions/", adminAuth(http.HandlerFunc(handlers.HandleAdminPermission)))

	// Services
	mux.Handle("/api/admin/services", adminAuth(http.HandlerFunc(handlers.HandleAdminServices)))
	mux.Handle("/api/admin/services/", adminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Route /api/admin/services/{id}/regen-key to special handler
		if len(r.URL.Path) > len("/api/admin/services/") && r.URL.Path[len(r.URL.Path)-len("regen-key"):] == "regen-key" {
			handlers.HandleAdminServiceRegenKey(w, r)
			return
		}
		handlers.HandleAdminService(w, r)
	})))

	// Audit logs
	mux.Handle("/api/admin/audit-logs", adminAuth(http.HandlerFunc(handlers.HandleAdminAuditLogs)))

	// Settings
	mux.Handle("/api/admin/settings", adminAuth(http.HandlerFunc(handlers.HandleAdminSettings)))

	// Apply global middleware
	handler := handlers.LoggingMiddleware(handlers.CORSMiddleware(mux))

	// Periodic session cleanup
	go func() {
		for {
			time.Sleep(1 * time.Hour)
			database.CleanExpiredSessions()
		}
	}()

	log.Printf("SSO Userinfo URL: %s", config.SSOUserinfoURL)
	log.Printf("Listening on %s", config.Port)
	log.Fatal(http.ListenAndServe(config.Port, handler))
}

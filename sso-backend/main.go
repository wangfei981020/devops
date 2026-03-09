package main

import (
	"io"
	"log"
	"net/http"
	"strings"

	"sso-backend/config"
	"sso-backend/crypto"
	"sso-backend/database"
	"sso-backend/handlers"
	"sso-backend/storage"
)

func main() {
	log.Println("=== SSO Server Starting ===")

	// Initialize RSA keys
	crypto.InitKeys()

	// Initialize database
	database.InitDB()

	// Initialize storage (MinIO) - non-fatal
	if err := storage.Init(); err != nil {
		log.Printf("[Storage] Warning: %v (logo upload disabled)", err)
	}

	// Setup routes
	mux := http.NewServeMux()

	// OIDC standard endpoints (public)
	mux.HandleFunc("/.well-known/openid-configuration", handlers.HandleDiscovery)
	mux.HandleFunc("/jwks", handlers.HandleJWKS)
	mux.HandleFunc("/authorize", handlers.HandleAuthorize)
	mux.HandleFunc("/token", handlers.HandleToken)
	mux.HandleFunc("/userinfo", handlers.HandleUserInfo)

	// SSO login pages (for OIDC flow, server-rendered)
	mux.HandleFunc("/sso/login", handlers.HandleLoginPage)
	mux.HandleFunc("/sso/login/submit", handlers.HandleLoginSubmit)

	// Root path
	mux.HandleFunc("/", handlers.HandleRoot)

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	// API: Public endpoints
	mux.HandleFunc("/api/login", handlers.HandleLogin)
	mux.HandleFunc("/api/logout", handlers.HandleLogout)
	mux.HandleFunc("/api/public/env", handlers.HandlePublicEnv)
	mux.HandleFunc("/api/public/environments", handlers.HandlePublicEnvironments)

	// API: Session check (needs auth)
	mux.Handle("/api/session", handlers.SSOAuthMiddleware(http.HandlerFunc(handlers.HandleSessionCheck)))

	// API: Portal (needs auth)
	mux.Handle("/api/portal/services", handlers.SSOAuthMiddleware(http.HandlerFunc(handlers.HandlePortalServices)))
	mux.Handle("/api/portal/profile", handlers.SSOAuthMiddleware(http.HandlerFunc(handlers.HandlePortalProfile)))

	// API: Admin (needs auth + admin)
	adminAuth := func(h http.HandlerFunc) http.Handler {
		return handlers.SSOAuthMiddleware(handlers.AdminMiddleware(http.HandlerFunc(h)))
	}
	mux.Handle("/api/admin/users", adminAuth(handlers.HandleAdminUsers))
	mux.Handle("/api/admin/users/", adminAuth(handlers.HandleAdminUsers))
	mux.Handle("/api/admin/clients", adminAuth(handlers.HandleAdminClients))
	mux.Handle("/api/admin/clients/", adminAuth(handlers.HandleAdminClients))
	mux.Handle("/api/admin/roles", adminAuth(handlers.HandleAdminListRoles))
	mux.Handle("/api/admin/audit-logs", adminAuth(handlers.HandleAdminAuditLogs))
	mux.Handle("/api/admin/upload", adminAuth(handlers.HandleUpload))
	mux.Handle("/api/admin/environments", adminAuth(handlers.HandleAdminEnvironments))
	mux.Handle("/api/admin/environments/", adminAuth(handlers.HandleAdminEnvironments))

	// Storage proxy (serve MinIO files)
	mux.HandleFunc("/storage/", func(w http.ResponseWriter, r *http.Request) {
		if !storage.IsReady() {
			http.NotFound(w, r)
			return
		}
		// /storage/sso-logos/logos/xxx.png → objectName = logos/xxx.png
		path := strings.TrimPrefix(r.URL.Path, "/storage/")
		parts := strings.SplitN(path, "/", 2)
		if len(parts) < 2 {
			http.NotFound(w, r)
			return
		}
		objectName := parts[1]
		obj, err := storage.GetStorage().GetObject(r.Context(), objectName)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer obj.Close()
		info, _ := obj.Stat()
		if info.ContentType != "" {
			w.Header().Set("Content-Type", info.ContentType)
		}
		w.Header().Set("Cache-Control", "public, max-age=3600")
		io.Copy(w, obj)
	})

	// Apply global middleware
	handler := handlers.LoggingMiddleware(handlers.CORSMiddleware(mux))

	log.Printf("Issuer (external): %s", config.Issuer)
	if config.InternalIssuer != "" {
		log.Printf("Issuer (internal): %s", config.InternalIssuer)
	}
	log.Printf("Listening on %s", config.Port)
	log.Fatal(http.ListenAndServe(config.Port, handler))
}

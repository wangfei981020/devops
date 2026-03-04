package main

import (
	"log"
	"net/http"

	"sso-backend/config"
	"sso-backend/crypto"
	"sso-backend/database"
	"sso-backend/handlers"
)

func main() {
	log.Println("=== SSO Server Starting ===")

	// Initialize RSA keys
	crypto.InitKeys()

	// Initialize database
	database.InitDB()

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

	// API: Login/Logout (public)
	mux.HandleFunc("/api/login", handlers.HandleLogin)
	mux.HandleFunc("/api/logout", handlers.HandleLogout)

	// API: Session check (needs auth)
	mux.Handle("/api/session", handlers.SSOAuthMiddleware(http.HandlerFunc(handlers.HandleSessionCheck)))

	// API: Portal (needs auth)
	mux.Handle("/api/portal/services", handlers.SSOAuthMiddleware(http.HandlerFunc(handlers.HandlePortalServices)))
	mux.Handle("/api/portal/profile", handlers.SSOAuthMiddleware(http.HandlerFunc(handlers.HandlePortalProfile)))

	// Apply global middleware
	handler := handlers.LoggingMiddleware(handlers.CORSMiddleware(mux))

	log.Printf("Issuer (external): %s", config.Issuer)
	if config.InternalIssuer != "" {
		log.Printf("Issuer (internal): %s", config.InternalIssuer)
	}
	log.Printf("Listening on %s", config.Port)
	log.Fatal(http.ListenAndServe(config.Port, handler))
}

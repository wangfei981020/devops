package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"opsplatform-alert-backend/alert"
	"opsplatform-alert-backend/config"
	"opsplatform-alert-backend/database"
	"opsplatform-alert-backend/handlers"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting Alert Platform Backend...")

	// Load config
	cfg := config.Load()
	handlers.SetConfig(cfg)

	// Init MySQL
	if err := database.InitMySQL(cfg); err != nil {
		log.Fatalf("Failed to init MySQL: %v", err)
	}

	// Init Redis
	if err := database.InitRedis(cfg); err != nil {
		log.Fatalf("Failed to init Redis: %v", err)
	}

	// Init Prometheus metrics
	alert.InitMetrics()

	// Init global query concurrency limiter
	alert.InitGlobalSemaphore(cfg.GlobalMaxConcurrency)

	// Init alert engine
	engine := alert.NewEngine()
	handlers.SetAlertEngine(engine)
	handlers.SetRuleEngine(engine)

	if err := engine.Start(); err != nil {
		log.Fatalf("Failed to start alert engine: %v", err)
	}

	// Setup router
	r := mux.NewRouter()

	// Global middleware
	r.Use(handlers.CORSMiddleware(cfg.CORSOrigin))
	r.Use(handlers.SecurityMiddleware)
	r.Use(handlers.LoggingMiddleware)

	api := r.PathPrefix("/api").Subrouter()

	// Public routes
	api.HandleFunc("/login", handlers.HandleLogin).Methods("POST", "OPTIONS")
	api.HandleFunc("/logout", handlers.HandleLogout).Methods("POST", "OPTIONS")
	api.HandleFunc("/portal-auth", handlers.HandlePortalAuth).Methods("POST", "OPTIONS")

	// Protected routes
	protected := api.PathPrefix("").Subrouter()
	protected.Use(handlers.AuthMiddleware)

	// User info
	protected.HandleFunc("/users/me", handlers.HandleGetCurrentUser).Methods("GET")

	// Dashboard stats
	protected.HandleFunc("/stats", handlers.HandleGetAlertStats).Methods("GET")

	// ES Connections
	protected.HandleFunc("/es-connections", handlers.HandleListESConnections).Methods("GET")
	protected.HandleFunc("/es-connections", handlers.HandleCreateESConnection).Methods("POST")
	protected.HandleFunc("/es-connections/{id}", handlers.HandleUpdateESConnection).Methods("PUT")
	protected.HandleFunc("/es-connections/{id}", handlers.HandleDeleteESConnection).Methods("DELETE")
	protected.HandleFunc("/es-connections/{id}/toggle", handlers.HandleToggleESConnection).Methods("PUT")
	protected.HandleFunc("/es-connections/test", handlers.HandleTestESConnection).Methods("POST")

	// Lark Configs
	protected.HandleFunc("/lark-configs", handlers.HandleListLarkConfigs).Methods("GET")
	protected.HandleFunc("/lark-configs", handlers.HandleCreateLarkConfig).Methods("POST")
	protected.HandleFunc("/lark-configs/{id}", handlers.HandleUpdateLarkConfig).Methods("PUT")
	protected.HandleFunc("/lark-configs/{id}", handlers.HandleDeleteLarkConfig).Methods("DELETE")
	protected.HandleFunc("/lark-configs/{id}/toggle", handlers.HandleToggleLarkConfig).Methods("PUT")
	protected.HandleFunc("/lark-configs/test", handlers.HandleTestLarkConfig).Methods("POST")

	// Loki Connections
	protected.HandleFunc("/loki-connections", handlers.HandleListLokiConnections).Methods("GET")
	protected.HandleFunc("/loki-connections", handlers.HandleCreateLokiConnection).Methods("POST")
	protected.HandleFunc("/loki-connections/{id}", handlers.HandleUpdateLokiConnection).Methods("PUT")
	protected.HandleFunc("/loki-connections/{id}", handlers.HandleDeleteLokiConnection).Methods("DELETE")
	protected.HandleFunc("/loki-connections/{id}/toggle", handlers.HandleToggleLokiConnection).Methods("PUT")
	protected.HandleFunc("/loki-connections/test", handlers.HandleTestLokiConnection).Methods("POST")
	protected.HandleFunc("/loki-labels", handlers.HandleLokiLabels).Methods("GET")
	protected.HandleFunc("/loki-label-values", handlers.HandleLokiLabelValues).Methods("GET")

	// Alert Rules
	protected.HandleFunc("/alert-rules", handlers.HandleListAlertRules).Methods("GET")
	protected.HandleFunc("/alert-rules", handlers.HandleCreateAlertRule).Methods("POST")
	protected.HandleFunc("/alert-rules/{id}", handlers.HandleGetAlertRule).Methods("GET")
	protected.HandleFunc("/alert-rules/{id}", handlers.HandleUpdateAlertRule).Methods("PUT")
	protected.HandleFunc("/alert-rules/{id}", handlers.HandleDeleteAlertRule).Methods("DELETE")
	protected.HandleFunc("/alert-rules/{id}/toggle", handlers.HandleToggleAlertRule).Methods("PUT")
	protected.HandleFunc("/alert-rules/{id}/run", handlers.HandleRunAlertRule).Methods("POST")
	protected.HandleFunc("/alert-rules/preview", handlers.HandlePreviewAlertRule).Methods("POST")
	protected.HandleFunc("/alert-rules/test-send", handlers.HandleTestSendAlertRule).Methods("POST")

	// ES Explore
	protected.HandleFunc("/es-explore", handlers.HandleESExplore).Methods("POST")
	protected.HandleFunc("/es-explore/indices", handlers.HandleESIndices).Methods("GET")
	protected.HandleFunc("/loki-explore", handlers.HandleLokiExplore).Methods("POST")

	// Permission refresh
	protected.HandleFunc("/refresh-permissions", handlers.HandleRefreshPermissions).Methods("GET")

	// Users (admin only)
	admin := protected.PathPrefix("").Subrouter()
	admin.Use(handlers.AdminMiddleware)
	admin.HandleFunc("/users", handlers.HandleListUsers).Methods("GET")
	admin.HandleFunc("/users", handlers.HandleCreateUser).Methods("POST")
	admin.HandleFunc("/users/{id}", handlers.HandleUpdateUser).Methods("PUT")
	admin.HandleFunc("/users/{id}/toggle", handlers.HandleToggleUser).Methods("PUT")
	admin.HandleFunc("/users/{id}/reset-password", handlers.HandleResetPassword).Methods("POST")
	admin.HandleFunc("/users/{id}", handlers.HandleDeleteUser).Methods("DELETE")

	// Alert Contacts
	protected.HandleFunc("/alert-contacts", handlers.HandleListContacts).Methods("GET")
	protected.HandleFunc("/alert-contacts", handlers.HandleCreateContact).Methods("POST")
	protected.HandleFunc("/alert-contacts/batch", handlers.HandleBatchCreateContacts).Methods("POST")
	protected.HandleFunc("/alert-contacts/{id}", handlers.HandleUpdateContact).Methods("PUT")
	protected.HandleFunc("/alert-contacts/{id}", handlers.HandleDeleteContact).Methods("DELETE")

	// Alert Mutes
	protected.HandleFunc("/alert-mutes", handlers.HandleListMutes).Methods("GET")
	protected.HandleFunc("/alert-mutes", handlers.HandleCreateMute).Methods("POST")
	protected.HandleFunc("/alert-mutes/{id}", handlers.HandleDeleteMute).Methods("DELETE")

	// Audit Logs
	protected.HandleFunc("/audit-logs", handlers.HandleListAuditLogs).Methods("GET")

	// Alert Logs
	protected.HandleFunc("/alert-logs", handlers.HandleListAlertLogs).Methods("GET")
	protected.HandleFunc("/alert-logs/clean", handlers.HandleCleanAlertLogs).Methods("DELETE")

	// Health check + Prometheus metrics server (separate port)
	go func() {
		healthMux := http.NewServeMux()
		healthMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"ok","service":"alert-backend"}`))
		})
		healthMux.Handle("/metrics", promhttp.Handler())
		log.Printf("Health/Metrics server on %s (/health, /metrics)", cfg.HealthPort)
		http.ListenAndServe(cfg.HealthPort, healthMux)
	}()

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		engine.Stop()
		os.Exit(0)
	}()

	// Start main server
	log.Printf("Alert backend server started on %s", cfg.Port)
	if err := http.ListenAndServe(cfg.Port, r); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

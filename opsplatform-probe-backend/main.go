package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"opsplatform-probe-backend/config"
	"opsplatform-probe-backend/database"
	"opsplatform-probe-backend/handlers"
	"opsplatform-probe-backend/services"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting opsplatform-probe-backend...")

	cfg := config.Load()
	handlers.SetConfig(cfg) // 含 JWT_SECRET 校验, 默认值会 log.Fatal

	// 生产环境强制检查关键密钥 (fix #6 MinIO 凭证)
	if cfg.MinIOEndpoint != "" {
		if cfg.MinIOAccessKey == "" || cfg.MinIOSecretKey == "" {
			log.Fatal("[SECURITY] MINIO_ACCESS_KEY / MINIO_SECRET_KEY 未配置, 拒绝启动")
		}
	}

	if err := database.InitMySQL(cfg); err != nil {
		log.Fatalf("init mysql: %v", err)
	}
	if err := services.InitMinIO(cfg); err != nil {
		log.Printf("init minio (continuing without): %v", err)
	}

	handlers.InitRateLimiters()
	handlers.StartTaskExpirer()

	r := mux.NewRouter()
	r.Use(handlers.CORSMiddleware(cfg.CORSOrigin))
	r.Use(handlers.SecurityMiddleware)
	r.Use(handlers.LoggingMiddleware)
	// Cap JSON body size to 1 MiB globally; multipart uploads bypassed inside the middleware (fix #8)
	r.Use(handlers.BodySizeLimitMiddleware(1 << 20))

	api := r.PathPrefix("/api").Subrouter()

	// Public auth
	api.HandleFunc("/login", handlers.HandleLogin).Methods("POST", "OPTIONS")
	api.HandleFunc("/logout", handlers.HandleLogout).Methods("POST", "OPTIONS")
	api.HandleFunc("/portal-auth", handlers.HandlePortalAuth).Methods("POST", "OPTIONS")

	// ====== Agent endpoints ======
	// Register: open (Agent doesn't have token yet on first call)
	api.HandleFunc("/agent/register", handlers.HandleAgentRegister).Methods("POST", "OPTIONS")

	// Authenticated agent endpoints
	agentRouter := api.PathPrefix("/agent").Subrouter()
	agentRouter.Use(handlers.AgentTokenMiddleware)
	agentRouter.HandleFunc("/heartbeat", handlers.HandleAgentHeartbeat).Methods("POST")
	agentRouter.HandleFunc("/tasks", handlers.HandleAgentPullTasks).Methods("GET")
	agentRouter.HandleFunc("/probe-result", handlers.HandleAgentReportResult).Methods("POST")
	agentRouter.HandleFunc("/upgrade-status", handlers.HandleAgentUpgradeStatus).Methods("POST")
	agentRouter.HandleFunc("/upgrade/download", handlers.HandleAgentUpgradeDownload).Methods("GET")

	// ====== Authenticated user endpoints ======
	protected := api.PathPrefix("").Subrouter()
	protected.Use(handlers.AuthMiddleware)

	protected.HandleFunc("/users/me", handlers.HandleGetCurrentUser).Methods("GET")
	protected.HandleFunc("/refresh-permissions", handlers.HandleRefreshPermissions).Methods("GET")
	protected.HandleFunc("/dashboard", handlers.HandleDashboard).Methods("GET")

	// Agents (admin)
	protected.HandleFunc("/agents", handlers.HandleListAgents).Methods("GET")
	protected.HandleFunc("/agents/{id}/approve", handlers.HandleApproveAgent).Methods("POST")
	protected.HandleFunc("/agents/{id}/offline", handlers.HandleOfflineAgent).Methods("POST")
	protected.HandleFunc("/agents/{id}/reissue-token", handlers.HandleReissueAgentToken).Methods("POST")
	protected.HandleFunc("/agents/{id}", handlers.HandleUpdateAgent).Methods("PUT")
	protected.HandleFunc("/agents/{id}", handlers.HandleDeleteAgent).Methods("DELETE")

	// Groups
	protected.HandleFunc("/agent-groups", handlers.HandleListGroups).Methods("GET")
	protected.HandleFunc("/agent-groups", handlers.HandleCreateGroup).Methods("POST")
	protected.HandleFunc("/agent-groups/{id}", handlers.HandleUpdateGroup).Methods("PUT")
	protected.HandleFunc("/agent-groups/{id}", handlers.HandleDeleteGroup).Methods("DELETE")

	// Targets
	protected.HandleFunc("/targets", handlers.HandleListTargets).Methods("GET")
	protected.HandleFunc("/targets", handlers.HandleCreateTarget).Methods("POST")
	protected.HandleFunc("/targets/batch-import", handlers.HandleBatchImportTargets).Methods("POST")
	protected.HandleFunc("/targets/{id}", handlers.HandleUpdateTarget).Methods("PUT")
	protected.HandleFunc("/targets/{id}", handlers.HandleDeleteTarget).Methods("DELETE")
	protected.HandleFunc("/targets/{id}/agents", handlers.HandleListTargetAgents).Methods("GET")
	protected.HandleFunc("/targets/{id}/eligible-agents", handlers.HandleGetEligibleAgents).Methods("GET")

	// Probe
	protected.HandleFunc("/probe", handlers.HandleManualProbe).Methods("POST")
	protected.HandleFunc("/probe/result", handlers.HandleGetBatchResult).Methods("GET")
	protected.HandleFunc("/probe/results", handlers.HandleListResults).Methods("GET")
	protected.HandleFunc("/probe/results/clean", handlers.HandleCleanResults).Methods("POST")

	// Versions
	protected.HandleFunc("/versions", handlers.HandleListVersions).Methods("GET")
	protected.HandleFunc("/versions/upload", handlers.HandleUploadVersion).Methods("POST")
	protected.HandleFunc("/versions/import-from-image", handlers.HandleImportVersionFromImage).Methods("POST")
	protected.HandleFunc("/versions/{id}", handlers.HandleDeleteVersion).Methods("DELETE")

	// Upgrades
	protected.HandleFunc("/upgrades", handlers.HandleDispatchUpgrade).Methods("POST")
	protected.HandleFunc("/upgrades", handlers.HandleListUpgradeTasks).Methods("GET")

	// Audit
	protected.HandleFunc("/audit-logs", handlers.HandleListAuditLogs).Methods("GET")

	// Users (admin only)
	admin := protected.PathPrefix("").Subrouter()
	admin.Use(handlers.AdminMiddleware)
	admin.HandleFunc("/users", handlers.HandleListUsers).Methods("GET")
	admin.HandleFunc("/users", handlers.HandleCreateUser).Methods("POST")
	admin.HandleFunc("/users/{id}", handlers.HandleUpdateUser).Methods("PUT")
	admin.HandleFunc("/users/{id}/toggle", handlers.HandleToggleUser).Methods("PUT")
	admin.HandleFunc("/users/{id}/reset-password", handlers.HandleResetPassword).Methods("POST")
	admin.HandleFunc("/users/{id}", handlers.HandleDeleteUser).Methods("DELETE")
	admin.HandleFunc("/audit-logs/verify", handlers.HandleVerifyAuditChain).Methods("GET")

	// Health
	go func() {
		hm := http.NewServeMux()
		hm.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"ok","service":"probe-backend"}`))
		})
		log.Printf("Health server on %s", cfg.HealthPort)
		http.ListenAndServe(cfg.HealthPort, hm)
	}()

	srv := &http.Server{
		Addr:         cfg.Port,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown (fix #7)
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down gracefully (30s timeout)...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown error: %v", err)
		}
	}()

	log.Printf("Probe backend server listening on %s", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
	log.Println("server exited")
}

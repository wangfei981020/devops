package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/config"
	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/database/migrations"
	"opsplatform-deploy-backend/handlers"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Deploy Center backend starting...")

	cfg := config.Load()
	log.Printf("config: Port=%s HealthPort=%s GitCacheDir=%s", cfg.Port, cfg.HealthPort, cfg.GitCacheDir)
	crypto.Init(cfg.AESKey)

	if err := database.InitMySQL(cfg); err != nil {
		log.Fatalf("init mysql: %v", err)
	}
	defer database.Close()

	if err := migrations.Run(database.DB); err != nil {
		log.Fatalf("run migrations: %v", err)
	}

	go startHealthServer(cfg.HealthPort)

	handlers.SetConfig(cfg)

	r := mux.NewRouter()
	r.Use(handlers.CORSMiddleware(cfg.CORSOrigin))
	r.Use(handlers.RecoverMiddleware)
	r.Use(handlers.LoggingMiddleware)
	api := r.PathPrefix("/api").Subrouter()

	api.HandleFunc("/global-config", handlers.HandleGetGlobalConfig).Methods("GET", "OPTIONS")
	api.HandleFunc("/global-config", handlers.HandleUpdateGlobalConfig).Methods("PUT", "OPTIONS")
	api.HandleFunc("/global-config/test-gitlab", handlers.HandleTestGlobalGitlab).Methods("POST", "OPTIONS")

	api.HandleFunc("/project-envs", handlers.HandleListProjectEnvs).Methods("GET", "OPTIONS")
	api.HandleFunc("/project-envs", handlers.HandleCreateProjectEnv).Methods("POST", "OPTIONS")
	api.HandleFunc("/project-envs/{id}", handlers.HandleGetProjectEnv).Methods("GET", "OPTIONS")
	api.HandleFunc("/project-envs/{id}", handlers.HandleUpdateProjectEnv).Methods("PUT", "OPTIONS")
	api.HandleFunc("/project-envs/{id}", handlers.HandleDeleteProjectEnv).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/project-envs/{id}/test-git", handlers.HandleTestProjectEnvGit).Methods("POST", "OPTIONS")
	api.HandleFunc("/project-envs/{id}/test-argocd", handlers.HandleTestProjectEnvArgocd).Methods("POST", "OPTIONS")
	api.HandleFunc("/project-envs/{id}/scan-modules", handlers.HandleScanModules).Methods("POST", "OPTIONS")

	log.Printf("API listening on %s", cfg.Port)
	server := &http.Server{Addr: cfg.Port, Handler: r}
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("api server: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("shutting down...")
}

func startHealthServer(addr string) {
	m := http.NewServeMux()
	m.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"deploy-backend"}`))
	})
	m.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := database.DB.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"db unreachable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	log.Printf("health listening on %s", addr)
	if err := http.ListenAndServe(addr, m); err != nil {
		log.Printf("health server: %v", err)
	}
}

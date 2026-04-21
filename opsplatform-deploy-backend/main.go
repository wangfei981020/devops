package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

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
	go startScanScheduler(cfg.Port)

	handlers.SetConfig(cfg)

	r := mux.NewRouter()
	r.Use(handlers.CORSMiddleware(cfg.CORSOrigin))
	r.Use(handlers.RecoverMiddleware)
	r.Use(handlers.LoggingMiddleware)
	api := r.PathPrefix("/api").Subrouter()

	api.HandleFunc("/global-config", handlers.HandleGetGlobalConfig).Methods("GET", "OPTIONS")
	api.HandleFunc("/global-config", handlers.HandleUpdateGlobalConfig).Methods("PUT", "OPTIONS")
	api.HandleFunc("/global-config/test-gitlab", handlers.HandleTestGlobalGitlab).Methods("POST", "OPTIONS")

	api.HandleFunc("/projects", handlers.HandleListProjects).Methods("GET", "OPTIONS")
	api.HandleFunc("/projects", handlers.HandleCreateProject).Methods("POST", "OPTIONS")
	api.HandleFunc("/projects/{id}", handlers.HandleUpdateProject).Methods("PUT", "OPTIONS")
	api.HandleFunc("/projects/{id}", handlers.HandleDeleteProject).Methods("DELETE", "OPTIONS")

	api.HandleFunc("/accounts", handlers.HandleListAccounts).Methods("GET", "OPTIONS")
	api.HandleFunc("/accounts", handlers.HandleCreateAccount).Methods("POST", "OPTIONS")
	api.HandleFunc("/accounts/{id}", handlers.HandleUpdateAccount).Methods("PUT", "OPTIONS")
	api.HandleFunc("/accounts/{id}", handlers.HandleDeleteAccount).Methods("DELETE", "OPTIONS")

	api.HandleFunc("/argocd-instances", handlers.HandleListArgocdInstances).Methods("GET", "OPTIONS")
	api.HandleFunc("/argocd-instances", handlers.HandleCreateArgocdInstance).Methods("POST", "OPTIONS")
	api.HandleFunc("/argocd-instances/{id}", handlers.HandleUpdateArgocdInstance).Methods("PUT", "OPTIONS")
	api.HandleFunc("/argocd-instances/{id}", handlers.HandleDeleteArgocdInstance).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/argocd-instances/{id}/test", handlers.HandleTestArgocdInstance).Methods("POST", "OPTIONS")

	api.HandleFunc("/project-envs", handlers.HandleListProjectEnvs).Methods("GET", "OPTIONS")
	api.HandleFunc("/project-envs", handlers.HandleCreateProjectEnv).Methods("POST", "OPTIONS")
	api.HandleFunc("/project-envs/{id}", handlers.HandleGetProjectEnv).Methods("GET", "OPTIONS")
	api.HandleFunc("/project-envs/{id}", handlers.HandleUpdateProjectEnv).Methods("PUT", "OPTIONS")
	api.HandleFunc("/project-envs/{id}", handlers.HandleDeleteProjectEnv).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/project-envs/{id}/test-git", handlers.HandleTestProjectEnvGit).Methods("POST", "OPTIONS")
	api.HandleFunc("/project-envs/{id}/test-argocd", handlers.HandleTestProjectEnvArgocd).Methods("POST", "OPTIONS")
	api.HandleFunc("/project-envs/{id}/scan-modules", handlers.HandleScanModules).Methods("POST", "OPTIONS")

	api.HandleFunc("/modules", handlers.HandleListModules).Methods("GET", "OPTIONS")
	api.HandleFunc("/modules/{id}/tag-history", handlers.HandleModuleTagHistory).Methods("GET", "OPTIONS")

	api.HandleFunc("/deployments", handlers.HandleListDeployments).Methods("GET", "OPTIONS")
	api.HandleFunc("/deployments/{id}", handlers.HandleGetDeployment).Methods("GET", "OPTIONS")
	api.HandleFunc("/deployments/{id}/rollback-preview", handlers.HandleRollbackPreview).Methods("GET", "OPTIONS")

	api.HandleFunc("/deploy/preview-image", handlers.HandlePreviewImage).Methods("POST", "OPTIONS")
	api.HandleFunc("/deploy/update-image", handlers.HandleUpdateImage).Methods("POST", "OPTIONS")
	api.HandleFunc("/deploy/restart", handlers.HandleRestart).Methods("POST", "OPTIONS")
	api.HandleFunc("/deploy/rollback", handlers.HandleRollback).Methods("POST", "OPTIONS")

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

// startScanScheduler 每 5 分钟对所有 project_env 触发一次 scan-modules（静默失败）
// 通过 HTTP 自调用触发，避免 handlers 和 main 产生循环依赖
func startScanScheduler(apiAddr string) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rows, err := database.DB.Query(`SELECT id FROM project_env`)
		if err != nil {
			log.Printf("scheduler: list project_env err: %v", err)
			continue
		}
		var ids []int64
		for rows.Next() {
			var id int64
			_ = rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		for _, id := range ids {
			url := "http://localhost" + apiAddr + "/api/project-envs/" + strconv.FormatInt(id, 10) + "/scan-modules"
			resp, err := http.Post(url, "application/json", nil)
			if err != nil {
				log.Printf("scheduler: scan id=%d err: %v", id, err)
				continue
			}
			resp.Body.Close()
		}
	}
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

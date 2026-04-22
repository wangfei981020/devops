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
	warnIfDefaultAdmin()

	go startHealthServer(cfg.HealthPort)
	go startScanScheduler()

	handlers.SetConfig(cfg)
	handlers.StartSessionCleaner()

	r := mux.NewRouter()
	r.Use(handlers.CORSMiddleware(cfg.CORSOrigin))
	r.Use(handlers.RecoverMiddleware)
	r.Use(handlers.LoggingMiddleware)
	api := r.PathPrefix("/api").Subrouter()

	// Public
	api.Handle("/login", handlers.LoginRateLimit(http.HandlerFunc(handlers.HandleLogin))).Methods("POST", "OPTIONS")
	api.HandleFunc("/logout", handlers.HandleLogout).Methods("POST", "OPTIONS")
	api.Handle("/portal-auth", handlers.LoginRateLimit(http.HandlerFunc(handlers.HandlePortalAuth))).Methods("POST", "OPTIONS")

	// Protected
	protected := api.PathPrefix("").Subrouter()
	protected.Use(handlers.AuthMiddleware)

	// ========== Protected：登录后可用（只读 + UAT 发布等低危操作）==========
	protected.HandleFunc("/users/me", handlers.HandleGetCurrentUser).Methods("GET")
	protected.HandleFunc("/refresh-permissions", handlers.HandleRefreshPermissions).Methods("GET")
	protected.HandleFunc("/dashboard/stats", handlers.HandleDashboardStats).Methods("GET", "OPTIONS")

	// 全局/项目/环境/ArgoCD/Lark/通知人 —— 所有用户可 GET
	protected.HandleFunc("/global-config", handlers.HandleGetGlobalConfig).Methods("GET", "OPTIONS")
	protected.HandleFunc("/projects", handlers.HandleListProjects).Methods("GET", "OPTIONS")
	protected.HandleFunc("/project-envs", handlers.HandleListProjectEnvs).Methods("GET", "OPTIONS")
	protected.HandleFunc("/project-envs/{id}", handlers.HandleGetProjectEnv).Methods("GET", "OPTIONS")
	protected.HandleFunc("/contacts", handlers.HandleListContacts).Methods("GET", "OPTIONS")
	protected.HandleFunc("/lark-bots", handlers.HandleListLarkBots).Methods("GET", "OPTIONS")
	protected.HandleFunc("/argocd-instances", handlers.HandleListArgocdInstances).Methods("GET", "OPTIONS")

	// 模块 / 发布历史 —— 只读
	protected.HandleFunc("/modules", handlers.HandleListModules).Methods("GET", "OPTIONS")
	protected.HandleFunc("/modules/{id}/tag-history", handlers.HandleModuleTagHistory).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deployments", handlers.HandleListDeployments).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deployments/{id}", handlers.HandleGetDeployment).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deployments/{id}/rollback-preview", handlers.HandleRollbackPreview).Methods("GET", "OPTIONS")

	// 发布动作（UAT handler 内自行放行 / PROD handler 内要求 admin）
	protected.HandleFunc("/deploy/preview-image", handlers.HandlePreviewImage).Methods("POST", "OPTIONS")
	protected.HandleFunc("/deploy/update-image", handlers.HandleUpdateImage).Methods("POST", "OPTIONS")
	protected.HandleFunc("/deploy/restart", handlers.HandleRestart).Methods("POST", "OPTIONS")
	protected.HandleFunc("/deploy/rollback", handlers.HandleRollback).Methods("POST", "OPTIONS")

	// ========== Admin-only：所有管理类 CRUD、测试接口、PROD 扫描等 ==========
	admin := protected.PathPrefix("").Subrouter()
	admin.Use(handlers.AdminMiddleware)

	// 全局配置
	admin.HandleFunc("/global-config", handlers.HandleUpdateGlobalConfig).Methods("PUT", "OPTIONS")
	admin.Handle("/global-config/test-gitlab", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestGlobalGitlab))).Methods("POST", "OPTIONS")

	// 项目 CRUD
	admin.HandleFunc("/projects", handlers.HandleCreateProject).Methods("POST", "OPTIONS")
	admin.HandleFunc("/projects/{id}", handlers.HandleUpdateProject).Methods("PUT", "OPTIONS")
	admin.HandleFunc("/projects/{id}", handlers.HandleDeleteProject).Methods("DELETE", "OPTIONS")

	// 项目环境 CRUD + 测试 + 扫描
	admin.HandleFunc("/project-envs", handlers.HandleCreateProjectEnv).Methods("POST", "OPTIONS")
	admin.HandleFunc("/project-envs/{id}", handlers.HandleUpdateProjectEnv).Methods("PUT", "OPTIONS")
	admin.HandleFunc("/project-envs/{id}", handlers.HandleDeleteProjectEnv).Methods("DELETE", "OPTIONS")
	admin.Handle("/project-envs/{id}/test-git", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestProjectEnvGit))).Methods("POST", "OPTIONS")
	admin.Handle("/project-envs/{id}/test-argocd", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestProjectEnvArgocd))).Methods("POST", "OPTIONS")
	admin.Handle("/project-envs/{id}/scan-modules", handlers.ScanRateLimit(http.HandlerFunc(handlers.HandleScanModules))).Methods("POST", "OPTIONS")

	// 通知人 CRUD
	admin.HandleFunc("/contacts", handlers.HandleCreateContact).Methods("POST", "OPTIONS")
	admin.HandleFunc("/contacts/{id}", handlers.HandleUpdateContact).Methods("PUT", "OPTIONS")
	admin.HandleFunc("/contacts/{id}", handlers.HandleDeleteContact).Methods("DELETE", "OPTIONS")

	// Lark 机器人 CRUD + 测试
	admin.HandleFunc("/lark-bots", handlers.HandleCreateLarkBot).Methods("POST", "OPTIONS")
	admin.HandleFunc("/lark-bots/{id}", handlers.HandleUpdateLarkBot).Methods("PUT", "OPTIONS")
	admin.HandleFunc("/lark-bots/{id}", handlers.HandleDeleteLarkBot).Methods("DELETE", "OPTIONS")
	admin.Handle("/lark-bots/{id}/test", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestLarkBot))).Methods("POST", "OPTIONS")

	// ArgoCD 实例 CRUD + 测试
	admin.HandleFunc("/argocd-instances", handlers.HandleCreateArgocdInstance).Methods("POST", "OPTIONS")
	admin.HandleFunc("/argocd-instances/{id}", handlers.HandleUpdateArgocdInstance).Methods("PUT", "OPTIONS")
	admin.HandleFunc("/argocd-instances/{id}", handlers.HandleDeleteArgocdInstance).Methods("DELETE", "OPTIONS")
	admin.Handle("/argocd-instances/{id}/test", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestArgocdInstance))).Methods("POST", "OPTIONS")

	// 用户管理
	admin.HandleFunc("/users", handlers.HandleListUsers).Methods("GET", "OPTIONS")
	admin.HandleFunc("/users", handlers.HandleCreateUser).Methods("POST", "OPTIONS")
	admin.HandleFunc("/users/{id}", handlers.HandleUpdateUser).Methods("PUT", "OPTIONS")
	admin.HandleFunc("/users/{id}/toggle", handlers.HandleToggleUser).Methods("PUT", "OPTIONS")
	admin.HandleFunc("/users/{id}/reset-password", handlers.HandleResetPassword).Methods("POST", "OPTIONS")
	admin.HandleFunc("/users/{id}", handlers.HandleDeleteUser).Methods("DELETE", "OPTIONS")

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

// startScanScheduler 每 5 分钟对所有 project_env 直接调用内部扫描函数（静默失败）
func startScanScheduler() {
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
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			if _, err := handlers.ScanModulesByProjectEnvID(ctx, id); err != nil {
				log.Printf("scheduler: scan id=%d err: %v", id, err)
			}
			cancel()
		}
	}
}

// warnIfDefaultAdmin: 检查默认 admin 是否还是初始 bcrypt hash，是则在 log 里提醒改密码
func warnIfDefaultAdmin() {
	const initialHash = "$2a$10$vXhq5Vju4qCuhXhbGNjvyOqrEkXxTkkzyOokD0jKV5d8bjMOpgNQ6"
	var hash string
	err := database.DB.QueryRow(`SELECT password_hash FROM users WHERE username='admin' AND auth_source='local'`).Scan(&hash)
	if err == nil && hash == initialHash {
		log.Println("⚠️  默认 admin 账号仍使用初始密码 admin123，请立即通过 UI「系统设置→用户管理」修改")
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

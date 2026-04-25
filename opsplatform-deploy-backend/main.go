package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	cleanupZombiePending()

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

	// 系统间调用（运维平台管理页拉 env 列表用）：X-Internal-Token header 校验，不走用户 JWT
	api.Handle("/public/project-envs",
		handlers.InternalTokenMiddleware(http.HandlerFunc(handlers.HandlePublicListProjectEnvs))).
		Methods("GET", "OPTIONS")
	api.Handle("/public/projects",
		handlers.InternalTokenMiddleware(http.HandlerFunc(handlers.HandlePublicListProjects))).
		Methods("GET", "OPTIONS")

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
	protected.HandleFunc("/gitlab-repos", handlers.HandleListGitlabRepos).Methods("GET", "OPTIONS")

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

	// ========== 按钮权限分组：每组 admin 自动放行，portal 用户按勾选授权 ==========
	// ① manage_global — 全局凭证 + GitLab 仓库登记
	mg := protected.PathPrefix("").Subrouter()
	mg.Use(handlers.RequireButton("manage_global"))
	mg.HandleFunc("/global-config", handlers.HandleUpdateGlobalConfig).Methods("PUT", "OPTIONS")
	mg.Handle("/global-config/test-gitlab", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestGlobalGitlab))).Methods("POST", "OPTIONS")
	mg.HandleFunc("/gitlab-repos", handlers.HandleCreateGitlabRepo).Methods("POST", "OPTIONS")
	mg.HandleFunc("/gitlab-repos/{id}", handlers.HandleUpdateGitlabRepo).Methods("PUT", "OPTIONS")
	mg.HandleFunc("/gitlab-repos/{id}", handlers.HandleDeleteGitlabRepo).Methods("DELETE", "OPTIONS")

	// ② manage_projects — 项目 / 环境 CRUD + 测试连接
	mp := protected.PathPrefix("").Subrouter()
	mp.Use(handlers.RequireButton("manage_projects"))
	mp.HandleFunc("/projects", handlers.HandleCreateProject).Methods("POST", "OPTIONS")
	mp.HandleFunc("/projects/{id}", handlers.HandleUpdateProject).Methods("PUT", "OPTIONS")
	mp.HandleFunc("/projects/{id}", handlers.HandleDeleteProject).Methods("DELETE", "OPTIONS")
	mp.HandleFunc("/project-envs", handlers.HandleCreateProjectEnv).Methods("POST", "OPTIONS")
	mp.HandleFunc("/project-envs/{id}", handlers.HandleUpdateProjectEnv).Methods("PUT", "OPTIONS")
	mp.HandleFunc("/project-envs/{id}", handlers.HandleDeleteProjectEnv).Methods("DELETE", "OPTIONS")
	mp.Handle("/project-envs/{id}/test-git", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestProjectEnvGit))).Methods("POST", "OPTIONS")
	mp.Handle("/project-envs/{id}/test-argocd", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestProjectEnvArgocd))).Methods("POST", "OPTIONS")
	mp.Handle("/project-envs/test-git", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestProjectEnvGitByBody))).Methods("POST", "OPTIONS")
	mp.Handle("/project-envs/test-argocd", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestProjectEnvArgocdByBody))).Methods("POST", "OPTIONS")

	// ③ scan_modules — 单独按钮，让只想看模块状态的人也能勾
	sc := protected.PathPrefix("").Subrouter()
	sc.Use(handlers.RequireButton("scan_modules"))
	sc.Handle("/project-envs/{id}/scan-modules", handlers.ScanRateLimit(http.HandlerFunc(handlers.HandleScanModules))).Methods("POST", "OPTIONS")

	// ④ manage_contacts — 通知人 CRUD
	mc := protected.PathPrefix("").Subrouter()
	mc.Use(handlers.RequireButton("manage_contacts"))
	mc.HandleFunc("/contacts", handlers.HandleCreateContact).Methods("POST", "OPTIONS")
	mc.HandleFunc("/contacts/{id}", handlers.HandleUpdateContact).Methods("PUT", "OPTIONS")
	mc.HandleFunc("/contacts/{id}", handlers.HandleDeleteContact).Methods("DELETE", "OPTIONS")

	// ⑤ manage_lark_bots — Lark 机器人 CRUD
	ml := protected.PathPrefix("").Subrouter()
	ml.Use(handlers.RequireButton("manage_lark_bots"))
	ml.HandleFunc("/lark-bots", handlers.HandleCreateLarkBot).Methods("POST", "OPTIONS")
	ml.HandleFunc("/lark-bots/{id}", handlers.HandleUpdateLarkBot).Methods("PUT", "OPTIONS")
	ml.HandleFunc("/lark-bots/{id}", handlers.HandleDeleteLarkBot).Methods("DELETE", "OPTIONS")
	ml.Handle("/lark-bots/{id}/test", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestLarkBot))).Methods("POST", "OPTIONS")
	ml.Handle("/lark-bots/test", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestLarkBotByBody))).Methods("POST", "OPTIONS")

	// ⑥ manage_argocd — ArgoCD 实例 CRUD
	ma := protected.PathPrefix("").Subrouter()
	ma.Use(handlers.RequireButton("manage_argocd"))
	ma.HandleFunc("/argocd-instances", handlers.HandleCreateArgocdInstance).Methods("POST", "OPTIONS")
	ma.HandleFunc("/argocd-instances/{id}", handlers.HandleUpdateArgocdInstance).Methods("PUT", "OPTIONS")
	ma.HandleFunc("/argocd-instances/{id}", handlers.HandleDeleteArgocdInstance).Methods("DELETE", "OPTIONS")
	ma.Handle("/argocd-instances/{id}/test", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestArgocdInstance))).Methods("POST", "OPTIONS")
	ma.Handle("/argocd-instances/test", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestArgocdInstanceByBody))).Methods("POST", "OPTIONS")

	// ========== 仍然严格 admin-only：平台用户管理、审计日志 ==========
	// 这两个不属于发布中心按钮语义，保留 admin-only
	admin := protected.PathPrefix("").Subrouter()
	admin.Use(handlers.AdminMiddleware)
	admin.HandleFunc("/audit-logs", handlers.HandleListAuditLogs).Methods("GET", "OPTIONS")
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
	log.Println("SIGTERM received, draining + waiting for in-flight deploy jobs...")

	// 收到 SIGTERM 时再次保险设 drain，preStop 也应已设过
	// 等所有 in-flight 异步发布任务跑完。preStop 已经轮询过 inflight=0 才放行 SIGTERM，
	// 此处兜底（避免 preStop 没正确配上时直接掐断 in-flight）。
	handlers.MarkDraining()
	doneCh := make(chan struct{})
	go func() { handlers.WaitInflight(); close(doneCh) }()
	select {
	case <-doneCh:
		log.Println("all in-flight jobs done")
	case <-time.After(540 * time.Second):
		log.Printf("⚠ timed out waiting in-flight jobs (still %d running), forcing shutdown", handlers.InflightCount())
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = server.Shutdown(shutCtx)
	log.Println("api server stopped, bye")
}

// cleanupZombiePending：启动时把上次崩溃/重启遗留的 pending 任务标记成 unknown。
//
//	判定条件：status='pending' AND created_at < NOW()-10min。正常发布几十秒最多几分钟，
//	超过 10 分钟还 pending 一定是上次进程异常退出留下的"僵尸"。
//	已经 push 的 git commit 物理存在，ArgoCD 也会继续同步；只是 deployment 表里需要标个状态。
func cleanupZombiePending() {
	res, err := database.DB.Exec(`
		UPDATE deployment
		   SET status='unknown',
		       error_msg=CONCAT(IFNULL(error_msg,''),
		                ' [auto] backend restarted while job was pending; check git/argocd manually')
		 WHERE status='pending'
		   AND created_at < DATE_SUB(NOW(), INTERVAL 10 MINUTE)`)
	if err != nil {
		log.Printf("⚠ cleanupZombiePending: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("⚠ cleaned up %d zombie pending deployments (backend was restarted while they were running)", n)
	}
}

// startScanScheduler 每 5 分钟对所有 project_env 直接调用内部扫描函数（静默失败）
func startScanScheduler() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		rows, err := database.DB.Query(`SELECT id FROM project_env ORDER BY id LIMIT 500`)
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

// requireLocalhost：只允许来自 pod 自身 (127.0.0.1 / ::1) 的请求通过，
// 集群内其他 pod 通过 Service IP 访问会被 403。用于 /internal/* 这类
// 只打算给 preStop hook 调用的内部接口。
//
// 注意：health server 不在 Service 端口 8080 上，所以 /metrics /health /ready
// 无法被集群内其他 pod 通过 Service 访问；但 8088 直接暴露在 pod IP 上，
// 集群内可通过 pod IP:8088 访问。这层校验防的就是这种路径。
func requireLocalhost(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		if host != "127.0.0.1" && host != "::1" {
			http.Error(w, "forbidden: localhost only", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

func startHealthServer(addr string) {
	m := http.NewServeMux()
	m.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","service":"deploy-backend"}`))
	})
	m.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		// drain 期间 readiness 主动失败，让 Service 提前摘 endpoint，新请求不再路由到本 pod
		if handlers.IsDraining() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"draining"}`))
			return
		}
		if err := database.DB.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"db unreachable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	// Prometheus 抓取 /metrics（和生产 Helm values.yaml 里的 prometheus.io/port=8088 对齐）
	m.Handle("/metrics", promhttp.Handler())
	// 滚动升级配套 endpoint：仅 pod 自己 preStop hook (localhost) 可调，
	// 防止集群内其他 pod 通过 Service IP 触发 drain 造成 DoS
	m.HandleFunc("/internal/drain", requireLocalhost(handlers.HandleInternalDrain))
	m.HandleFunc("/internal/inflight", requireLocalhost(handlers.HandleInternalInflight))
	log.Printf("Health/Metrics server on %s (/health, /ready, /metrics)", addr)
	if err := http.ListenAndServe(addr, m); err != nil {
		log.Printf("health server: %v", err)
	}
}

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
	"opsplatform-deploy-backend/services"
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
	// 清掉上次崩溃遗留的隔离工作区临时目录
	services.CleanWorkRoot(cfg.GitCacheDir)
	// 并发闸：限制同时进行的 git/helm 重活数，防 100 并发发布/新增时 OOM
	services.InitHeavyGate(cfg.DeployConcurrency)
	log.Printf("并发闸已启用：同时最多 %d 个 git/helm 重活（DEPLOY_CONCURRENCY）", cfg.DeployConcurrency)
	// 启动对账：收拾上次进程重启时遗留的孤儿 pending 发布（放后台，ArgoCD 查询可能慢，别挡启动）
	go handlers.ReconcileOrphanedDeploys("startup")
	// 开机预热 git 缓存：覆盖"全新环境/空卷第一次"冷启动（正常重启缓存在 PVC 上不丢）
	go handlers.WarmAllEnvCaches()

	go startHealthServer(cfg.HealthPort)
	go startScanScheduler()
	go startReconcileScheduler()

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
	// 注：/global-config GET 移到 mg 子路由（manage_global），因为响应里包含 minio AK / lark webhook /
	// 各家 endpoint URL 等敏感信息。前端 SystemSettings 页面本来就是 manage_global 限定。
	protected.HandleFunc("/projects", handlers.HandleListProjects).Methods("GET", "OPTIONS")
	protected.HandleFunc("/project-envs", handlers.HandleListProjectEnvs).Methods("GET", "OPTIONS")
	protected.HandleFunc("/project-envs/{id}", handlers.HandleGetProjectEnv).Methods("GET", "OPTIONS")
	protected.HandleFunc("/contacts", handlers.HandleListContacts).Methods("GET", "OPTIONS")
	protected.HandleFunc("/lark-bots", handlers.HandleListLarkBots).Methods("GET", "OPTIONS")
	protected.HandleFunc("/argocd-instances", handlers.HandleListArgocdInstances).Methods("GET", "OPTIONS")
	protected.HandleFunc("/gitlab-repos", handlers.HandleListGitlabRepos).Methods("GET", "OPTIONS")
	// Harbor: 拉某模块的最近 tag 列表 (任何登录用户能查)
	protected.HandleFunc("/harbor/tags", handlers.HandleListHarborTags).Methods("GET", "OPTIONS")
	// ArgoCD App 缓存手动刷新（前端 [刷新] 按钮）
	protected.HandleFunc("/argocd-app-cache/refresh", handlers.HandleRefreshArgocdAppCache).Methods("POST", "OPTIONS")

	// 模块 / 发布历史 —— 只读
	protected.HandleFunc("/modules", handlers.HandleListModules).Methods("GET", "OPTIONS")
	protected.HandleFunc("/modules/{id}/tag-history", handlers.HandleModuleTagHistory).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deployments", handlers.HandleListDeployments).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deployments/{id}", handlers.HandleGetDeployment).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deployments/{id}/rollback-preview", handlers.HandleRollbackPreview).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deployments/{id}/pods", handlers.HandleGetDeploymentPods).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deployments/{id}/pod-logs", handlers.HandleGetDeploymentPodLogs).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deployments/{id}/pod-events", handlers.HandleGetDeploymentPodEvents).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deployments/{id}/archived-pods", handlers.HandleGetDeploymentArchivedPods).Methods("GET", "OPTIONS")
	// 注：/global-config/test-minio 也移到 mg（同 test-gitlab/test-harbor），任何登录用户能触发出站请求
	// + 用 DB AK 探测任意 bucket 是 SSRF 面，必须 manage_global 限定。

	// 发布动作（UAT handler 内自行放行 / PROD handler 内要求 admin）
	protected.HandleFunc("/deploy/preview-image", handlers.HandlePreviewImage).Methods("POST", "OPTIONS")
	protected.HandleFunc("/deploy/update-image", handlers.HandleUpdateImage).Methods("POST", "OPTIONS")
	protected.HandleFunc("/deploy/restart", handlers.HandleRestart).Methods("POST", "OPTIONS")
	protected.HandleFunc("/deploy/rollback", handlers.HandleRollback).Methods("POST", "OPTIONS")
	// 取消等待：handler 内部判定权限（操作发起人 OR admin）
	protected.HandleFunc("/deployments/{id}/cancel", handlers.HandleCancelDeployment).Methods("POST", "OPTIONS")
	protected.HandleFunc("/deployments/{id}/retry", handlers.HandleRetryDeployment).Methods("POST", "OPTIONS")

	// ========== 服务编排：模板库 + 新增模块 ==========
	// 读 + 预填 + 预览（登录即可）；提交在 handler 内按环境权限 submit_<env> 放行
	protected.HandleFunc("/orchestration/templates", handlers.HandleListTemplates).Methods("GET", "OPTIONS")
	protected.HandleFunc("/orchestration/prefill", handlers.HandlePrefillModule).Methods("POST", "OPTIONS")
	protected.HandleFunc("/orchestration/derive", handlers.HandleDeriveModules).Methods("POST", "OPTIONS")
	protected.HandleFunc("/orchestration/preview", handlers.HandlePreviewModule).Methods("POST", "OPTIONS")
	protected.HandleFunc("/orchestration/submit", handlers.HandleSubmitModule).Methods("POST", "OPTIONS")
	protected.HandleFunc("/orchestration/batch-preview", handlers.HandleBatchPreview).Methods("POST", "OPTIONS")
	protected.HandleFunc("/orchestration/batch-submit", handlers.HandleBatchSubmit).Methods("POST", "OPTIONS")
	protected.HandleFunc("/orchestration/tasks", handlers.HandleListOrchTasks).Methods("GET", "OPTIONS")
	protected.HandleFunc("/orchestration/tasks/{id}/retry", handlers.HandleRetryOrchTask).Methods("POST", "OPTIONS")
	protected.HandleFunc("/orchestration/tasks/{id}/pods", handlers.HandleGetOrchTaskPods).Methods("GET", "OPTIONS")
	protected.HandleFunc("/orchestration/tasks/{id}/pod-logs", handlers.HandleGetOrchTaskPodLogs).Methods("GET", "OPTIONS")

	// 模板 CRUD —— manage_templates 权限
	mt := protected.PathPrefix("").Subrouter()
	mt.Use(handlers.RequireButton("manage_templates"))
	mt.HandleFunc("/orchestration/templates", handlers.HandleCreateTemplate).Methods("POST", "OPTIONS")
	mt.HandleFunc("/orchestration/templates/{id}", handlers.HandleUpdateTemplate).Methods("PUT", "OPTIONS")
	mt.HandleFunc("/orchestration/templates/{id}", handlers.HandleDeleteTemplate).Methods("DELETE", "OPTIONS")

	// 环境列表（读）+ 环境 CRUD（manage_projects 权限）
	protected.HandleFunc("/environments", handlers.HandleListEnvironments).Methods("GET", "OPTIONS")
	me := protected.PathPrefix("").Subrouter()
	me.Use(handlers.RequireButton("manage_projects"))
	me.HandleFunc("/environments", handlers.HandleCreateEnvironment).Methods("POST", "OPTIONS")
	me.HandleFunc("/environments/{name}", handlers.HandleUpdateEnvironment).Methods("PUT", "OPTIONS")
	me.HandleFunc("/environments/{name}", handlers.HandleDeleteEnvironment).Methods("DELETE", "OPTIONS")
	// 项目参数：更新某环境的 ingress 网关名 / Harbor 项目
	me.HandleFunc("/orchestration/env-gateway/{id}", handlers.HandleUpdateEnvGateway).Methods("PUT", "OPTIONS")
	me.HandleFunc("/orchestration/env-harbor/{id}", handlers.HandleUpdateEnvHarbor).Methods("PUT", "OPTIONS")
	me.HandleFunc("/orchestration/env-domain/{id}", handlers.HandleUpdateEnvDomain).Methods("PUT", "OPTIONS")
	me.HandleFunc("/orchestration/env-namespaces/{id}", handlers.HandleUpdateEnvNamespaces).Methods("PUT", "OPTIONS")
	me.HandleFunc("/orchestration/env-zkv-path/{id}", handlers.HandleUpdateEnvZkvPath).Methods("PUT", "OPTIONS")
	// z-kv-secrets 初始化（新项目从 zkv 模板复制一份）
	me.HandleFunc("/orchestration/zkv-status/{id}", handlers.HandleZkvStatus).Methods("GET", "OPTIONS")
	me.HandleFunc("/orchestration/zkv-preview", handlers.HandleZkvPreview).Methods("POST", "OPTIONS")
	me.HandleFunc("/orchestration/zkv-init", handlers.HandleInitZkv).Methods("POST", "OPTIONS")

	// ========== 按钮权限分组：每组 admin 自动放行，portal 用户按勾选授权 ==========
	// ① manage_global — 全局凭证 + GitLab 仓库登记
	mg := protected.PathPrefix("").Subrouter()
	mg.Use(handlers.RequireButton("manage_global"))
	mg.HandleFunc("/global-config", handlers.HandleGetGlobalConfig).Methods("GET", "OPTIONS")
	mg.HandleFunc("/global-config", handlers.HandleUpdateGlobalConfig).Methods("PUT", "OPTIONS")
	mg.Handle("/global-config/test-gitlab", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestGlobalGitlab))).Methods("POST", "OPTIONS")
	mg.Handle("/global-config/test-harbor", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestHarbor))).Methods("POST", "OPTIONS")
	mg.Handle("/global-config/test-minio", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestMinIO))).Methods("POST", "OPTIONS")
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

	// ⑦ VM 部署 — Deploy Agents + VM 项目环境 + VM 服务发现 + 版本代理 + 部署触发
	//   读接口 protected 即可；写接口跟 K8s 一样按 button 权限
	protected.HandleFunc("/deploy-agents", handlers.HandleListDeployAgents).Methods("GET", "OPTIONS")
	protected.HandleFunc("/vm-project-envs", handlers.HandleListVmProjectEnvs).Methods("GET", "OPTIONS")
	protected.HandleFunc("/vm-project-envs/{id}", handlers.HandleGetVmProjectEnv).Methods("GET", "OPTIONS")
	protected.HandleFunc("/vm-project-envs/{id}/services", handlers.HandleListVmServices).Methods("GET", "OPTIONS")
	protected.HandleFunc("/vm-services/{id}/versions", handlers.HandleListVmServiceVersions).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deploy/vm-run", handlers.HandleVmDeploy).Methods("POST", "OPTIONS")
	protected.HandleFunc("/deployments/{id}/vm-logs", handlers.HandleVmDeployLogs).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deployments/{id}/vm-archived-log", handlers.HandleVmArchivedLog).Methods("GET", "OPTIONS")
	protected.HandleFunc("/deployments/{id}/vm-cancel", handlers.HandleVmDeployCancel).Methods("POST", "OPTIONS")

	// VM 写接口（仅 manage_argocd 权限放行；后续如需独立 manage_vm 权限再分）
	ma.HandleFunc("/deploy-agents", handlers.HandleCreateDeployAgent).Methods("POST", "OPTIONS")
	ma.HandleFunc("/deploy-agents/{id}", handlers.HandleUpdateDeployAgent).Methods("PUT", "OPTIONS")
	ma.HandleFunc("/deploy-agents/{id}", handlers.HandleDeleteDeployAgent).Methods("DELETE", "OPTIONS")
	ma.Handle("/deploy-agents/test", handlers.TestRateLimit(http.HandlerFunc(handlers.HandleTestDeployAgent))).Methods("POST", "OPTIONS")

	mp.HandleFunc("/vm-project-envs", handlers.HandleCreateVmProjectEnv).Methods("POST", "OPTIONS")
	mp.HandleFunc("/vm-project-envs/{id}", handlers.HandleUpdateVmProjectEnv).Methods("PUT", "OPTIONS")
	mp.HandleFunc("/vm-project-envs/{id}", handlers.HandleDeleteVmProjectEnv).Methods("DELETE", "OPTIONS")

	sc.Handle("/vm-project-envs/{id}/scan-services", handlers.ScanRateLimit(http.HandlerFunc(handlers.HandleScanVmServices))).Methods("POST", "OPTIONS")

	// ========== 仍然严格 admin-only：平台用户管理、审计日志 ==========
	// 这两个不属于发布中心按钮语义，保留 admin-only
	admin := protected.PathPrefix("").Subrouter()
	admin.Use(handlers.AdminMiddleware)
	admin.HandleFunc("/audit-logs", handlers.HandleListAuditLogs).Methods("GET", "OPTIONS")
	admin.HandleFunc("/audit-logs/action-types", handlers.HandleListAuditActionTypes).Methods("GET", "OPTIONS")
	admin.HandleFunc("/users", handlers.HandleListUsers).Methods("GET", "OPTIONS")
	admin.HandleFunc("/users", handlers.HandleCreateUser).Methods("POST", "OPTIONS")
	admin.HandleFunc("/users/{id}", handlers.HandleUpdateUser).Methods("PUT", "OPTIONS")
	admin.HandleFunc("/users/{id}/toggle", handlers.HandleToggleUser).Methods("PUT", "OPTIONS")
	admin.HandleFunc("/users/{id}/reset-password", handlers.HandleResetPassword).Methods("POST", "OPTIONS")
	admin.HandleFunc("/users/{id}", handlers.HandleDeleteUser).Methods("DELETE", "OPTIONS")
	admin.HandleFunc("/history-cleanup/preview", handlers.HandlePreviewHistoryCleanup).Methods("GET", "OPTIONS")
	admin.HandleFunc("/history-cleanup", handlers.HandleRunHistoryCleanup).Methods("POST", "OPTIONS")

	// 后台定时任务：发布历史自动清理（每天 02:00）+ 模块锁过期 sweep（每 5 分钟）
	handlers.StartHistoryCleanupCron()
	handlers.StartLockSweeper()

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

// startReconcileScheduler 每 2 分钟对账一次孤儿 pending 发布。
//
//	启动对账只在进程刚起来时跑一次；这里的定时兜底捡漏——比如某个跟踪 goroutine
//	因异常退出（非重启）留下的 pending，或启动时 ArgoCD 短暂不可达没对上的。
//	只收拾超过 reconcileGrace 且当前进程没在跟的记录，不会误伤正常在跑的长发布。
func startReconcileScheduler() {
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		handlers.ReconcileOrphanedDeploys("periodic")
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

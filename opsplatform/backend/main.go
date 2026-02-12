package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"opsplatform/database"
	"opsplatform/handlers"

	"github.com/gorilla/mux"
)

func main() {
	// 初始化数据库
	if err := database.InitDB(); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	defer database.Close()

	// 初始化默认管理员
	if err := handlers.InitDefaultAdmin(); err != nil {
		log.Printf("创建默认管理员失败: %v", err)
	}

	// 创建路由
	r := mux.NewRouter()

	// CORS 中间件
	r.Use(corsMiddleware)

	// 安全响应头中间件
	r.Use(handlers.SecurityHeadersMiddleware)

	// API 速率限制中间件
	r.Use(handlers.RateLimitMiddleware)

	// 请求体大小限制中间件（防止 DoS）
	r.Use(limitRequestBodyMiddleware)

	// 审计日志中间件（记录请求开始时间）
	r.Use(auditMiddleware)

	// API 路由
	api := r.PathPrefix("/api").Subrouter()

	// ===== 公开路由（无需认证）=====
	api.HandleFunc("/login", handlers.HandleLogin).Methods("POST", "OPTIONS")
	api.HandleFunc("/logout", handlers.HandleLogout).Methods("POST", "OPTIONS")
	api.HandleFunc("/csrf-token", handlers.HandleGetCSRFToken).Methods("GET", "OPTIONS")

	// MFA 多因素认证（登录流程）
	api.HandleFunc("/mfa/setup", handlers.HandleMFASetup).Methods("POST", "OPTIONS")
	api.HandleFunc("/mfa/bind", handlers.HandleMFABind).Methods("POST", "OPTIONS")
	api.HandleFunc("/mfa/verify", handlers.HandleMFAVerify).Methods("POST", "OPTIONS")

	// Logo 配置（公开接口）
	api.HandleFunc("/logo-config", handlers.HandleGetLogoConfig).Methods("GET", "OPTIONS")

	// ===== 需要认证的路由 =====
	protected := api.PathPrefix("").Subrouter()
	protected.Use(handlers.AuthMiddleware)

	// 会话管理
	protected.HandleFunc("/session/info", handlers.HandleGetSessionInfo).Methods("GET", "OPTIONS")
	protected.HandleFunc("/session/refresh", handlers.HandleRefreshSession).Methods("POST", "OPTIONS")

	// ===== 仅管理员可访问的路由 =====
	adminOnly := api.PathPrefix("").Subrouter()
	adminOnly.Use(handlers.AuthMiddleware)
	adminOnly.Use(handlers.AdminOnlyMiddleware)

	// 用户管理（仅管理员）
	protected.HandleFunc("/users", handlers.HandleGetUsers).Methods("GET", "OPTIONS")
	adminOnly.HandleFunc("/users", handlers.HandleCreateUser).Methods("POST", "OPTIONS")
	adminOnly.HandleFunc("/users/{id}", handlers.HandleUpdateUser).Methods("PUT", "OPTIONS")
	adminOnly.HandleFunc("/users/{id}", handlers.HandleDeleteUser).Methods("DELETE", "OPTIONS")
	// adminOnly.HandleFunc("/users/{id}/password", handlers.HandleChangePassword).Methods("PUT", "OPTIONS") // TODO: implement

	// MFA 管理（仅管理员可重置他人）
	protected.HandleFunc("/mfa/disable", handlers.HandleMFADisable).Methods("POST", "OPTIONS")
	adminOnly.HandleFunc("/mfa/reset", handlers.HandleMFAReset).Methods("POST", "OPTIONS")

	// 系统设置（仅管理员）
	protected.HandleFunc("/settings", handlers.HandleGetSettings).Methods("GET", "OPTIONS")
	adminOnly.HandleFunc("/settings", handlers.HandleUpdateSettings).Methods("POST", "OPTIONS")

	// 安全设置（仅管理员）
	adminOnly.HandleFunc("/security-settings", handlers.HandleGetSecuritySettings).Methods("GET", "OPTIONS")
	adminOnly.HandleFunc("/security-settings", handlers.HandleUpdateSecuritySettings).Methods("POST", "OPTIONS")

	// 记录管理
	protected.HandleFunc("/records/export", handlers.HandleExportRecords).Methods("GET", "OPTIONS")
	protected.HandleFunc("/records/query", handlers.HandleQueryRecords).Methods("GET", "OPTIONS")
	// protected.HandleFunc("/records/grouped", handlers.HandleGetGroupedRecords).Methods("GET", "OPTIONS") // TODO: implement
	// protected.HandleFunc("/records/search-by-connid", handlers.HandleSearchByConnectionID).Methods("GET", "OPTIONS") // TODO: implement
	protected.HandleFunc("/records/batch", handlers.HandleBatchAddRecords).Methods("POST", "OPTIONS")
	protected.HandleFunc("/records/batch-check", handlers.HandleBatchCheckRecords).Methods("POST", "OPTIONS")
	protected.HandleFunc("/records/batch-delete", handlers.HandleBatchDeleteRecords).Methods("POST", "OPTIONS")
	protected.HandleFunc("/records/batch-status", handlers.HandleBatchUpdateStatus).Methods("POST", "OPTIONS")
	// protected.HandleFunc("/records/batch-edit", handlers.HandleBatchEdit).Methods("POST", "OPTIONS") // TODO: implement
	protected.HandleFunc("/records/batch-update", handlers.HandleBatchUpdate).Methods("POST", "OPTIONS")
	protected.HandleFunc("/records", handlers.HandleGetRecords).Methods("GET", "OPTIONS")
	protected.HandleFunc("/records", handlers.HandleAddRecord).Methods("POST", "OPTIONS")
	protected.HandleFunc("/records/{id}", handlers.HandleGetRecord).Methods("GET", "OPTIONS")
	protected.HandleFunc("/records/{id}", handlers.HandleUpdateRecord).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/records/{id}", handlers.HandleDeleteRecord).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/records/{id}/history", handlers.HandleGetRecordHistory).Methods("GET", "OPTIONS")
	protected.HandleFunc("/records/{id}/rollback", handlers.HandleRollbackRecord).Methods("POST", "OPTIONS")

	// 审计日志
	protected.HandleFunc("/audit-logs", handlers.HandleGetAuditLogs).Methods("GET", "OPTIONS")
	protected.HandleFunc("/audit-logs/export", handlers.HandleExportAuditLogs).Methods("GET", "OPTIONS")

	// 数据源管理
	protected.HandleFunc("/datasources", handlers.HandleGetDataSources).Methods("GET", "OPTIONS")
	protected.HandleFunc("/datasources", handlers.HandleAddDataSource).Methods("POST", "OPTIONS")
	protected.HandleFunc("/datasources/test", handlers.HandleTestDataSource).Methods("POST", "OPTIONS")
	protected.HandleFunc("/datasources/{id}", handlers.HandleUpdateDataSource).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/datasources/{id}", handlers.HandleDeleteDataSource).Methods("DELETE", "OPTIONS")

	// 自定义指标管理
	protected.HandleFunc("/metrics", handlers.HandleGetMetrics).Methods("GET", "OPTIONS")
	protected.HandleFunc("/metrics", handlers.HandleCreateMetric).Methods("POST", "OPTIONS")
	protected.HandleFunc("/metrics", handlers.HandleUpdateMetric).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/metrics", handlers.HandleDeleteMetric).Methods("DELETE", "OPTIONS")

	// 排班管理
	protected.HandleFunc("/schedule", handlers.HandleGetSchedule).Methods("GET", "OPTIONS")
	protected.HandleFunc("/schedule", handlers.HandleSaveSchedule).Methods("POST", "OPTIONS")
	protected.HandleFunc("/schedule/employee", handlers.HandleAddEmployee).Methods("POST", "OPTIONS")
	protected.HandleFunc("/schedule/employee", handlers.HandleDeleteEmployee).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/schedule/shift", handlers.HandleUpdateShift).Methods("POST", "OPTIONS")
	protected.HandleFunc("/schedule/config", handlers.HandleGetShiftConfig).Methods("GET", "OPTIONS")
	protected.HandleFunc("/schedule/config", handlers.HandleSaveShiftConfig).Methods("POST", "OPTIONS")
	protected.HandleFunc("/metrics/init", handlers.HandleInitDefaultMetrics).Methods("POST", "OPTIONS")

	// 巡检功能
	protected.HandleFunc("/inspection/execute", handlers.HandleExecuteInspection).Methods("POST", "OPTIONS")

	// 域名管理
	protected.HandleFunc("/domains", handlers.HandleGetDomains).Methods("GET", "OPTIONS")
	protected.HandleFunc("/domains", handlers.HandleAddDomain).Methods("POST", "OPTIONS")
	protected.HandleFunc("/domains/export", handlers.HandleExportDomains).Methods("GET", "OPTIONS")
	protected.HandleFunc("/domains/batch", handlers.HandleBatchDomains).Methods("POST", "OPTIONS")
	protected.HandleFunc("/domains/batch-add", handlers.HandleBatchAddDomains).Methods("POST", "OPTIONS")
	protected.HandleFunc("/domains/check-cert", handlers.HandleCheckCert).Methods("GET", "OPTIONS")
	protected.HandleFunc("/domains/{id}", handlers.HandleUpdateDomain).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/domains/{id}", handlers.HandleDeleteDomain).Methods("DELETE", "OPTIONS")

	// 网站管理
	protected.HandleFunc("/websites", handlers.HandleGetWebsites).Methods("GET", "OPTIONS")
	protected.HandleFunc("/websites", handlers.HandleCreateWebsite).Methods("POST", "OPTIONS")
	protected.HandleFunc("/websites/{id}", handlers.HandleUpdateWebsite).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/websites/{id}", handlers.HandleDeleteWebsite).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/websites/{id}/password", handlers.HandleGetWebsitePassword).Methods("GET", "OPTIONS")
	// 厅方管理
	protected.HandleFunc("/websites/{id}/halls", handlers.HandleGetWebsiteHalls).Methods("GET", "OPTIONS")
	protected.HandleFunc("/websites/{id}/halls", handlers.HandleCreateWebsiteHall).Methods("POST", "OPTIONS")
	protected.HandleFunc("/websites/{id}/halls/{hallId}", handlers.HandleUpdateWebsiteHall).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/websites/{id}/halls/{hallId}", handlers.HandleDeleteWebsiteHall).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/websites/{id}/halls/{hallId}/password", handlers.HandleGetHallPassword).Methods("GET", "OPTIONS")

	// 密码库管理
	protected.HandleFunc("/vault/status", handlers.HandleVaultStatus).Methods("GET", "OPTIONS")
	protected.HandleFunc("/vault/init", handlers.HandleVaultInit).Methods("POST", "OPTIONS")
	protected.HandleFunc("/vault/unlock", handlers.HandleVaultUnlock).Methods("POST", "OPTIONS")
	protected.HandleFunc("/vault/lock", handlers.HandleVaultLock).Methods("POST", "OPTIONS")
	protected.HandleFunc("/vault/reset-password", handlers.HandleVaultResetPassword).Methods("POST", "OPTIONS")
	protected.HandleFunc("/vault/items", handlers.HandleVaultItems).Methods("GET", "POST", "OPTIONS")
	protected.HandleFunc("/vault/items/{id}", handlers.HandleVaultItem).Methods("PUT", "DELETE", "OPTIONS")
	protected.HandleFunc("/vault/folders", handlers.HandleVaultFolders).Methods("GET", "POST", "OPTIONS")
	protected.HandleFunc("/vault/folders/{id}", handlers.HandleVaultFolder).Methods("PUT", "DELETE", "OPTIONS")
	protected.HandleFunc("/vault/generate-password", handlers.HandleVaultGeneratePassword).Methods("GET", "OPTIONS")
	// 密码库用户组和分享
	protected.HandleFunc("/vault/groups", handlers.HandleVaultGroups).Methods("GET", "POST", "OPTIONS")
	protected.HandleFunc("/vault/groups/{id}", handlers.HandleVaultGroup).Methods("PUT", "DELETE", "OPTIONS")
	protected.HandleFunc("/vault/groups/{id}/members", handlers.HandleVaultGroupMembers).Methods("GET", "POST", "OPTIONS")
	protected.HandleFunc("/vault/groups/{id}/members/{memberId}", handlers.HandleVaultGroupMember).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/vault/shares", handlers.HandleVaultShares).Methods("GET", "POST", "OPTIONS")
	protected.HandleFunc("/vault/shares/{id}", handlers.HandleVaultShare).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/vault/users", handlers.HandleVaultUsers).Methods("GET", "OPTIONS")

	// 权限管理 (RBAC)
	protected.HandleFunc("/roles", handlers.HandleRoles).Methods("GET", "POST", "OPTIONS")
	protected.HandleFunc("/roles/{id}", handlers.HandleRole).Methods("GET", "PUT", "DELETE", "OPTIONS")
	protected.HandleFunc("/roles/{id}/permissions", handlers.HandleRolePermissions).Methods("GET", "PUT", "OPTIONS")
	protected.HandleFunc("/permissions", handlers.HandlePermissions).Methods("GET", "POST", "OPTIONS")
	protected.HandleFunc("/permissions/{id}", handlers.HandlePermission).Methods("PUT", "DELETE", "OPTIONS")
	protected.HandleFunc("/users/{id}/roles", handlers.HandleUserRoles).Methods("GET", "PUT", "OPTIONS")
	protected.HandleFunc("/my/permissions", handlers.HandleMyPermissions).Methods("GET", "OPTIONS")

	// 任务池
	protected.HandleFunc("/tasks", handlers.HandleGetTasks).Methods("GET", "OPTIONS")
	protected.HandleFunc("/tasks", handlers.HandleCreateTask).Methods("POST", "OPTIONS")
	protected.HandleFunc("/tasks/batch", handlers.HandleBatchCreateTasks).Methods("POST", "OPTIONS")
	protected.HandleFunc("/tasks/stats", handlers.HandleGetTaskStats).Methods("GET", "OPTIONS")
	protected.HandleFunc("/tasks/projects", handlers.HandleGetTaskProjects).Methods("GET", "OPTIONS")
	protected.HandleFunc("/tasks/export", handlers.HandleExportTasks).Methods("GET", "OPTIONS")
	protected.HandleFunc("/tasks/{id}", handlers.HandleUpdateTask).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/tasks/{id}", handlers.HandleDeleteTask).Methods("DELETE", "OPTIONS")

	// 员工失误记录
	protected.HandleFunc("/incidents", handlers.HandleGetIncidents).Methods("GET", "OPTIONS")
	protected.HandleFunc("/incidents", handlers.HandleCreateIncident).Methods("POST", "OPTIONS")
	protected.HandleFunc("/incidents/stats", handlers.HandleGetIncidentStats).Methods("GET", "OPTIONS")
	protected.HandleFunc("/incidents/export", handlers.HandleExportIncidents).Methods("GET", "OPTIONS")
	protected.HandleFunc("/incidents/{id}", handlers.HandleUpdateIncident).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/incidents/{id}", handlers.HandleDeleteIncident).Methods("DELETE", "OPTIONS")

	// 服务配置管理
	protected.HandleFunc("/service-configs", handlers.HandleGetServiceConfigs).Methods("GET", "OPTIONS")
	protected.HandleFunc("/service-configs", handlers.HandleCreateServiceConfig).Methods("POST", "OPTIONS")
	protected.HandleFunc("/service-configs/batch", handlers.HandleBatchCreateServiceConfigs).Methods("POST", "OPTIONS")
	protected.HandleFunc("/service-configs/projects", handlers.HandleGetServiceProjects).Methods("GET", "OPTIONS")
	protected.HandleFunc("/service-configs/{id}", handlers.HandleGetServiceConfig).Methods("GET", "OPTIONS")
	protected.HandleFunc("/service-configs/{id}", handlers.HandleUpdateServiceConfig).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/service-configs/{id}", handlers.HandleDeleteServiceConfig).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/service-configs/{id}/deps", handlers.HandleGetServiceDependencies).Methods("GET", "OPTIONS")
	protected.HandleFunc("/service-configs/{id}/deps", handlers.HandleCreateServiceDependency).Methods("POST", "OPTIONS")
	protected.HandleFunc("/service-configs/{id}/deps/{depId}", handlers.HandleUpdateServiceDependency).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/service-configs/{id}/deps/{depId}", handlers.HandleDeleteServiceDependency).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/service-configs/{id}/deps/{depId}/password", handlers.HandleGetServiceDepPassword).Methods("GET", "OPTIONS")

	// Kubernetes 管理
	protected.HandleFunc("/k8s/namespaces", handlers.HandleGetNamespaces).Methods("GET", "OPTIONS")
	protected.HandleFunc("/k8s/deployments", handlers.HandleGetDeployments).Methods("GET", "OPTIONS")
	protected.HandleFunc("/k8s/deployments/{name}/yaml", handlers.HandleK8sGetDeploymentYaml).Methods("GET", "OPTIONS")
	protected.HandleFunc("/k8s/deployments/{name}/rollout-status", handlers.HandleK8sRolloutStatus).Methods("GET", "OPTIONS")
	protected.HandleFunc("/k8s/deployments/{name}/rollout-history", handlers.HandleK8sRolloutHistory).Methods("GET", "OPTIONS")
	protected.HandleFunc("/k8s/deployments/{name}/rollback", handlers.HandleK8sRollback).Methods("POST", "OPTIONS")
	protected.HandleFunc("/k8s/services", handlers.HandleGetServices).Methods("GET", "OPTIONS")
	protected.HandleFunc("/k8s/pods", handlers.HandleGetPods).Methods("GET", "OPTIONS")
	protected.HandleFunc("/k8s/pods/{name}/logs", handlers.HandleK8sGetPodLogs).Methods("GET", "OPTIONS")
	protected.HandleFunc("/k8s/pods/{name}", handlers.HandleK8sDeletePod).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/k8s/nodes", handlers.HandleGetNodes).Methods("GET", "OPTIONS")
	protected.HandleFunc("/k8s/apply", handlers.HandleK8sApply).Methods("POST", "OPTIONS")
	protected.HandleFunc("/k8s/restart", handlers.HandleK8sRestartDeployment).Methods("POST", "OPTIONS")
	protected.HandleFunc("/k8s/scale", handlers.HandleK8sScaleDeployment).Methods("POST", "OPTIONS")
	protected.HandleFunc("/k8s/update-image", handlers.HandleK8sUpdateImage).Methods("POST", "OPTIONS")

	// 静态文件 - 从 frontend 目录提供
	r.PathPrefix("/css/").Handler(http.StripPrefix("/css/", http.FileServer(http.Dir("../frontend/css"))))
	r.PathPrefix("/js/").Handler(http.StripPrefix("/js/", http.FileServer(http.Dir("../frontend/js"))))
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("../frontend")))

	// 启动 Prometheus metrics 服务器 (8088)
	go func() {
		metricsPort := os.Getenv("METRICS_PORT")
		if metricsPort == "" {
			metricsPort = "8088"
		}
		metricsMux := http.NewServeMux()
		metricsMux.HandleFunc("/metrics", handlers.HandleMetricsExport)
		metricsMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		})
		log.Printf("Prometheus metrics 服务启动在 http://localhost:%s/metrics", metricsPort)
		if err := http.ListenAndServe(":"+metricsPort, metricsMux); err != nil {
			log.Printf("Metrics 服务启动失败: %v", err)
		}
	}()

	// 启动主应用服务器 (8080)
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("主服务启动在 http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

// 限制请求体大小（10MB）
func limitRequestBodyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024) // 10MB
		next.ServeHTTP(w, r)
	})
}

// 审计日志中间件 - 记录请求开始时间
func auditMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 记录请求开始时间到 context（使用 handlers 包定义的 key）
		ctx := r.Context()
		ctx = context.WithValue(ctx, handlers.StartTimeKey, time.Now())

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 获取允许的源（生产环境应配置具体域名）
		allowedOrigin := os.Getenv("CORS_ALLOWED_ORIGIN")
		if allowedOrigin == "" {
			allowedOrigin = "*" // 开发环境默认允许所有
		}
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Operator, X-CSRF-Token")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// 安全响应头
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

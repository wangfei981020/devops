package main

import (
	"log"
	"net/http"
	"os"

	"jira-center-backend/database"
	"jira-center-backend/handlers"

	"github.com/gorilla/mux"
)

func main() {
	// 初始化数据库
	if err := database.InitDB(); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	defer database.Close()

	// 初始化 Redis（可选）
	if err := database.InitRedis(); err != nil {
		log.Printf("Redis 初始化警告: %v", err)
	}

	// 初始化默认管理员
	if err := handlers.InitDefaultAdmin(); err != nil {
		log.Printf("创建默认管理员失败: %v", err)
	}

	r := mux.NewRouter()

	// 全局中间件
	r.Use(corsMiddleware)
	r.Use(handlers.SecurityHeadersMiddleware)
	r.Use(handlers.LoggingMiddleware)

	api := r.PathPrefix("/api").Subrouter()

	// ===== 公开路由 =====
	api.HandleFunc("/login", handlers.HandleLogin).Methods("POST", "OPTIONS")
	api.HandleFunc("/logout", handlers.HandleLogout).Methods("POST", "OPTIONS")
	api.HandleFunc("/health", handleHealth).Methods("GET")

	// OIDC 单点登录
	api.HandleFunc("/oidc/config", handlers.HandleGetOIDCConfig).Methods("GET", "OPTIONS")
	api.HandleFunc("/oidc/login", handlers.HandleOIDCLogin).Methods("GET", "OPTIONS")
	api.HandleFunc("/oidc/callback", handlers.HandleOIDCCallback).Methods("GET", "OPTIONS")

	// ===== 需要认证的路由 =====
	protected := api.PathPrefix("").Subrouter()
	protected.Use(handlers.AuthMiddleware)

	// 用户
	protected.HandleFunc("/users/me", handlers.HandleGetCurrentUser).Methods("GET", "OPTIONS")
	protected.HandleFunc("/session/refresh", handlers.HandleRefreshSession).Methods("POST", "OPTIONS")

	// Jira 代理
	protected.HandleFunc("/dashboard", handlers.HandleGetDashboardData).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/projects", handlers.HandleGetProjects).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/projects/{key}", handlers.HandleGetProject).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/issues", handlers.HandleSearchIssues).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/issues/{key}", handlers.HandleGetIssue).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/issues/{key}/comments", handlers.HandleGetIssueComments).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/issues/{key}/transitions", handlers.HandleGetTransitions).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/issues/{key}/transitions", handlers.HandleDoTransition).Methods("POST", "OPTIONS")
	protected.HandleFunc("/jira/priorities", handlers.HandleGetPriorities).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/statuses", handlers.HandleGetStatuses).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/users", handlers.HandleSearchUsers).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/stats", handlers.HandleGetStats).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/fields", handlers.HandleGetProjectFields).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/issuetypes", handlers.HandleGetIssueTypes).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/resolutions", handlers.HandleGetResolutions).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/report", handlers.HandleGetReportData).Methods("GET", "OPTIONS")
	protected.HandleFunc("/report-config", handlers.HandleGetReportConfig).Methods("GET", "OPTIONS")
	protected.HandleFunc("/report-config", handlers.HandleSaveReportConfig).Methods("POST", "OPTIONS")

	// ===== 管理员路由 =====
	adminOnly := api.PathPrefix("").Subrouter()
	adminOnly.Use(handlers.AuthMiddleware)
	adminOnly.Use(handlers.AdminOnlyMiddleware)

	// Jira 设置
	adminOnly.HandleFunc("/settings", handlers.HandleGetSettings).Methods("GET", "OPTIONS")
	adminOnly.HandleFunc("/settings", handlers.HandleUpdateSettings).Methods("POST", "OPTIONS")
	adminOnly.HandleFunc("/settings/test-jira", handlers.HandleTestJiraConnection).Methods("POST", "OPTIONS")
	adminOnly.HandleFunc("/jira/proxy", handlers.HandleJiraProxy).Methods("GET", "POST", "OPTIONS")

	// OIDC 配置
	adminOnly.HandleFunc("/oidc/admin-config", handlers.HandleGetOIDCConfigAdmin).Methods("GET", "OPTIONS")
	adminOnly.HandleFunc("/oidc/admin-config", handlers.HandleSaveOIDCConfig).Methods("POST", "OPTIONS")

	// 用户管理
	adminOnly.HandleFunc("/users", handlers.HandleGetUsers).Methods("GET", "OPTIONS")
	adminOnly.HandleFunc("/users", handlers.HandleCreateUser).Methods("POST", "OPTIONS")
	adminOnly.HandleFunc("/users/{id}", handlers.HandleUpdateUser).Methods("PUT", "OPTIONS")
	adminOnly.HandleFunc("/users/{id}", handlers.HandleDeleteUser).Methods("DELETE", "OPTIONS")

	// 审计日志
	adminOnly.HandleFunc("/audit-logs", handlers.HandleGetAuditLogs).Methods("GET", "OPTIONS")

	// 启动服务
	port := os.Getenv("PORT")
	if port == "" {
		port = ":8080"
	}
	log.Printf("Jira Center 服务启动: http://localhost%s", port)
	log.Fatal(http.ListenAndServe(port, r))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","service":"jira-center-backend"}`))
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Operator")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Max-Age", "86400")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

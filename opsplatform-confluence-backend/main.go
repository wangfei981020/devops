package main

import (
	"log"
	"net/http"
	"os"

	"opsplatform-confluence-backend/database"
	"opsplatform-confluence-backend/handlers"

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

	// 迁移旧配置到连接表
	handlers.MigrateSettingsToConnections()

	// 启动截图定时任务
	handlers.InitScreenshotCron()
	defer handlers.StopScreenshotCron()

	r := mux.NewRouter()

	// 全局中间件
	r.Use(corsMiddleware)
	r.Use(handlers.SecurityHeadersMiddleware)
	r.Use(handlers.LoggingMiddleware)

	api := r.PathPrefix("/api").Subrouter()

	// ===== 公开路由 =====
	api.HandleFunc("/login", handlers.HandleLogin).Methods("POST", "OPTIONS")
	api.HandleFunc("/logout", handlers.HandleLogout).Methods("POST", "OPTIONS")
	// Portal 免登认证（运维平台 iframe 接入）
	api.HandleFunc("/portal-auth", handlers.HandlePortalAuth).Methods("POST", "OPTIONS")
	// 截图图片公开访问（飞书消息中的图片链接）
	api.HandleFunc("/screenshot-images/{key}", handlers.HandleGetScreenshotImage).Methods("GET")

	// ===== 需要认证的路由 =====
	protected := api.PathPrefix("").Subrouter()
	protected.Use(handlers.AuthMiddleware)

	// 公共配置（所有登录用户可访问）
	protected.HandleFunc("/settings/public", handlers.HandleGetPublicSettings).Methods("GET", "OPTIONS")
	protected.HandleFunc("/connections/public", handlers.HandleListConnectionsPublic).Methods("GET", "OPTIONS")

	// 用户
	protected.HandleFunc("/users/me", handlers.HandleGetCurrentUser).Methods("GET", "OPTIONS")
	protected.HandleFunc("/session/refresh", handlers.HandleRefreshSession).Methods("POST", "OPTIONS")

	// Confluence 仪表盘
	protected.HandleFunc("/dashboard", handlers.HandleGetDashboardData).Methods("GET", "OPTIONS")

	// 空间 (Spaces)
	protected.HandleFunc("/confluence/spaces", handlers.HandleGetSpaces).Methods("GET", "OPTIONS")
	protected.HandleFunc("/confluence/spaces/{key}", handlers.HandleGetSpace).Methods("GET", "OPTIONS")
	protected.HandleFunc("/confluence/spaces/{key}/content", handlers.HandleGetSpaceContent).Methods("GET", "OPTIONS")

	// 内容 (Content / Pages)
	protected.HandleFunc("/confluence/content", handlers.HandleCreateContent).Methods("POST", "OPTIONS")
	protected.HandleFunc("/confluence/content/{id}", handlers.HandleGetContent).Methods("GET", "OPTIONS")
	protected.HandleFunc("/confluence/content/{id}", handlers.HandleUpdateContent).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/confluence/content/{id}", handlers.HandleDeleteContent).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/confluence/content/{id}/children", handlers.HandleGetContentChildren).Methods("GET", "OPTIONS")

	// 搜索
	protected.HandleFunc("/confluence/search", handlers.HandleSearch).Methods("GET", "OPTIONS")

	// Jira API
	protected.HandleFunc("/jira/projects", handlers.HandleGetJiraProjects).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/search", handlers.HandleJiraSearch).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/issue", handlers.HandleGetJiraIssue).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/issuetypes", handlers.HandleGetJiraIssueTypes).Methods("GET", "OPTIONS")
	protected.HandleFunc("/jira/fields", handlers.HandleGetJiraFields).Methods("GET", "OPTIONS")

	// Grafana API
	protected.HandleFunc("/grafana/folders", handlers.HandleGetGrafanaFolders).Methods("GET", "OPTIONS")
	protected.HandleFunc("/grafana/dashboards", handlers.HandleGetGrafanaDashboards).Methods("GET", "OPTIONS")
	protected.HandleFunc("/grafana/dashboard", handlers.HandleGetGrafanaDashboard).Methods("GET", "OPTIONS")
	protected.HandleFunc("/grafana/render", handlers.HandleGrafanaRenderPanel).Methods("GET", "OPTIONS")
	protected.HandleFunc("/grafana/test", handlers.HandleTestGrafanaConnection).Methods("POST", "OPTIONS")

	// 截图任务
	protected.HandleFunc("/screenshot-tasks", handlers.HandleListScreenshotTasks).Methods("GET", "OPTIONS")
	protected.HandleFunc("/screenshot-tasks", handlers.HandleCreateScreenshotTask).Methods("POST", "OPTIONS")
	protected.HandleFunc("/screenshot-tasks/{id}", handlers.HandleUpdateScreenshotTask).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/screenshot-tasks/{id}", handlers.HandleDeleteScreenshotTask).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/screenshot-tasks/{id}/run", handlers.HandleRunScreenshotTask).Methods("POST", "OPTIONS")
	protected.HandleFunc("/screenshot-tasks/{id}/preview", handlers.HandlePreviewScreenshotTask).Methods("GET", "OPTIONS")
	protected.HandleFunc("/screenshot-tasks/{id}/panels", handlers.HandleGetDashboardPanels).Methods("GET", "OPTIONS")
	protected.HandleFunc("/screenshot-tasks/{id}/render-panel", handlers.HandleRenderSinglePanel).Methods("GET", "OPTIONS")
	protected.HandleFunc("/screenshot-tasks/{id}/test-send", handlers.HandleTestSendScreenshotTask).Methods("POST", "OPTIONS")
	protected.HandleFunc("/screenshot-tasks/{id}/toggle", handlers.HandleToggleScreenshotTask).Methods("PUT", "OPTIONS")

	// Lark 测试
	protected.HandleFunc("/lark/test", handlers.HandleTestLarkConnection).Methods("POST", "OPTIONS")

	// 标签 (Labels)
	protected.HandleFunc("/confluence/content/{id}/labels", handlers.HandleGetContentLabels).Methods("GET", "OPTIONS")
	protected.HandleFunc("/confluence/content/{id}/labels", handlers.HandleAddContentLabel).Methods("POST", "OPTIONS")

	// 评论 (Comments)
	protected.HandleFunc("/confluence/content/{id}/comments", handlers.HandleGetContentComments).Methods("GET", "OPTIONS")

	// 用户搜索
	protected.HandleFunc("/confluence/users", handlers.HandleGetConfluenceUsers).Methods("GET", "OPTIONS")

	// 连接管理（权限由运维平台 confluence:manage_connections 控制）
	protected.HandleFunc("/connections", handlers.HandleListConnections).Methods("GET", "OPTIONS")
	protected.HandleFunc("/connections", handlers.HandleCreateConnection).Methods("POST", "OPTIONS")
	protected.HandleFunc("/connections/{id}", handlers.HandleGetConnection).Methods("GET", "OPTIONS")
	protected.HandleFunc("/connections/{id}", handlers.HandleUpdateConnection).Methods("PUT", "OPTIONS")
	protected.HandleFunc("/connections/{id}", handlers.HandleDeleteConnection).Methods("DELETE", "OPTIONS")
	protected.HandleFunc("/connections/test", handlers.HandleTestConnection).Methods("POST", "OPTIONS")

	// 用户管理和审计日志（权限由运维平台 confluence:manage_settings 控制）
	protected.HandleFunc("/users", handlers.HandleListUsers).Methods("GET", "OPTIONS")
	protected.HandleFunc("/users", handlers.HandleCreateUser).Methods("POST", "OPTIONS")
	protected.HandleFunc("/audit-logs", handlers.HandleGetAuditLogs).Methods("GET", "OPTIONS")

	// 系统设置（权限由运维平台 confluence:manage_settings 控制）
	protected.HandleFunc("/settings", handlers.HandleGetSettings).Methods("GET", "OPTIONS")
	protected.HandleFunc("/settings", handlers.HandleUpdateSettings).Methods("POST", "OPTIONS")
	protected.HandleFunc("/settings/test-confluence", handlers.HandleTestConfluenceConnection).Methods("POST", "OPTIONS")
	protected.HandleFunc("/settings/test-jira", handlers.HandleTestJiraConnection).Methods("POST", "OPTIONS")

	// ===== 管理员路由 =====
	adminOnly := api.PathPrefix("").Subrouter()
	adminOnly.Use(handlers.AuthMiddleware)
	adminOnly.Use(handlers.AdminOnlyMiddleware)

	// 通用 API 代理
	adminOnly.HandleFunc("/confluence/proxy", handlers.HandleConfluenceProxy).Methods("GET", "POST", "PUT", "DELETE", "OPTIONS")
	adminOnly.HandleFunc("/jira/proxy", handlers.HandleJiraProxy).Methods("GET", "POST", "PUT", "DELETE", "OPTIONS")

	// 启动健康检查端口 (8088)
	healthMux := http.NewServeMux()
	healthMux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"opsplatform-confluence-backend"}`))
	})
	go func() {
		log.Println("健康检查服务启动: http://localhost:8088")
		if err := http.ListenAndServe(":8088", healthMux); err != nil {
			log.Printf("健康检查服务启动失败: %v", err)
		}
	}()

	// 启动主服务
	port := os.Getenv("PORT")
	if port == "" {
		port = ":8080"
	}
	log.Printf("Confluence Center 服务启动: http://localhost%s", port)
	log.Fatal(http.ListenAndServe(port, r))
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

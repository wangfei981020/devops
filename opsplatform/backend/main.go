package main

import (
	"log"
	"net/http"
	"os"

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

	// 请求体大小限制中间件（防止 DoS）
	r.Use(limitRequestBodyMiddleware)

	// API 路由
	api := r.PathPrefix("/api").Subrouter()

	// ===== 公开路由（无需认证）=====
	api.HandleFunc("/login", handlers.HandleLogin).Methods("POST", "OPTIONS")
	api.HandleFunc("/csrf-token", handlers.HandleGetCSRFToken).Methods("GET", "OPTIONS")

	// MFA 多因素认证（登录流程）
	api.HandleFunc("/mfa/setup", handlers.HandleMFASetup).Methods("POST", "OPTIONS")
	api.HandleFunc("/mfa/bind", handlers.HandleMFABind).Methods("POST", "OPTIONS")
	api.HandleFunc("/mfa/verify", handlers.HandleMFAVerify).Methods("POST", "OPTIONS")

	// ===== 需要认证的路由 =====
	protected := api.PathPrefix("").Subrouter()
	protected.Use(handlers.AuthMiddleware)

	// ===== 仅管理员可访问的路由 =====
	adminOnly := api.PathPrefix("").Subrouter()
	adminOnly.Use(handlers.AuthMiddleware)
	adminOnly.Use(handlers.AdminOnlyMiddleware)

	// 用户管理（仅管理员）
	protected.HandleFunc("/users", handlers.HandleGetUsers).Methods("GET", "OPTIONS")
	adminOnly.HandleFunc("/users", handlers.HandleCreateUser).Methods("POST", "OPTIONS")
	adminOnly.HandleFunc("/users/{id}", handlers.HandleUpdateUser).Methods("PUT", "OPTIONS")
	adminOnly.HandleFunc("/users/{id}", handlers.HandleDeleteUser).Methods("DELETE", "OPTIONS")

	// MFA 管理（仅管理员可重置他人）
	protected.HandleFunc("/mfa/disable", handlers.HandleMFADisable).Methods("POST", "OPTIONS")
	adminOnly.HandleFunc("/mfa/reset", handlers.HandleMFAReset).Methods("POST", "OPTIONS")

	// 记录管理
	protected.HandleFunc("/records/export", handlers.HandleExportRecords).Methods("GET", "OPTIONS")
	protected.HandleFunc("/records/query", handlers.HandleQueryRecords).Methods("GET", "OPTIONS")
	protected.HandleFunc("/records/batch", handlers.HandleBatchAddRecords).Methods("POST", "OPTIONS")
	protected.HandleFunc("/records/batch-delete", handlers.HandleBatchDeleteRecords).Methods("POST", "OPTIONS")
	protected.HandleFunc("/records/batch-status", handlers.HandleBatchUpdateStatus).Methods("POST", "OPTIONS")
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

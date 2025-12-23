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

	// API 路由
	api := r.PathPrefix("/api").Subrouter()

	// 用户管理
	api.HandleFunc("/login", handlers.HandleLogin).Methods("POST", "OPTIONS")
	api.HandleFunc("/users", handlers.HandleGetUsers).Methods("GET", "OPTIONS")
	api.HandleFunc("/users", handlers.HandleCreateUser).Methods("POST", "OPTIONS")
	api.HandleFunc("/users/{id}", handlers.HandleUpdateUser).Methods("PUT", "OPTIONS")
	api.HandleFunc("/users/{id}", handlers.HandleDeleteUser).Methods("DELETE", "OPTIONS")

	// MFA 多因素认证
	api.HandleFunc("/mfa/setup", handlers.HandleMFASetup).Methods("POST", "OPTIONS")
	api.HandleFunc("/mfa/bind", handlers.HandleMFABind).Methods("POST", "OPTIONS")
	api.HandleFunc("/mfa/verify", handlers.HandleMFAVerify).Methods("POST", "OPTIONS")
	api.HandleFunc("/mfa/disable", handlers.HandleMFADisable).Methods("POST", "OPTIONS")
	api.HandleFunc("/mfa/reset", handlers.HandleMFAReset).Methods("POST", "OPTIONS")

	// 记录管理 - 特殊路由放在前面
	api.HandleFunc("/records/export", handlers.HandleExportRecords).Methods("GET", "OPTIONS")
	api.HandleFunc("/records/query", handlers.HandleQueryRecords).Methods("GET", "OPTIONS")
	api.HandleFunc("/records/batch", handlers.HandleBatchAddRecords).Methods("POST", "OPTIONS")
	api.HandleFunc("/records/batch-delete", handlers.HandleBatchDeleteRecords).Methods("POST", "OPTIONS")
	api.HandleFunc("/records/batch-status", handlers.HandleBatchUpdateStatus).Methods("POST", "OPTIONS")
	api.HandleFunc("/records", handlers.HandleGetRecords).Methods("GET", "OPTIONS")
	api.HandleFunc("/records", handlers.HandleAddRecord).Methods("POST", "OPTIONS")
	api.HandleFunc("/records/{id}", handlers.HandleGetRecord).Methods("GET", "OPTIONS")
	api.HandleFunc("/records/{id}", handlers.HandleUpdateRecord).Methods("PUT", "OPTIONS")
	api.HandleFunc("/records/{id}", handlers.HandleDeleteRecord).Methods("DELETE", "OPTIONS")

	// 审计日志
	api.HandleFunc("/audit-logs", handlers.HandleGetAuditLogs).Methods("GET", "OPTIONS")
	api.HandleFunc("/audit-logs/export", handlers.HandleExportAuditLogs).Methods("GET", "OPTIONS")

	// 数据源管理
	api.HandleFunc("/datasources", handlers.HandleGetDataSources).Methods("GET", "OPTIONS")
	api.HandleFunc("/datasources", handlers.HandleAddDataSource).Methods("POST", "OPTIONS")
	api.HandleFunc("/datasources/test", handlers.HandleTestDataSource).Methods("POST", "OPTIONS")
	api.HandleFunc("/datasources/{id}", handlers.HandleUpdateDataSource).Methods("PUT", "OPTIONS")
	api.HandleFunc("/datasources/{id}", handlers.HandleDeleteDataSource).Methods("DELETE", "OPTIONS")

	// 巡检功能
	api.HandleFunc("/inspection/execute", handlers.HandleExecuteInspection).Methods("POST", "OPTIONS")

	// 静态文件 - 从 frontend 目录提供
	r.PathPrefix("/css/").Handler(http.StripPrefix("/css/", http.FileServer(http.Dir("../frontend/css"))))
	r.PathPrefix("/js/").Handler(http.StripPrefix("/js/", http.FileServer(http.Dir("../frontend/js"))))
	r.PathPrefix("/").Handler(http.FileServer(http.Dir("../frontend")))

	// 启动服务器
	port := os.Getenv("PORT")
	if port == "" {
		port = "8088"
	}

	log.Printf("服务启动在 http://localhost:%s", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Operator")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

package main

import (
	"log"
	"net/http"
	"os"

	"opsplatform-jira-confluence-lock-backend/database"
	"opsplatform-jira-confluence-lock-backend/handlers"
	"opsplatform-jira-confluence-lock-backend/poller"

	"github.com/gorilla/mux"
)

func main() {
	// 初始化数据库
	if err := database.InitDB(); err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	defer database.Close()

	r := mux.NewRouter()

	// 全局中间件
	r.Use(handlers.CORSMiddleware)
	r.Use(handlers.LoggingMiddleware)

	api := r.PathPrefix("/api").Subrouter()

	// 健康检查
	api.HandleFunc("/health", handleHealth).Methods("GET")

	// Jira Webhook 入口（无需认证，由 Jira 调用）
	api.HandleFunc("/webhook/jira", handlers.HandleJiraWebhook).Methods("POST", "OPTIONS")

	// Confluence 连接配置
	api.HandleFunc("/settings", handlers.HandleGetSettings).Methods("GET", "OPTIONS")
	api.HandleFunc("/settings", handlers.HandleUpdateSettings).Methods("POST", "OPTIONS")
	api.HandleFunc("/settings/test-confluence", handlers.HandleTestConfluence).Methods("POST", "OPTIONS")
	api.HandleFunc("/settings/test-jira", handlers.HandleTestJira).Methods("POST", "OPTIONS")

	// 触发规则管理
	api.HandleFunc("/triggers", handlers.HandleGetTriggers).Methods("GET", "OPTIONS")
	api.HandleFunc("/triggers", handlers.HandleCreateTrigger).Methods("POST", "OPTIONS")
	api.HandleFunc("/triggers/{id}", handlers.HandleUpdateTrigger).Methods("PUT", "OPTIONS")
	api.HandleFunc("/triggers/{id}", handlers.HandleDeleteTrigger).Methods("DELETE", "OPTIONS")

	// 权限规则管理
	api.HandleFunc("/permissions", handlers.HandleGetPermissions).Methods("GET", "OPTIONS")
	api.HandleFunc("/permissions", handlers.HandleCreatePermission).Methods("POST", "OPTIONS")
	api.HandleFunc("/permissions/{id}", handlers.HandleUpdatePermission).Methods("PUT", "OPTIONS")
	api.HandleFunc("/permissions/{id}", handlers.HandleDeletePermission).Methods("DELETE", "OPTIONS")

	// 事件日志
	api.HandleFunc("/events", handlers.HandleGetEvents).Methods("GET", "OPTIONS")
	api.HandleFunc("/events/{id}/retry", handlers.HandleRetryEvent).Methods("POST", "OPTIONS")

	// 启动轮询器（Jira 主动巡检兜底）
	poller.Start()

	// 启动服务
	port := os.Getenv("PORT")
	if port == "" {
		port = ":8080"
	}
	log.Printf("Jira-Confluence-Lock 服务启动: http://localhost%s", port)
	log.Fatal(http.ListenAndServe(port, r))
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","service":"opsplatform-jira-confluence-lock-backend"}`))
}

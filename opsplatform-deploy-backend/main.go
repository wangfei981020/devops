package main

import (
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/mux"
	"opsplatform-deploy-backend/config"
	"opsplatform-deploy-backend/crypto"
	"opsplatform-deploy-backend/database"
	"opsplatform-deploy-backend/handlers"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Starting Deploy Platform Backend...")

	cfg := config.Load()
	handlers.SetConfig(cfg)
	crypto.Init(cfg.AESKey)

	if err := database.InitMySQL(cfg); err != nil {
		log.Fatalf("Init MySQL: %v", err)
	}

	r := mux.NewRouter()
	r.Use(handlers.CORSMiddleware(cfg.CORSOrigin))
	r.Use(handlers.RecoverMiddleware)
	r.Use(handlers.LoggingMiddleware)

	api := r.PathPrefix("/api").Subrouter()

	// 项目
	api.HandleFunc("/projects", handlers.HandleListProjects).Methods("GET", "OPTIONS")
	api.HandleFunc("/projects", handlers.HandleCreateProject).Methods("POST", "OPTIONS")
	api.HandleFunc("/projects/{id}", handlers.HandleGetProject).Methods("GET", "OPTIONS")
	api.HandleFunc("/projects/{id}", handlers.HandleUpdateProject).Methods("PUT", "OPTIONS")
	api.HandleFunc("/projects/{id}", handlers.HandleDeleteProject).Methods("DELETE", "OPTIONS")

	// 环境字典
	api.HandleFunc("/environments", handlers.HandleListEnvironments).Methods("GET", "OPTIONS")
	api.HandleFunc("/environments", handlers.HandleCreateEnvironment).Methods("POST", "OPTIONS")
	api.HandleFunc("/environments/{id}", handlers.HandleUpdateEnvironment).Methods("PUT", "OPTIONS")
	api.HandleFunc("/environments/{id}", handlers.HandleDeleteEnvironment).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/environments/{id}/test-argocd", handlers.HandleTestEnvironmentArgocd).Methods("POST", "OPTIONS")

	// 项目-环境
	api.HandleFunc("/project-envs", handlers.HandleListProjectEnvs).Methods("GET", "OPTIONS")
	api.HandleFunc("/project-envs", handlers.HandleCreateProjectEnv).Methods("POST", "OPTIONS")
	api.HandleFunc("/project-envs/{id}", handlers.HandleGetProjectEnv).Methods("GET", "OPTIONS")
	api.HandleFunc("/project-envs/{id}", handlers.HandleUpdateProjectEnv).Methods("PUT", "OPTIONS")
	api.HandleFunc("/project-envs/{id}", handlers.HandleDeleteProjectEnv).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/project-envs/{id}/test-git", handlers.HandleTestGit).Methods("POST", "OPTIONS")
	api.HandleFunc("/project-envs/{id}/test-argocd", handlers.HandleTestArgocd).Methods("POST", "OPTIONS")

	// Chart 模板
	api.HandleFunc("/chart-templates", handlers.HandleListChartTemplates).Methods("GET", "OPTIONS")
	api.HandleFunc("/chart-templates", handlers.HandleCreateChartTemplate).Methods("POST", "OPTIONS")
	api.HandleFunc("/chart-templates/{id}", handlers.HandleGetChartTemplate).Methods("GET", "OPTIONS")
	api.HandleFunc("/chart-templates/{id}", handlers.HandleUpdateChartTemplate).Methods("PUT", "OPTIONS")
	api.HandleFunc("/chart-templates/{id}", handlers.HandleDeleteChartTemplate).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/chart-templates/{id}/preview", handlers.HandlePreviewChartTemplate).Methods("POST", "OPTIONS")

	// 模块 (核心)
	api.HandleFunc("/modules", handlers.HandleListModules).Methods("GET", "OPTIONS")
	api.HandleFunc("/modules", handlers.HandleCreateModule).Methods("POST", "OPTIONS")
	api.HandleFunc("/modules/{id}", handlers.HandleGetModule).Methods("GET", "OPTIONS")
	api.HandleFunc("/modules/{id}", handlers.HandleUpdateModule).Methods("PUT", "OPTIONS")
	api.HandleFunc("/modules/{id}", handlers.HandleDeleteModule).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/modules/{id}/update-image", handlers.HandleUpdateModuleImage).Methods("POST", "OPTIONS")
	api.HandleFunc("/modules/{id}/restart", handlers.HandleRestartModule).Methods("POST", "OPTIONS")
	api.HandleFunc("/modules/{id}/scale", handlers.HandleScaleModule).Methods("POST", "OPTIONS")
	api.HandleFunc("/modules/{id}/sync", handlers.HandleSyncModule).Methods("POST", "OPTIONS")
	api.HandleFunc("/modules/{id}/rollback", handlers.HandleRollbackModule).Methods("POST", "OPTIONS")
	api.HandleFunc("/modules/{id}/values", handlers.HandleGetModuleValues).Methods("GET", "OPTIONS")
	api.HandleFunc("/modules/{id}/runtime", handlers.HandleGetModuleRuntime).Methods("GET", "OPTIONS")

	// Secret
	api.HandleFunc("/secrets", handlers.HandleListSecrets).Methods("GET", "OPTIONS")
	api.HandleFunc("/secrets", handlers.HandleCreateSecret).Methods("POST", "OPTIONS")
	api.HandleFunc("/secrets/batch-update", handlers.HandleBatchUpdateSecret).Methods("POST", "OPTIONS")
	api.HandleFunc("/secrets/{id}", handlers.HandleGetSecret).Methods("GET", "OPTIONS")
	api.HandleFunc("/secrets/{id}", handlers.HandleUpdateSecret).Methods("PUT", "OPTIONS")
	api.HandleFunc("/secrets/{id}", handlers.HandleDeleteSecret).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/secrets/{id}/referenced-by", handlers.HandleSecretReferencedBy).Methods("GET", "OPTIONS")

	// 通知人
	api.HandleFunc("/contacts", handlers.HandleListContacts).Methods("GET", "OPTIONS")
	api.HandleFunc("/contacts", handlers.HandleCreateContact).Methods("POST", "OPTIONS")
	api.HandleFunc("/contacts/{id}", handlers.HandleGetContact).Methods("GET", "OPTIONS")
	api.HandleFunc("/contacts/{id}", handlers.HandleUpdateContact).Methods("PUT", "OPTIONS")
	api.HandleFunc("/contacts/{id}", handlers.HandleDeleteContact).Methods("DELETE", "OPTIONS")

	// Lark 群配置
	api.HandleFunc("/lark-configs", handlers.HandleListLarkConfigs).Methods("GET", "OPTIONS")
	api.HandleFunc("/lark-configs", handlers.HandleCreateLarkConfig).Methods("POST", "OPTIONS")
	api.HandleFunc("/lark-configs/{id}", handlers.HandleUpdateLarkConfig).Methods("PUT", "OPTIONS")
	api.HandleFunc("/lark-configs/{id}", handlers.HandleDeleteLarkConfig).Methods("DELETE", "OPTIONS")
	api.HandleFunc("/lark-configs/{id}/test", handlers.HandleTestLarkConfig).Methods("POST", "OPTIONS")

	// 项目-环境通知绑定
	api.HandleFunc("/project-env-notify", handlers.HandleGetProjectEnvNotify).Methods("GET", "OPTIONS")
	api.HandleFunc("/project-env-notify", handlers.HandleSetProjectEnvNotify).Methods("PUT", "OPTIONS")

	// 发布历史
	api.HandleFunc("/deployments", handlers.HandleListDeployments).Methods("GET", "OPTIONS")
	api.HandleFunc("/deployments/{id}", handlers.HandleGetDeployment).Methods("GET", "OPTIONS")
	api.HandleFunc("/deployments/{id}/diff", handlers.HandleGetDeploymentDiff).Methods("GET", "OPTIONS")

	// Harbor 代理
	api.HandleFunc("/harbor/projects", handlers.HandleHarborProjects).Methods("GET", "OPTIONS")
	api.HandleFunc("/harbor/repositories", handlers.HandleHarborRepositories).Methods("GET", "OPTIONS")
	api.HandleFunc("/harbor/tags", handlers.HandleHarborTags).Methods("GET", "OPTIONS")

	// ArgoCD 代理
	api.HandleFunc("/argocd/applications", handlers.HandleArgocdApplications).Methods("GET", "OPTIONS")
	api.HandleFunc("/argocd/applications/{name}", handlers.HandleArgocdApplication).Methods("GET", "OPTIONS")
	api.HandleFunc("/argocd/applications/{name}/status", handlers.HandleArgocdAppStatus).Methods("GET", "OPTIONS")
	api.HandleFunc("/argocd/applications/{name}/sync", handlers.HandleArgocdAppSync).Methods("POST", "OPTIONS")
	api.HandleFunc("/argocd/applications/{name}/events", handlers.HandleArgocdAppEvents).Methods("GET", "OPTIONS")

	// 全局配置
	api.HandleFunc("/global-config", handlers.HandleGetGlobalConfig).Methods("GET", "OPTIONS")
	api.HandleFunc("/global-config", handlers.HandleUpdateGlobalConfig).Methods("PUT", "OPTIONS")
	api.HandleFunc("/global-config/test-gitlab", handlers.HandleTestGlobalGitlab).Methods("POST", "OPTIONS")
	api.HandleFunc("/global-config/test-harbor", handlers.HandleTestGlobalHarbor).Methods("POST", "OPTIONS")
	api.HandleFunc("/global-config/test-argocd", handlers.HandleTestGlobalArgocd).Methods("POST", "OPTIONS")

	// 环境变量模板
	api.HandleFunc("/env-templates", handlers.HandleListEnvTemplates).Methods("GET", "OPTIONS")
	api.HandleFunc("/env-templates", handlers.HandleCreateEnvTemplate).Methods("POST", "OPTIONS")
	api.HandleFunc("/env-templates/{id}", handlers.HandleUpdateEnvTemplate).Methods("PUT", "OPTIONS")
	api.HandleFunc("/env-templates/{id}", handlers.HandleDeleteEnvTemplate).Methods("DELETE", "OPTIONS")

	// 健康/就绪检查 (独立端口)
	go func() {
		hm := http.NewServeMux()
		hm.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"ok","service":"deploy-backend"}`))
		})
		hm.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
			if err := database.DB.Ping(); err != nil {
				w.WriteHeader(http.StatusServiceUnavailable)
				w.Write([]byte(`{"status":"db unreachable"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"status":"ready"}`))
		})
		log.Printf("Health server on %s (/health, /ready)", cfg.HealthPort)
		http.ListenAndServe(cfg.HealthPort, hm)
	}()

	// 优雅关闭
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("Shutting down...")
		os.Exit(0)
	}()

	log.Printf("Deploy backend started on %s", cfg.Port)
	if err := http.ListenAndServe(cfg.Port, r); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

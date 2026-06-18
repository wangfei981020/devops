package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"

	"opsplatform-cmdb-backend/config"
	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/database"
	"opsplatform-cmdb-backend/handlers"
)

func main() {
	cfg := config.Load()
	db, err := database.Open(cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("db open: %v", err)
	}
	defer db.Close()

	cipher, err := crypto.New(cfg.AESKey)
	if err != nil {
		log.Fatalf("crypto init: %v", err)
	}

	// Prometheus: 证书/域名到期与创建时间指标（白名单控自定义 label 基数）
	prometheus.MustRegister(handlers.NewCMDBCollector(db))

	// 健康 + /metrics 独立端口（业务端口 hang 死时仍可探活），与 k8sinsight/gke-version 一致
	go handlers.StartHealthServer(cfg.HealthPort, db)
	// 每日任务：自动续期 + 到期提醒
	go handlers.StartScheduler(db, cipher)

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()
	r.Use(handlers.CORS())

	authH := handlers.NewAuthHandler(db, cfg.JWTSecret)
	authH.EnsureAdmin()

	api := r.Group("/api")
	authH.RegisterPublic(api)
	// A+ 取证书：token 自鉴权，注册在登录中间件之前（目标机无需登录态拉取）
	handlers.NewBundleHandler(db, cipher).RegisterPublic(api)
	api.Use(authH.Middleware())
	handlers.NewCIHandler(db).Register(api)
	handlers.NewRelationHandler(db).Register(api)
	handlers.NewRegistrarHandler(db, cipher).Register(api)
	handlers.NewDomainHandler(db).Register(api)
	handlers.NewCertHandler(db, cipher).Register(api)
	handlers.NewSettingsHandler(db).Register(api)
	handlers.NewDashboardHandler(db).Register(api)
	handlers.NewBasicHandler(db).Register(api)
	handlers.NewRecordHandler(db).Register(api)
	handlers.NewSyncHandler(db, cipher).Register(api)

	log.Printf("CMDB backend listening on %s", cfg.Port)
	if err := http.ListenAndServe(cfg.Port, r); err != nil {
		log.Fatal(err)
	}
}

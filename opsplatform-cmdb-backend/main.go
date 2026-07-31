package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"

	"opsplatform-cmdb-backend/config"
	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/database"
	"opsplatform-cmdb-backend/handlers"
	"opsplatform-cmdb-backend/k8ssource"
	"opsplatform-cmdb-backend/logx"
)

// fatal 打一条 JSON 日志后退出（替代 log.Fatal，统一日志格式）。
func fatal(msg string) {
	logx.Line("main", msg)
	os.Exit(1)
}

func main() {
	cfg := config.Load()
	db, err := database.Open(cfg.MySQLDSN)
	if err != nil {
		fatal(fmt.Sprintf("db open: %v", err))
	}
	defer db.Close()

	cipher, err := crypto.New(cfg.AESKey)
	if err != nil {
		fatal(fmt.Sprintf("crypto init: %v", err))
	}

	// Prometheus: 证书/域名到期与创建时间指标（白名单控自定义 label 基数）
	prometheus.MustRegister(handlers.NewCMDBCollector(db))

	// 健康 + /metrics 独立端口（业务端口 hang 死时仍可探活），与 k8sinsight/gke-version 一致
	go handlers.StartHealthServer(cfg.HealthPort, db)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New() // 不用 gin.Default()，改用 JSON 访问日志
	r.Use(gin.Recovery())
	r.Use(handlers.RequestID())
	r.Use(handlers.AccessLog())
	r.Use(handlers.CORS())

	authH := handlers.NewAuthHandler(db, cfg.JWTSecret)
	authH.EnsureAdmin()

	api := r.Group("/api")
	authH.RegisterPublic(api)
	// A+ 取证书：token 自鉴权，注册在登录中间件之前（目标机无需登录态拉取）
	handlers.NewBundleHandler(db, cipher).RegisterPublic(api)
	// MCP：自带 token 鉴权，注册在登录中间件之前（AI 客户端用 MCP token 连）
	handlers.NewMCPHandler(db, cfg.JWTSecret, cfg.Port).RegisterPublic(api)
	api.Use(authH.Middleware())
	handlers.NewCIHandler(db).Register(api)
	handlers.NewRelationHandler(db).Register(api)
	handlers.NewRegistrarHandler(db, cipher).Register(api)
	handlers.NewDomainHandler(db).Register(api)
	handlers.NewCertHandler(db, cipher).Register(api)
	handlers.NewSettingsHandler(db).Register(api)
	handlers.NewSchedHandler(db).Register(api)
	handlers.NewNotifyHandler(db).Register(api)
	handlers.NewLarkGroupHandler(db).Register(api)
	handlers.NewDashboardHandler(db).Register(api)
	handlers.NewBasicHandler(db).Register(api)
	handlers.NewRecordHandler(db).Register(api)
	handlers.NewSyncHandler(db, cipher).Register(api)
	handlers.NewCertInspectHandler(db).Register(api)
	handlers.NewHostHandler(db, cipher).Register(api)
	netH := handlers.NewNetworkHandler(db)
	netH.Register(api)
	netH.RegisterIAMDNS(api) // GCP IAM 权限审计 + Cloud DNS 台账/与 Cloudflare 一致性
	cdnH := handlers.NewCDNHandler(db, cipher)
	cdnH.Register(api)      // CDN(Cloudflare) 只读接入
	cdnH.RegisterRules(api) // CDN 规则台账 + 优化分析（Page Rules / Rulesets）
	// K8s 模块（k8sinsight 合并，只读多集群）：阶段1 集群纳管
	k8sPool := k8ssource.NewPool(db, cipher)
	// 定时任务调度器：放在 Pool 之后启动，节点健康任务需要 Pool 直连集群
	go handlers.StartScheduler(db, cipher, k8sPool)
	handlers.NewK8sClusterHandler(db, cipher, k8sPool).Register(api)
	handlers.NewGKEUpgradeHandler(db).Register(api)
	handlers.NewGKEHistoryHandler(db).Register(api)
	handlers.NewGKEUpgradePlanHandler(db).Register(api) // 升级预案 + 过程看板（只读，执行仍在 GCP 控制台）
	handlers.NewK8sResourceHandler(db, k8sPool, cipher).Register(api)
	handlers.NewK8sPDBHandler(db).Register(api) // PDB：节点能不能被 drain 走，升级卡不卡
	handlers.NewK8sTopologyHandler(db, cipher).Register(api)
	handlers.NewK8sCostHandler(db).Register(api)
	handlers.NewK8sDiagHandler(db, k8sPool, cipher).Register(api) // 合并 k8sinsight：实时日志/事件/规则诊断
	handlers.NewEventCenterHandler(db, k8sPool).Register(api)     // 事件中心:到期/变更/同步失败/K8s Warning 统一时间线
	handlers.NewObsHandler(db, cipher).Register(api)              // 数据源接入(Prometheus/Loki/KubeSphere 地址)
	obsQ := handlers.NewObsQueryHandler(db, cipher)
	obsQ.Register(api)          // 资源使用率/Loki/KubeSphere 查询
	obsQ.RegisterInsights(api)  // 浪费排行/闲置成本（需 Prometheus 实测数据）
	obsQ.RegisterDevOps(api)    // 流水线运行记录/构建日志（Jenkins 输出不进 pod stdout，只能走这条）
	obsQ.RegisterPromQuery(api) // 通用 PromQL + 指标发现：中间件(Kafka/nacos/etcd…)指标不必逐个写接口
	harborH := handlers.NewHarborHandler(db, cipher)
	harborH.Register(api)      // Harbor 只读：健康/配额/仓库（补发布链路「推送」「拉取」两个环节）
	harborH.RegisterAdmin(api) // Harbor 接入配置
	mcpH := handlers.NewMCPHandler(db, cfg.JWTSecret, cfg.Port)
	mcpH.RegisterAuthed(api)
	// 周期全量同步所有启用集群（阶段3），默认每 120s 一轮
	go k8ssource.StartScheduler(db, k8sPool, k8ssource.DefaultSyncIntervalSec)
	// 每 6h 刷新当月成本快照（跨月自动定格上月），供环比/报告用
	go handlers.StartCostSnapshotScheduler(db)
	// 每 6h 同步 CDN(Cloudflare) 的 Zone/DNS/设置
	go handlers.StartCDNScheduler(db, cipher)

	logx.Line("main", fmt.Sprintf("CMDB backend listening on %s", cfg.Port))
	if err := http.ListenAndServe(cfg.Port, r); err != nil {
		fatal(err.Error())
	}
}

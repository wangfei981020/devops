package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"

	"ops-cmdb-backend/cloudsource"
	"ops-cmdb-backend/config"
	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/database"
	"ops-cmdb-backend/dnsource"
	"ops-cmdb-backend/handlers"
	notifyapi "ops-cmdb-backend/internal/api/automate/notify"
	"ops-cmdb-backend/internal/api/inventory/registrar"
	"ops-cmdb-backend/internal/api/middleware"
	"ops-cmdb-backend/internal/cluster"
	"ops-cmdb-backend/internal/license"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/k8ssource"
	"ops-cmdb-backend/logx"
)

// version 由构建时 -ldflags 注入。
//
//	不注入就是 "dev" —— 那样线上排障看不出跑的是哪个版本，
//	靠镜像 tag 推断在滚动更新中途尤其不可靠（两个版本同时在跑）。
var version = "dev"

// fatal 打一条 JSON 日志后退出（替代 log.Fatal，统一日志格式）。
func fatal(msg string) {
	logx.Line("main", msg)
	os.Exit(1)
}

func main() {
	// -version 在连数据库之前处理：确认镜像里到底是哪一版，
	// 不该要求有一个能连通的库。没有它的话，验证版本只能靠启动日志，
	// 而启动失败的镜像恰恰是最需要确认版本的那个。
	for _, a := range os.Args[1:] {
		if a == "-version" || a == "--version" {
			fmt.Println(version)
			return
		}
	}

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
	//nolint:leadergate 探活与指标必须每个副本各跑一份：非 leader 的副本一样在服务流量，不能没有健康端点
	go handlers.StartHealthServer(cfg.HealthPort, db)

	// 全库唯一的数据访问入口。业务代码不应再直接持有 db。
	st := store.New(db)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New() // 不用 gin.Default()，改用 JSON 访问日志
	r.Use(gin.Recovery())
	r.Use(handlers.RequestID())
	r.Use(handlers.AccessLog())
	r.Use(handlers.CORS())

	// ── 授权 ───────────────────────────────────────────────────
	//
	// 启动时装载一次，随后由 Watch 让各副本自行收敛（LICENSING §12）。
	// 装载失败**不阻止启动**：授权读不出来时系统退回 CE 档，
	// 而不是整个服务起不来 —— 后者会把"数据库抖了一下"放大成一次全站故障。
	licMgr := license.NewManager()
	licStore := license.NewStore(db)
	if err := licStore.Reload(context.Background(), licMgr); err != nil {
		logx.J("license", "initial_load_failed", map[string]any{
			"err":  err.Error(),
			"note": "本副本按未激活（CE 档）启动；Watch 会在恢复后自行收敛",
		})
	}
	//nolint:leadergate 每个副本都要自己收敛授权状态，不能只让 leader 跑 ——
	// 非 leader 的副本一样在服务写请求，状态停在旧值就会出现"一半请求说未授权"
	licStore.StartWatch(context.Background(), licMgr)
	logx.J("license", "loaded", map[string]any{"status": string(licMgr.Status())})

	authH := handlers.NewAuthHandler(db, cfg.JWTSecret, cfg.PortalAPIURL, cfg.SessionHours, cipher)
	authH.EnsureAdmin()
	// 独立于 EnsureAdmin：那个函数只在首次创建 admin 时往下走，
	// 归属种子必须每次启动都跑，否则老库升级上来的安装永远补不上。
	authH.EnsureAdminTenant()

	api := r.Group("/api")
	authH.RegisterPublic(api)
	// 登录页左栏的三个数字。免鉴权，只给聚合计数、不给任何资源明细
	handlers.NewPublicStatsHandler(db).RegisterPublic(api)
	// 运维平台单点登录入口：注册在登录中间件之前（拿 portal token 换 CMDB 会话）
	authH.RegisterPortal(api)
	// A+ 取证书：token 自鉴权，注册在登录中间件之前（目标机无需登录态拉取）
	handlers.NewBundleHandler(db, cipher).RegisterPublic(api)
	// MCP：自带 token 鉴权，注册在登录中间件之前（AI 客户端用 MCP token 连）
	mcpH := handlers.NewMCPHandler(db, cfg.JWTSecret, cfg.Port, licMgr)
	mcpH.RegisterPublic(api)
	// OIDC 单点登录：整条链路都在登录中间件之前 —— 用户就是因为还没登录才走这里。
	// ⚠️ 无论 SSO 配成什么样，上面的本地密码登录都保留着：
	// IdP 挂了/配错了的时候，那是唯一进得来的门。
	idpH := handlers.NewIdPHandler(db, cipher, authH, licMgr)
	idpH.RegisterPublic(api)
	api.Use(authH.Middleware())
	// 租户上下文：从会话取当前活跃租户并校验归属，注入 request context 供 store 层使用。
	// 必须在认证之后 —— 它依赖会话信息。
	// 没有租户时不拦请求：平台级接口（租户管理、license、系统信息）本来就不需要租户。
	api.Use(middleware.Tenant(&middleware.SessionResolver{DB: db}))
	// 接口级权限校验：表驱动（handlers/perm.go），未映射的路由一律 403。
	// 必须挂在 Middleware 之后——它要读会话里的权限快照。
	api.Use(handlers.PermGuard(db))
	// 授权只读降级：过期/超期/未买本产品时拦下**写**操作。
	//
	// ⚠️ 挂在 PermGuard 之后：没权限就是没权限，不该因为授权过期
	// 被告知"系统只读" —— 那会让人以为续了费就能做，而其实他压根没这个权限。
	// ⚠️ 只拦写。把读也锁掉，客户连自己的数据都导不出来，那不是催款是扣押。
	api.Use(license.ReadOnlyGuard(licMgr))
	// 功能级门控：表驱动（internal/license/guard.go 的 featureRoutes），只拦写接口。
	//
	// ⚠️ 挂在 ReadOnlyGuard 之后：先回答"系统是不是只读"，再回答"这个功能买没买"。
	// 反过来的话，一个过期的实例会先被告知"没买这个功能"——而他买了，只是过期了，
	// 两者的下一步完全不同（续费 vs 加购）。
	api.Use(license.FeatureGuard(licMgr))
	// 变更追溯：写请求前后各拍一次行快照，自动 diff 后落 audit_changes。
	// 必须在 PermGuard 之后——被权限挡下的请求由 denyPerm 单独记 denied，
	// 不该在这里再记一条 success。
	handlers.SetAuditCipher(cipher)
	api.Use(handlers.AuditMiddleware(db))
	authH.RegisterAuthed(api)
	handlers.NewLicenseHandler(db, licMgr, licStore).Register(api)
	idpH.RegisterAuthed(api)
	handlers.NewClusterListHandler(db).Register(api)
	handlers.NewNodeListHandler(db).Register(api)
	handlers.NewPodListHandler(db).Register(api)
	handlers.NewWorkloadListHandler(db).Register(api)
	handlers.NewNamespaceListHandler(db).Register(api)
	handlers.NewCertListHandler(db).Register(api)
	handlers.NewSubnetListHandler(db).Register(api)
	lbList := handlers.NewLBListHandler(db)
	lbList.Register(api)
	// 启动自检：库里出现了 lbHealth 不认识的 backend_state 就 WARN。
	// 不认识的取值会让判定退回只看条数 —— 那正是 P0-8 的失效模式，必须吵出来
	lbList.CheckBackendStates()
	handlers.NewDomainListHandler(db).Register(api)
	handlers.NewSvcListHandler(db).Register(api)
	handlers.NewPVCListHandler(db, st).Register(api)
	handlers.NewOverviewHandler(db).Register(api)
	handlers.NewTaskListHandler(db).Register(api)
	handlers.NewCostOverviewHandler(st, db).Register(api)
	handlers.NewDataSourceHandler(db).Register(api)
	handlers.NewExposureHandler(db).Register(api)
	handlers.NewTopologyHandler(db).Register(api)
	handlers.NewEventListHandler(db).Register(api)
	handlers.NewCIHandler(st, db).Register(api)
	handlers.NewRelationHandler(st, db).Register(api)
	// 已迁移到 store 层（租户隔离 + 修掉了 WHERE id=? 的跨租户越权）。
	// 其余 handler 照 internal/api/inventory/registrar 的样板逐个迁。
	registrar.New(st, cipher).Register(api)
	handlers.NewDomainHandler(st, db).Register(api)
	handlers.NewCertHandler(st, db, cipher).Register(api)
	handlers.NewSettingsHandler(db).Register(api)
	handlers.NewSchedHandler(db).Register(api)
	// 已迁移到 store 层（租户隔离）。设置读取与 @ 拼接从旧包注入 —— 迁移期桥接。
	notifyapi.NewUsers(st,
		func(k string) string { return handlers.GetSetting(db, k) },
		func() string { return handlers.AtMentions(db) },
	).Register(api)
	// 已迁移到 store 层（租户隔离 + 修掉 4 处跨租户越权）。
	// 权限判定与掩码函数从旧包注入 —— 它们迁移后这两个参数可以去掉。
	notifyapi.NewLark(st, handlers.HasPerm, handlers.MaskWebhookURL).Register(api)
	handlers.NewDashboardHandler(db).Register(api)
	handlers.NewBasicHandler(st, db).Register(api)
	handlers.NewRecordHandler(st, db).Register(api)
	handlers.NewSyncHandler(st, db, cipher).Register(api)
	handlers.NewCertInspectHandler(st, db).Register(api)
	handlers.NewHostHandler(st, db, cipher).Register(api)
	netH := handlers.NewNetworkHandler(db)
	netH.Register(api)
	netH.RegisterIAMDNS(api) // GCP IAM 权限审计 + Cloud DNS 台账/与 Cloudflare 一致性
	cdnH := handlers.NewCDNHandler(st, db, cipher)
	cdnH.Register(api) // CDN(Cloudflare) 只读接入
	// 注册商(GoDaddy)侧的解析记录。数据一直在采，但从没有页面读过它
	// —— DNS 解析页只有 CF 和 GCP 两个来源（OPSCMDB-031 P0-10）
	(&handlers.RegistrarDNSHandler{DB: db}).Register(api)
	cdnH.RegisterRules(api) // CDN 规则台账 + 优化分析（Page Rules / Rulesets）
	// 自检：库内字符串列的排序规则是否统一。不统一时跨表字符串比较会抛 Error 1267，
	// 而这种失败在页面上只表现为"查不到数据"——LB 后端 81/81 全空就是这么来的。
	go handlers.CheckCollations(db)

	// ── 多副本协调 ──────────────────────────────────────────────
	//
	// 每个副本都会注册同一份 cron。不做约束的话，3 副本 = 每个定时任务
	// 触发 3 次：主机同步 3 倍云 API 调用、证书续期被 ACME 限速，
	// 而域名续费是非幂等写 —— **等于扣 3 次钱**。
	//
	// 所以 cron 类的后台循环统一由 leader 执行；HTTP 请求不受影响，
	// 所有副本照常提供服务（那才是扩容的意义）。
	leader, stopLeader := startLeader(st)

	// 厂商 API 配额改成跨副本记账。
	//
	// ⚠️ 不注入的话，每个副本各算 50/分钟，2 副本 = 100，
	// 越过 GoDaddy 的 60。撞上之后的现象不是一句报错，
	// 而是批量续费里"莫名其妙有几个没续上"。
	dnsource.SetQuota(cluster.NewQuota(db))
	// ⚠️ 同一个坑在两个地方，此前只填了一个：cloudsource（GCP 300/分钟）
	// 一直是纯进程内计数。7 副本（HPA 上限）= 2100/分钟。
	// 表现同样不是报错，而是同步结果里"莫名其妙少了几台机器"。
	cloudsource.SetQuota(cluster.NewQuota(db))

	// K8s 模块（k8sinsight 合并，只读多集群）：阶段1 集群纳管
	k8sPool := k8ssource.NewPool(db, cipher)
	// 定时任务调度器：放在 Pool 之后启动，节点健康任务需要 Pool 直连集群
	go handlers.StartScheduler(st, db, cipher, k8sPool, leader)
	handlers.NewK8sClusterHandler(st, db, cipher, k8sPool).Register(api)
	handlers.NewGKEUpgradeHandler(st, db).Register(api)
	handlers.NewGKEHistoryHandler(db).Register(api)
	handlers.NewGKEUpgradePlanHandler(db).Register(api) // 升级预案 + 过程看板（只读，执行仍在 GCP 控制台）
	handlers.NewK8sResourceHandler(st, db, k8sPool, cipher).Register(api)
	handlers.NewK8sPDBHandler(db).Register(api) // PDB：节点能不能被 drain 走，升级卡不卡
	handlers.NewK8sTopologyHandler(db, cipher).Register(api)
	handlers.NewK8sCostHandler(st, db).Register(api)
	handlers.NewK8sDiagHandler(db, k8sPool, cipher).Register(api)     // 合并 k8sinsight：实时日志/事件/规则诊断
	handlers.NewEventCenterHandler(db, k8sPool, cipher).Register(api) // 事件中心:到期/变更/同步失败/K8s Warning 统一时间线
	handlers.NewObsHandler(st, db, cipher).Register(api)              // 数据源接入(Prometheus/Loki/KubeSphere 地址)
	obsQ := handlers.NewObsQueryHandler(st, db, cipher)
	obsQ.Register(api) // 资源使用率/Loki/KubeSphere 查询
	// 域名访问质量：台账 vs 客户端实际拿到的东西（OPSCMDB-035）。
	// 复用 obsQ 拿数据源解析 —— 它和 blackbox 读的是同一个 VM
	handlers.NewDomainQualityHandler(db, obsQ).Register(api)
	obsQ.RegisterInsights(api)  // 浪费排行/闲置成本（需 Prometheus 实测数据）
	obsQ.RegisterDevOps(api)    // 流水线运行记录/构建日志（Jenkins 输出不进 pod stdout，只能走这条）
	obsQ.RegisterPromQuery(api) // 通用 PromQL + 指标发现：中间件(Kafka/nacos/etcd…)指标不必逐个写接口
	harborH := handlers.NewHarborHandler(st, db, cipher)
	harborH.Register(api)      // Harbor 只读：健康/配额/仓库（补发布链路「推送」「拉取」两个环节）
	harborH.RegisterAdmin(api) // Harbor 接入配置
	// 复用上面那个实例：新建第二个会各自持有一份授权判断，
	// 以后改了一处忘了另一处，就会出现"清单里有、调用被拒"的分裂
	mcpH.RegisterAuthed(api)
	handlers.NewAlertHandler(db, cipher).Register(api) // 夜莺告警（接入走 obs_endpoints type=n9e）
	handlers.NewUserHandler(db).Register(api)          // 用户管理（区分本地账号与运维平台 SSO 影子账号）
	// 本地账号的角色。SSO 用户不看这份——他们的权限每次登录从运维平台实时拉。
	handlers.SeedLocalRoles(db)
	api.GET("/local-roles", (&handlers.LocalRoleHandler{DB: db}).List)
	handlers.NewAuditHandler(st, db).Register(api) // 操作审计查询 + 变更回滚
	handlers.StartAuditCleanup(db)                 // 按保留期清理（默认 0=永久，不删）
	// 下面三个也是"每副本各跑一份"的后台循环，同样只让 leader 干活。
	// 周期全量同步所有启用集群（阶段3），默认每 120s 一轮
	go k8ssource.StartScheduler(db, k8sPool, k8ssource.DefaultSyncIntervalSec, leaderGate(leader))
	// 每 6h 刷新当月成本快照（跨月自动定格上月），供环比/报告用
	go handlers.StartCostSnapshotScheduler(st, db, leaderGate(leader))
	// 每 6h 同步 CDN(Cloudflare) 的 Zone/DNS/设置
	go handlers.StartCDNScheduler(st, db, cipher, leaderGate(leader))

	// 权限映射自检：把没被 perm.go 规则覆盖的路由打出来。
	// fail-closed 下漏配 = 该接口直接 403，与其等用户报"页面打不开"，
	// 不如启动时就在日志里列清单。免登录路由（登录/SSO/MCP/证书自取）不参与。
	handlers.AuditRouteCheck(r.Routes())
	handlers.AuditPermCheck(r.Routes(), []string{
		"/api/login", "/api/portal-auth", "/api/mcp", "/api/certs/:id/bundle",
	})

	// 功能门控自检：featureRoutes 里的路径必须真实存在。
	//
	// ⚠️ 这条的失效方向是**放行** —— 路径改了名而表没跟上，那条接口
	// 从此不问授权，谁都能用，没有报错、没有日志、界面上完全正常。
	// 与权限自检（漏配=403，用户当场就会报）恰好相反，只能靠启动时对账发现。
	if missing := license.UnmatchedFeatureRoutes(r.Routes()); len(missing) > 0 {
		logx.J("license", "feature_route_missing", map[string]any{
			"routes": missing,
			"note":   "功能门控表里的路由在实际路由表中不存在，这些接口当前不受授权限制",
		})
	}

	// 把生效的日志等级打出来：不打的话没人知道 LOG_LEVEL 到底有没有被读到，
	// 「我明明设了 debug 怎么没日志」会变成又一轮排查
	logx.J("main", "starting", map[string]any{"version": version, "port": cfg.Port,
		"replica": os.Getenv("POD_NAME"), "log_level": logx.Level()})
	serve(cfg.Port, r, stopLeader)
}

// startLeader 参选 leader。
//
// 返回值：
//
//   - *cluster.Leader 可为 nil —— 表示本进程放弃协调、所有后台任务照跑
//     （单副本 / 本地开发）
//
//   - 第二个返回值用来在退出时**主动让位**
//
//     ⚠️ 这里必须给一个**可取消**的 context。
//     第一版传的是 context.Background()，那个 context 永远不会取消，
//     于是 Leader.Run 里的优雅释放分支根本走不到 ——
//     代码写了、测试也覆盖了，实际却从不执行。
//     实测表现：滚动更新时新副本要等 40 秒（TTL 30s + 一个续约周期）
//     才当选，那段时间没有任何副本跑定时任务。
//     这种"功能存在但接线接错"的 bug，单测抓不到（测的是 Run 本身），
//     只有看真实滚动更新的时间线才会露出来。
func startLeader(st *store.Store) (*cluster.Leader, func()) {
	// Pod 名天然唯一，是最合适的 owner。K8s 里用 downward API 注入：
	//   env: [{name: POD_NAME, valueFrom: {fieldRef: {fieldPath: metadata.name}}}]
	owner := os.Getenv("POD_NAME")
	if owner == "" {
		owner, _ = os.Hostname()
	}
	if owner == "" {
		// 拿不到唯一标识时**不能**编一个固定值糊弄过去 ——
		// 那样多个副本会互相认成自己，租约形同虚设，
		// 而症状（任务重复执行）要到扣了多次费才被发现。
		// 宁可退回"不协调、全都跑"，那至少是个已知且一致的行为。
		logx.Line("main", "WARN 取不到 POD_NAME/hostname，跳过 leader 选举：所有后台任务将在本副本执行。多副本部署下必须注入 POD_NAME")
		return nil, func() {}
	}
	m, err := cluster.New(st, cluster.Options{Owner: owner})
	if err != nil {
		logx.Line("main", "WARN 构造租约管理器失败，跳过 leader 选举: "+err.Error())
		return nil, func() {}
	}
	l := cluster.NewLeader(m, "scheduler", 30*time.Second)
	l.OnChange = func(became bool, ls cluster.Lease) {
		logx.J("cluster", "leader_change", map[string]any{
			"owner": owner, "leader": became, "fence": ls.Fence,
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { l.Run(ctx); close(done) }()
	// 返回的函数会等 Run 真正退出（释放已发出去）再返回，
	// 否则主进程可能在 SQL 发出之前就结束了，等于没释放。
	return l, func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			logx.Line("main", "WARN 让出 leader 超时，接班副本需等租约自然过期")
		}
	}
}

// leaderGate 把 Leader 包成一个"现在能不能干活"的判断函数，
// 供那些不方便直接依赖 cluster 包的后台循环使用。
// leader 为 nil 时恒真（单副本行为不变）。
func leaderGate(l *cluster.Leader) func() bool {
	if l == nil {
		return func() bool { return true }
	}
	return l.IsLeader
}

// serve 启动 HTTP 服务并处理优雅退出。
//
// # 为什么必须优雅退出
//
// 承诺过「客户升级不中断业务」。滚动更新时 K8s 发 SIGTERM，
// 直接退出的话，正在处理的请求会被拦腰截断 —— 用户看到的是 502。
//
// 这里做两件事：停止接受新连接、等待在途请求跑完（上限 20s，
// 比 K8s 默认的 terminationGracePeriodSeconds=30 留了余量）。
func serve(port string, h http.Handler, stopLeader func()) {
	srv := &http.Server{Addr: port, Handler: h}
	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err := <-errc:
		fatal(err.Error())
	case sig := <-stop:
		logx.Line("main", fmt.Sprintf("收到 %s，开始优雅退出", sig))
	}

	// 先让出 leader，再排空请求。
	//
	//	顺序是有讲究的：让位是给**别的副本**用的信号，越早越好 ——
	//	它可以立刻接管定时任务。而排空请求是本副本自己的事，
	//	要花几秒到几十秒。反过来的话，这几十秒里没有任何副本在跑定时任务。
	stopLeader()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		// 超时说明有请求迟迟没结束。如实记下来 ——
		// 静默退出的话，偶发的 502 永远查不到源头。
		logx.Line("main", "WARN 优雅退出超时，仍有在途请求被中断: "+err.Error())
		return
	}
	logx.Line("main", "已优雅退出")
}

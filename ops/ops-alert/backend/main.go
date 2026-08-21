package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"ops-alert-backend/internal/license"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"ops-alert-backend/config"
	"ops-alert-backend/crypto"
	"ops-alert-backend/database"
	"ops-alert-backend/engine"
	"ops-alert-backend/internal/api"
	"ops-alert-backend/internal/store"
	"ops-alert-backend/logx"
)

// version 由构建时 -ldflags 注入。不注入就是 "dev" —— 那样线上排障
// 看不出跑的是哪一版，靠镜像 tag 推断在滚动更新中途尤其不可靠。
var version = "dev"

func fatal(msg string) {
	logx.Line("main", msg)
	os.Exit(1)
}

func main() {
	// -version 在连数据库之前处理：启动失败的镜像恰恰是最需要确认版本的那个。
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
		fatal(fmt.Sprintf("crypto: %v", err))
	}

	st := store.New(db)
	if err := seedAdmin(st); err != nil {
		fatal(fmt.Sprintf("seed admin: %v", err))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 检测引擎与 HTTP 服务同进程但各自独立：
	// 引擎卡住时接口仍然可用，这样才能进去看「为什么不告警了」。
	eng := engine.New(st, cfg, cipher)
	go eng.Start(ctx)
	// 后台任务：数据源探测、未认领升级、保留期清理。
	// 与检测共用同一个租约，避免"A 副本检测、B 副本升级"这种难以推理的时序。
	go eng.RunTasks(ctx)

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	// 授权：启动时装载一次，随后由 Watch 让各副本自行收敛（LICENSING §12）。
	//
	// ⚠️ 装载失败**不阻止启动**：授权读不出来时退回 CE 档，
	// 而不是整个服务起不来 —— 后者会把"数据库抖了一下"放大成一次全站故障。
	// 对一个告警系统尤其不能这样：它起不来的时候，正是最需要它的时候。
	// 时区解析结果必须打出来。配错了（或没配、走了默认）时，
	// 唯一的现象是日报在意料之外的时刻发出去 —— 那要好几天才会被发现。
	//
	// ⚠️ 过夏令时的时区要额外警告：MySQL 会话的偏移量是启动时算的一次，
	// 夏令时切换后进程里那个值不会跟着变，NOW() 会与 Go 差一小时直到重启。
	{
		zoneName, offset := time.Now().In(cfg.Location).Zone()
		janOff := time.Date(time.Now().Year(), 1, 15, 12, 0, 0, 0, cfg.Location).Format("-07:00")
		julOff := time.Date(time.Now().Year(), 7, 15, 12, 0, 0, 0, cfg.Location).Format("-07:00")
		fields := map[string]any{
			"tz": cfg.Location.String(), "abbr": zoneName, "offset_sec": offset,
			"note": "业务时区只影响墙上时钟判定（日报几点发、「昨天」的边界）；界面显示走浏览器本地时区",
		}
		if janOff != julOff {
			fields["warn"] = "该时区有夏令时（" + janOff + " / " + julOff + "）。" +
				"MySQL 会话偏移在启动时算定，夏令时切换后会与实际差一小时，需重启修正"
		}
		logx.J("main", "timezone", fields)
	}

	licMgr := license.NewManager()
	licStore := license.NewStore(db)
	if err := licStore.Reload(context.Background(), licMgr); err != nil {
		logx.J("license", "initial_load_failed", map[string]any{
			"err":  err.Error(),
			"note": "本副本按未激活（CE 档）启动；Watch 会在恢复后自行收敛",
		})
	}
	// 每个副本都要自己收敛授权状态，**不能只让 leader 跑** ——
	// 非 leader 的副本一样在服务请求，状态停在旧值就会出现"一半请求说未授权"
	licStore.StartWatch(context.Background(), licMgr)
	logx.J("license", "loaded", map[string]any{"status": string(licMgr.Status())})

	srvAPI := api.New(st, cfg, cipher, eng).
		WithLicense(licMgr, licStore).
		WithMetrics(version, cfg.MetricsToken)
	srvAPI.Register(r)
	// 权限映射自检。fail-closed 意味着漏配 = 该接口直接 403，
	// 与其等用户报"页面打不开"，不如启动时就把清单打出来。
	api.AuditPermCheck(r.Routes())

	// 健康与 metrics 独立端口：业务端口被打满时探针仍要能回答，
	// 否则 K8s 会把一个只是慢的实例当成死的，直接重启，问题现场就没了。
	health := gin.New()
	health.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	health.GET("/readyz", func(c *gin.Context) {
		if err := db.PingContext(c.Request.Context()); err != nil {
			c.String(http.StatusServiceUnavailable, "db: %v", err)
			return
		}
		c.String(http.StatusOK, "ok")
	})
	health.GET("/version", func(c *gin.Context) { c.String(http.StatusOK, version) })
	// /metrics 挂在健康端口而不是业务端口：抓取端用不了登录态，
	// 而这个端口本来就只在集群内可达。业务端口在 Ingress 后面，
	// 把 /metrics 放那儿等于把规则名和标签暴露到公网。
	health.GET("/metrics", srvAPI.MetricsHandler)
	if cfg.MetricsToken == "" {
		// ⚠️ 不是错误，但必须说出来：指标里含规则名与用户填的静态标签。
		// 健康端口一旦被 Ingress 或 NodePort 暴露出去，这些就都公开了。
		logx.J("main", "metrics_anonymous", map[string]any{
			"hint": "METRICS_TOKEN 未配置，/metrics 匿名可读。健康端口若对外暴露，请配上"})
	}

	srv := &http.Server{Addr: cfg.Port, Handler: r, ReadHeaderTimeout: 10 * time.Second}
	healthSrv := &http.Server{Addr: cfg.HealthPort, Handler: health, ReadHeaderTimeout: 10 * time.Second}

	go func() {
		if err := healthSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logx.J("main", "health_server_error", map[string]any{"error": err.Error()})
		}
	}()
	go func() {
		logx.Line("main", fmt.Sprintf("OpsAlert backend %s listening on %s", version, cfg.Port))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal(fmt.Sprintf("listen: %v", err))
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logx.Line("main", "收到退出信号，开始优雅停止")
	cancel()

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutCancel()
	_ = srv.Shutdown(shutCtx)
	_ = healthSrv.Shutdown(shutCtx)
	logx.Line("main", "已停止")
}

// seedAdmin 幂等地确保有一个可登录的管理员，并挂到默认租户下。
//
// ⚠️ 每次启动都跑，不能写在「首次初始化」分支里：
// 那样的补数据逻辑对已经存在的安装永远不会执行——全新部署一切正常，
// 升级上来的库里账号继续每个接口 403，而且没有任何报错。
func seedAdmin(st *store.Store) error {
	ctx := context.Background()
	p := st.Platform("seed_admin")

	pw := os.Getenv("ADMIN_PASSWORD")
	if pw == "" {
		pw = "Admin@2026"
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	if _, err := p.Exec(ctx, `INSERT INTO users (username, display_name, password_hash, auth_source, role_code, status)
		VALUES ('admin', '管理员', ?, 'local', 'admin', 'active')
		ON DUPLICATE KEY UPDATE id = id`, string(hash)); err != nil {
		return err
	}
	if _, err := p.Exec(ctx, store.SeedAdminTenantQuery); err != nil {
		return err
	}
	if _, err := p.Exec(ctx, store.BackfillLocalUserTenantsQuery); err != nil {
		return err
	}
	return nil
}

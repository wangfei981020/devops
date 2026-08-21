// ops-access-plane 控制面。
//
// 阶段 0 的服务只提供控制台与门户的 API；网关数据面是独立进程，稍后加。
package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"

	"ops-sso-backend/database/migrations"
	"ops-sso-backend/internal/api"
	"ops-sso-backend/internal/config"
	"ops-sso-backend/internal/domain/access"
	"ops-sso-backend/internal/domain/appcat"
	"ops-sso-backend/internal/domain/approval"
	"ops-sso-backend/internal/domain/auth"
	"ops-sso-backend/internal/domain/idp"
	"ops-sso-backend/internal/domain/mfa"
	"ops-sso-backend/internal/domain/oidcp"
	"ops-sso-backend/internal/domain/pathpolicy"
	"ops-sso-backend/internal/health"
	"ops-sso-backend/internal/license"
	"ops-sso-backend/internal/secrets"
	"ops-sso-backend/internal/store"
	"ops-sso-backend/internal/worker"
	"ops-sso-backend/logx"
)

// 由 Makefile 用 -ldflags 注入。默认值是 dev —— 如果生产上看到 dev，
// 说明构建没走 Makefile，那本身就是个要修的问题。
var (
	version = "dev"
	gitsha  = "unknown"
	edition = "ce"
)

func main() {
	dsn := env("DB_DSN", "")
	if dsn == "" {
		host := env("DB_HOST", "127.0.0.1")
		port := env("DB_PORT", "3306")
		user := env("DB_USER", "access")
		pass := env("DB_PASSWORD", "")
		name := env("DB_NAME", "ops_access_plane")
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
			user, pass, host, port, name)
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		logx.Line("boot", "打开数据库失败: "+err.Error())
		os.Exit(1)
	}
	db.SetMaxOpenConns(32)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(time.Hour)
	// 空闲连接 30 秒就回收。
	//
	// MySQL 的 wait_timeout 是 8 小时，看着很宽 —— 但**中间的网络设备不管这个**：
	// Docker Desktop 的 NAT、云上的 NLB、企业防火墙都会在一两分钟空闲后悄悄丢掉 TCP，
	// 而连接池并不知道，下一次查询才会撞上 EOF。本地实测就是这么炸的：
	// 登录接口隔几分钟必失败一次。让池子自己先回收，比等着撞墙便宜得多。
	db.SetConnMaxIdleTime(30 * time.Second)
	if err := db.Ping(); err != nil {
		logx.Line("boot", "连接数据库失败: "+err.Error())
		os.Exit(1)
	}

	// 迁移在启动时自动跑：客户升级只需换镜像，不需要额外执行一条命令 ——
	// 需要人工执行的步骤，早晚有一次会被漏掉。
	if err := migrations.Run(db); err != nil {
		logx.Line("boot", "迁移失败: "+err.Error())
		os.Exit(1)
	}

	st := store.New(db)

	// 密钥缺失时**拒绝启动**，而不是退化成明文存 —— 静默降级会让人以为自己加密了。
	// 本地开发可以用 make dev-key 生成一个临时密钥。
	box, err := secrets.New(env("OAP_SECRET_KEY", ""))
	if err != nil {
		logx.Line("boot", "密钥未就绪: "+err.Error()+"（设置 OAP_SECRET_KEY 为 64 位十六进制）")
		os.Exit(1)
	}

	// 授权。验签公钥编译在共享内核里（公钥不是秘密，但也不该能被换掉——
	// 早先这里读 OAP_LICENSE_PUBKEY 环境变量，改一个变量就能换成自己的公钥，
	// 然后用自己的私钥签任意 license）。
	//
	// 装载失败**不中断启动**：没有授权的系统仍然要能跑，
	// 访问判定那条路径压根不查 license —— 一个"连不上供应商就把全公司
	// 挡在门外"的系统没人敢装。
	licMgr := license.NewManager()
	licStore := license.NewStore(db)
	if err := licStore.Reload(context.Background(), licMgr); err != nil {
		logx.Line("boot", "授权装载失败，本副本以未授权状态运行（访问判定不受影响）: "+err.Error())
	}
	// 多副本收敛：管理员把激活码装进 A 副本，B、C 还停在旧状态直到各自重启。
	//
	// 不再需要"定期重算过期"那个循环 —— 内核现在是读时求值，
	// 状态随时间自行推进，不依赖任何一次重新装载。
	licStore.StartWatch(context.Background(), licMgr)

	// ★ 签发方标识必须在建 Deps 之前定下来，配错直接拒启。
	//
	// 早先这里是 env("OAP_ISSUER", "http://localhost:18095") —— 生产上忘了配
	// 也会照常启动，然后把 localhost 写进每一个 id_token 的 iss 和
	// discovery 里的 jwks_uri。报错会出现在**下游**（它从自己的容器里
	// 去拉 localhost），排查的人根本不会怀疑到这里。
	runMode := env("RUN_MODE", "dev")
	issuer, issuerWarns, err := config.ResolveIssuer(os.Getenv("OAP_ISSUER"), runMode == "prod")
	if err != nil {
		logx.Line("boot", "拒绝启动："+err.Error())
		logx.Line("boot", "配法：环境变量 OAP_ISSUER=https://<对外域名>（helm 里在 backend.env）")
		os.Exit(1)
	}
	for _, w := range issuerWarns {
		logx.Line("boot", "⚠️ "+w)
	}
	// 每次启动都打出实际生效的值：接入方来问「issuer 填什么」时，
	// 唯一可信的答案是这一行，而不是某份可能过期的文档。
	logx.Line("boot", "OIDC 签发方 issuer="+issuer)

	deps := api.Deps{
		Access:   access.NewRepo(st),
		AppCat:   appcat.NewRepo(st),
		Path:     pathpolicy.NewRepo(st),
		Auth:     auth.New(st),
		MFA:      mfa.NewRepo(st, box),
		IdP:      idp.NewRepo(st, box),
		Approval: approval.NewRepo(st),
		OIDCP:    oidcp.NewRepo(st, box),
		// 对外签发方标识。**必须与下游配置里的 issuer 完全一致**，
		// 且上生产后不能改 —— 改了所有下游的 id_token 校验会全部失败。
		// 解析与自检见上面的 config.ResolveIssuer。
		Issuer:  issuer,
		RunMode: runMode,
		// 审计锚点私钥。没配就不签 —— 但每轮都会记日志，
		// 免得客户以为锚点在工作而实际上从没签过
		AnchorKey:    api.AnchorKeyFromHex(env("OAP_ANCHOR_KEY", "")),
		License:      licMgr,
		LicenseStore: licStore,
		Audit:        api.NewAuditor(st),
		Store:        st,
		Secrets:      box,
		Version:      version,
		GitSHA:       gitsha,
		Edition:      edition,
	}

	mode := runMode
	if mode == "prod" {
		gin.SetMode(gin.ReleaseMode)

		// ★ 生产模式自检：把初装弱口令当**长期口令**用的账号，直接拒启。
		//
		// 一个用 admin123 对外服务的访问控制系统，比没有这套系统更危险 ——
		// 它会让所有人以为访问是受控的。
		// 报错里给出具体账号名，而不是笼统一句"存在弱口令"：
		// 后者会让人在几十个账号里挨个猜。
		//
		// 已标记"必须改密"的账号只是过渡态（它除了改口令什么都干不了），
		// 放行启动但每次都告警 —— 详见 auth.CheckBootstrapPasswords 的注释，
		// 那里写了为什么第一版"一律拒启"是错的。
		scan, err := deps.Auth.CheckBootstrapPasswords(context.Background())
		if err != nil {
			logx.Line("boot", "初装口令自检失败，保守起见拒绝启动: "+err.Error())
			os.Exit(1)
		}
		if len(scan.Settled) > 0 {
			logx.Line("boot", "拒绝启动：以下账号把初装弱口令当成了长期口令 → "+strings.Join(scan.Settled, ", "))
			logx.Line("boot", "改密：oapctl set-password <user_id> <新口令>（至少 12 位）")
			os.Exit(1)
		}
		if len(scan.Pending) > 0 {
			// 放行，但每次启动都喊。受控的过渡态不等于可以忘掉它 ——
			// 一个"临时"状态最常见的结局就是一直留着。
			logx.Line("boot", "⚠️ 以下账号仍是初装口令、等待首次登录改密 → "+strings.Join(scan.Pending, ", "))
			logx.Line("boot", "⚠️ 这些口令是公开的：谁先登进去谁就能改掉它。尽快完成首次改密")
		}
	}
	// 头部冒充身份只给本地联调，且必须显式打开。默认关闭 ——
	// 运维平台踩过的坑是 X-Operator 头可伪造却长期没人发现。
	if env("DEV_HEADER_AUTH", "") == "1" && mode != "prod" {
		api.SetDevHeaderAuth(true)
		logx.Line("boot", "⚠️ 已开启头部冒充认证（仅本地联调）：X-Gate-User-Id / X-Gate-Tenant-Id")
	}

	// 后台任务：到期回收、清理登录中间态、审计锚点。
	// 多副本下用数据库咨询锁选举，拿不到锁的副本跳过本轮。
	jobs := worker.New(db)
	api.RegisterJobs(jobs, deps)
	jobCtx, stopJobs := context.WithCancel(context.Background())
	defer stopJobs()
	jobs.Start(jobCtx)

	r := gin.New()
	r.Use(gin.Recovery())
	api.Register(r, deps)

	// 健康 + 就绪独立端口。契约与 ops-cmdb 一致（:8088 /health /ready），
	// 因为 helm chart 是跨产品统一的 —— 服务适配模板，不是给每个服务改模板。
	go health.Start(env("HEALTH_PORT", ":8088"), db)

	addr := ":" + env("PORT", "8080")
	logx.Line("boot", fmt.Sprintf("ops-access-plane %s (%s, %s) 监听 %s", version, gitsha, edition, addr))

	srv := &http.Server{
		Addr:              addr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logx.Line("boot", "服务退出: "+err.Error())
		os.Exit(1)
	}
}

// 验签公钥与安装指纹都已移入共享内核 ops-kit/licensekit：
//
//	公钥   编译进二进制的常量。原先读 OAP_LICENSE_PUBKEY 环境变量，
//	       改一个变量就能换成自己的公钥再用自己的私钥签任意 license。
//	指纹   licensekit.Fingerprint(install_uuid, @@server_id)，见 internal/license/store.go。
//	       原先是 sha256("oap-install|"+@@server_uuid) 取前 16 字节 ——
//	       与其他产品算出来的不是同一个值，而整份 license 只有一个 install_id。

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

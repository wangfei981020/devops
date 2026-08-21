package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"ops-cmdb-backend/logx"
)

type Config struct {
	Port       string // :8080 业务端口
	HealthPort string // :8088 健康 + /metrics 端口（与 k8sinsight/gke-version 对齐）
	MySQLDSN   string
	JWTSecret  string
	AESKey     string // 凭据/私钥 AES 加密密钥（任意长度，crypto 内部派生 32 字节）
	// 运维平台后端地址，如 http://opsplatform-backend:8080。留空则关闭 portal 登录，
	// 只剩本地 admin 可用（权限校验随之全放行，见 handlers/perm.go 的降级说明）。
	PortalAPIURL string
	SessionHours int // 会话有效期（小时），默认 24
}

// 开发默认密钥。**这两个字符串会随源码公开**（CE 仓是公开仓），
// 所以它们等于没有密钥：拿到源码的人可以签任意会话 token。
const (
	devJWTSecret = "cmdb-dev-jwt-secret-change-in-prod"
	devAESKey    = "cmdb-dev-aes-key-change-in-prod"
)

func Load() *Config {
	c := &Config{
		Port:       getenv("PORT", ":8080"),
		HealthPort: getenv("HEALTH_PORT", ":8088"),
		MySQLDSN:   buildDSN(),
		JWTSecret:  getenv("JWT_SECRET", devJWTSecret),
		AESKey:     getenv("CMDB_AES_KEY", devAESKey),

		PortalAPIURL: getenv("PORTAL_API_URL", ""),
		SessionHours: getenvInt("SESSION_HOURS", 24),
	}
	checkSecrets(c)
	return c
}

// checkSecrets 用着开发默认密钥就不许在生产启动。
//
// # 为什么必须是硬失败
//
// 这两个默认值让服务**能正常跑起来**：登录能登、页面能开、监控全绿。
// 唯一的区别是签名密钥写在源码里 —— 而 CE 仓是要公开的，
// 等于任何人都能伪造一个管理员会话。这种"看起来完全正常"的失败
// 不可能被巡检发现，只会在出事之后才知道。
//
// AES 密钥更狠一层：它加密的是云账号凭据和证书私钥。
// 用默认值跑一段时间之后再换成真密钥，**库里已存的凭据全部解不开**，
// 而那时人已经录了几十条。所以必须在第一次启动就拦住。
//
// # 为什么用 OPS_ENV 而不是"猜"
//
// 靠"是不是在 K8s 里"之类的启发式判断会两头出错：
// 本地 kind 集群被判成生产（跑不起来），生产裸机被判成开发（静默放行）。
// 显式声明才有确定的行为。默认按**生产**处理 —— 忘了配的那次必须是响的。
func checkSecrets(c *Config) {
	if getenv("OPS_ENV", "prod") == "dev" {
		if c.JWTSecret == devJWTSecret || c.AESKey == devAESKey {
			logx.Line("config", "WARN 正在使用开发默认密钥（OPS_ENV=dev）。生产必须注入 JWT_SECRET 与 CMDB_AES_KEY")
		}
		return
	}
	var bad []string
	if c.JWTSecret == devJWTSecret {
		bad = append(bad, "JWT_SECRET")
	}
	if c.AESKey == devAESKey {
		bad = append(bad, "CMDB_AES_KEY")
	}
	if len(bad) > 0 {
		fmt.Fprintf(os.Stderr,
			"启动中止：%v 仍是源码里的开发默认值。\n"+
				"  这两个默认值不会让服务报错——登录能登、页面能开、监控全绿，\n"+
				"  只是签名/加密密钥写在公开源码里，等于任何人都能伪造管理员会话。\n"+
				"  ⚠️ CMDB_AES_KEY 加密的是云账号凭据与证书私钥：先用默认值跑一段时间\n"+
				"     再换成真密钥，库里已存的凭据将全部解不开。必须一开始就配对。\n"+
				"  请在 Secret 里注入这些变量；本地开发请设 OPS_ENV=dev。\n",
			bad)
		os.Exit(1)
	}
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// buildDSN 从 MYSQL_* 拼 DSN；默认连本地 mysql-deploy 的 cmdb 库。
func buildDSN() string {
	h := getenv("MYSQL_HOST", "127.0.0.1")
	p := getenv("MYSQL_PORT", "3306")
	u := getenv("MYSQL_USER", "cmdb_user")
	pw := os.Getenv("MYSQL_PASSWORD")
	db := getenv("MYSQL_DATABASE", "cmdb")
	// loc=Local：驱动按容器时区(TZ=Asia/Manila)解析 DATETIME；
	// time_zone='+08:00'：让 MySQL 会话 NOW() 按马尼拉(UTC+8)返回，存/读一致。
	//
	// timeout/readTimeout/writeTimeout 是 2026-07-31 故障的直接教训（CMDB-012）：
	// MySQL 数据盘写满后挂在「等磁盘空间写 binlog」上，此前 DSN 没有任何超时，
	// 查询就无限期等下去、永不返回，连接池 10 条被占死后**每一个查库接口都卡住**，
	// 对外表现成 Cloudflare 524。有了读超时，卡死的查询会在 30s 内失败并释放连接，
	// 接口快速报错而不是拖成 524——故障依然是故障，但可见、可诊断、不放大。
	// 30s 的取值：实测最慢的采集查询约 1.3s，正常查询远低于此，30s 只兜异常。
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local&time_zone=%s"+
		"&timeout=5s&readTimeout=30s&writeTimeout=30s",
		u, pw, h, p, db, url.QueryEscape("'+08:00'"))
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

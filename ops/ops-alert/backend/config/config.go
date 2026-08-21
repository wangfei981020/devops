package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"
)

type Config struct {
	// Location 业务时区。影响的是**墙上时钟判定**：日报几点发、
	// 日报里「昨天」的边界。时间点类的东西（查询窗口、时序分桶）
	// 用的是 Unix 时刻，与时区无关，不受它影响。
	//
	// ⚠️ 界面上的时间显示**不看这个**，走浏览器本地时区 —— 那是对的：
	// 在马尼拉值班的人该看到马尼拉时间，而不是被强制显示上海时间。
	Location *time.Location
	// MetricsToken 保护健康端口上的 /metrics。空 = 匿名可读（内网默认可接受）
	MetricsToken string
	Port         string // :8080 业务端口
	HealthPort   string // :8088 健康 + /metrics 端口（与其余 ops 服务对齐）
	MySQLDSN     string
	JWTSecret    string
	AESKey       string // 数据源与渠道凭据的 AES 密钥（任意长度，crypto 内部派生 32 字节）

	SessionHours int // 会话有效期（小时）

	// 检测引擎的全局并发闸。所有规则的数据源查询共用这个信号量：
	// 单条规则的并发再小，几十条规则同时到点也会把数据源打垮，
	// 而数据源被打垮的表现是「规则查询超时」——看起来像我们自己的 bug。
	QueryConcurrency int
	// 调度租约名。多副本时只有持有租约的副本跑检测，其余待命。
	// 用租约而非 leader 选举库：持有者被 kill 后租约自然过期，不会永久卡死。
	LeaseName string
}

func Load() *Config {
	// 时区先解析出来：DSN 要用它推 MySQL 会话的偏移量，
	// 两边不一致的话所有时间戳会整体偏移几小时而不报错
	loc := mustLocation(getenv("TZ", "Asia/Shanghai"))
	return &Config{
		Port:         getenv("PORT", ":8080"),
		HealthPort:   getenv("HEALTH_PORT", ":8088"),
		Location:     loc,
		MetricsToken: os.Getenv("METRICS_TOKEN"),
		MySQLDSN:     buildDSN(loc),
		JWTSecret:    getenv("JWT_SECRET", "alert-dev-jwt-secret-change-in-prod"),
		AESKey:       getenv("ALERT_AES_KEY", "alert-dev-aes-key-change-in-prod"),

		SessionHours:     getenvInt("SESSION_HOURS", 24),
		QueryConcurrency: getenvInt("QUERY_CONCURRENCY", 16),
		LeaseName:        getenv("LEASE_NAME", "detect-engine"),
	}
}

// buildDSN 从 MYSQL_* 拼 DSN。
//
// readTimeout 不是可选项：没有它时，数据库端卡住（磁盘写满、锁等待）会让查询
// 无限期等下去，连接池占满后每一个查库接口一起卡死，对外表现成网关 524。
// 有了读超时，卡死的查询会失败并释放连接——故障依然是故障，但可见、可诊断、不放大。
func buildDSN(loc *time.Location) string {
	h := getenv("MYSQL_HOST", "127.0.0.1")
	p := getenv("MYSQL_PORT", "3306")
	u := getenv("MYSQL_USER", "alert_user")
	pw := os.Getenv("MYSQL_PASSWORD")
	db := getenv("MYSQL_DATABASE", "opsalert")
	// ⚠️ Go 侧的 loc 和 MySQL 侧的 time_zone **必须一致**，否则读回来的
	// DATETIME 会被按错误的时区解释，整套时间戳偏移几小时而不报任何错。
	//
	// MySQL 的 time_zone 用**偏移量**而不是时区名：用名字要求 MySQL 装了
	// 时区表（多数托管实例没装），装不了的话连接直接失败。
	//
	// ⚠️ 偏移量在**启动时**按当前时刻算一次。对有夏令时的时区，
	// 一年会有两次偏移变化而进程里这个值不会跟着变 —— 那时 MySQL 的 NOW()
	// 会与 Go 差一小时，直到重启。默认的 Asia/Shanghai 不过夏令时，
	// 所以默认配置没有这个问题；选了会过夏令时的时区时启动日志会警告。
	_, offset := time.Now().In(loc).Zone()
	sign, abs := "+", offset
	if offset < 0 {
		sign, abs = "-", -offset
	}
	tz := fmt.Sprintf("'%s%02d:%02d'", sign, abs/3600, (abs%3600)/60)
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=%s&time_zone=%s"+
		"&timeout=5s&readTimeout=30s&writeTimeout=30s",
		u, pw, h, p, db, url.QueryEscape(loc.String()), url.QueryEscape(tz))
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// mustLocation 解析时区名。
//
// 🔴 名字不认识时**直接退出，不能退回 UTC**。
//
// 退回 UTC 的后果是日报在错误的时间发出去（上海配 09:00 会变成 17:00），
// 而没有任何一处会说"时区没解析成功"——界面上配置显示 09:00，
// 日志里一切正常，只有收件人觉得奇怪。启动就失败反而是最便宜的发现方式。
//
// ⚠️ 依赖镜像里的 tzdata（Dockerfile 装了）。剥掉 tzdata 的话
// 除 UTC 外所有名字都会解析失败，而那正是这里要拦住的情况。
func mustLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"✗ 时区 %q 无法解析: %v\n  TZ 必须是 IANA 名字（如 Asia/Shanghai、Asia/Manila、Europe/London）。\n"+
				"  这里不退回 UTC —— 那会让日报在错误的时间发出去且无人察觉。\n", name, err)
		os.Exit(1)
	}
	return loc
}

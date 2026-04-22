package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port        string // ":8080"
	HealthPort  string // ":8081"
	MySQLDSN    string // 由 MYSQL_HOST/PORT/USER/PASSWORD/DATABASE 拼出；兼容直接给 MYSQL_DSN 的老配置
	AESKey      string // base64 decoded -> 32 bytes
	CORSOrigin  string // "*"
	GitCacheDir string // "/app/git-cache" (本地调试用 ./git-cache)

	// 认证 / SSO
	JWTSecret       string // JWT HS256 签名密钥
	SessionTimeout  int    // 分钟
	PortalAPIURL    string // 运维平台后端地址，空则禁用 SSO
	AppPortalSecret string // 运维平台跳转 token 的 AES 解密 secret（留空走明文 JWT）
	CookieSecure    bool
	CookieSameSite  string
}

func Load() *Config {
	sessionTimeout, _ := strconv.Atoi(getEnv("SESSION_TIMEOUT", "180"))
	appEnv := getEnv("APP_ENV", "prod")
	isDev := appEnv == "dev"

	c := &Config{
		Port:        getEnv("PORT", ":8080"),
		HealthPort:  getEnv("HEALTH_PORT", ":8088"),
		MySQLDSN:    buildMySQLDSN(),
		AESKey:      getEnv("AES_KEY", ""),
		CORSOrigin:  getEnv("CORS_ORIGIN", ""),
		GitCacheDir: getEnv("GIT_CACHE_DIR", "./git-cache"),

		JWTSecret:       getEnv("JWT_SECRET", ""),
		SessionTimeout:  sessionTimeout,
		PortalAPIURL:    getEnv("PORTAL_API_URL", ""),
		AppPortalSecret: getEnv("APP_PORTAL_SECRET", ""),
		CookieSecure:    getEnv("COOKIE_SECURE", "true") == "true",
		CookieSameSite:  getEnv("COOKIE_SAMESITE", "strict"),
	}

	// fail-fast: 生产环境必须显式提供 AES_KEY 和 JWT_SECRET
	if c.AESKey == "" {
		if !isDev {
			fmt.Fprintln(os.Stderr, "FATAL: AES_KEY not set. Set APP_ENV=dev to use a dev fallback.")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "WARN: AES_KEY not set, using dev-only fallback (APP_ENV=dev)")
		c.AESKey = "ZGV2LW9ubHktZmFsbGJhY2stZG8tbm90LXVzZS1wcm9k"
	}
	if c.JWTSecret == "" {
		if !isDev {
			fmt.Fprintln(os.Stderr, "FATAL: JWT_SECRET not set. Set APP_ENV=dev to use a dev fallback.")
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "WARN: JWT_SECRET not set, using dev-only fallback (APP_ENV=dev)")
		c.JWTSecret = "dev-secret-key-change-in-production"
	}
	if c.CORSOrigin == "" {
		if !isDev {
			fmt.Fprintln(os.Stderr, "FATAL: CORS_ORIGIN not set. Specify the allowed origin or set APP_ENV=dev.")
			os.Exit(1)
		}
		c.CORSOrigin = "*"
	}
	return c
}

// buildMySQLDSN 优先从 MYSQL_HOST/PORT/USER/PASSWORD/DATABASE 拼 DSN；
// 若都没设置则 fallback 到 MYSQL_DSN（老配置兼容）。
func buildMySQLDSN() string {
	host := os.Getenv("MYSQL_HOST")
	if host == "" {
		if dsn := os.Getenv("MYSQL_DSN"); dsn != "" {
			return dsn
		}
		host = "127.0.0.1"
	}
	port := getEnv("MYSQL_PORT", "3306")
	user := getEnv("MYSQL_USER", "deploy_user")
	pwd := getEnv("MYSQL_PASSWORD", "")
	db := getEnv("MYSQL_DATABASE", "deploy_center")
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&charset=utf8mb4&loc=Local",
		user, pwd, host, port, db)
}

func getEnv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func MustInt(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

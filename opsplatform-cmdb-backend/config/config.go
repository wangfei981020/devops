package config

import (
	"fmt"
	"net/url"
	"os"
)

type Config struct {
	Port       string // :8080 业务端口
	HealthPort string // :8088 健康 + /metrics 端口（与 k8sinsight/gke-version 对齐）
	MySQLDSN   string
	JWTSecret  string
	AESKey     string // 凭据/私钥 AES 加密密钥（任意长度，crypto 内部派生 32 字节）
}

func Load() *Config {
	return &Config{
		Port:       getenv("PORT", ":8080"),
		HealthPort: getenv("HEALTH_PORT", ":8088"),
		MySQLDSN:   buildDSN(),
		JWTSecret:  getenv("JWT_SECRET", "cmdb-dev-jwt-secret-change-in-prod"),
		AESKey:     getenv("CMDB_AES_KEY", "cmdb-dev-aes-key-change-in-prod"),
	}
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
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local&time_zone=%s",
		u, pw, h, p, db, url.QueryEscape("'+08:00'"))
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

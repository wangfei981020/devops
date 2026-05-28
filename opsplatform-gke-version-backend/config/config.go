package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port       string // ":8080" 业务端口
	HealthPort string // ":8088" 健康检查 + Prometheus 指标，与发布系统对齐
	MySQLDSN   string
}

func Load() *Config {
	return &Config{
		Port:       getenv("PORT", ":8080"),
		HealthPort: getenv("HEALTH_PORT", ":8088"),
		MySQLDSN:   buildMySQLDSN(),
	}
}

// buildMySQLDSN 优先从 MYSQL_HOST/PORT/USER/PASSWORD/DATABASE 拼 DSN；
// 若都没设置则 fallback 到 MYSQL_DSN（老配置兼容）。
func buildMySQLDSN() string {
	host := os.Getenv("MYSQL_HOST")
	if host == "" {
		if d := os.Getenv("MYSQL_DSN"); d != "" {
			return d
		}
		host = "127.0.0.1"
	}
	port := getenv("MYSQL_PORT", "3306")
	user := getenv("MYSQL_USER", "root")
	pwd := getenv("MYSQL_PASSWORD", "")
	db := getenv("MYSQL_DATABASE", "gke_version_monitor")
	return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&charset=utf8mb4",
		user, pwd, host, port, db)
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

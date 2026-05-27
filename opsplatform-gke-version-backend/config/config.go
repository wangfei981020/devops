package config

import "os"

type Config struct {
	Port     string
	MySQLDSN string
}

func Load() *Config {
	return &Config{
		Port:     getenv("PORT", "8080"),
		MySQLDSN: getenv("MYSQL_DSN", "root:123456@tcp(mysql-deploy.opsplatform:3306)/gke_version_monitor?parseTime=true&charset=utf8mb4"),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

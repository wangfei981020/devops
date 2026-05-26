package config

import (
	"log"
	"os"
)

type Config struct {
	Port          string
	MySQLDSN      string
	SAKeyPath     string
	GoogleADCAuto bool
}

func Load() *Config {
	cfg := &Config{
		Port:      getenv("PORT", "8080"),
		MySQLDSN:  getenv("MYSQL_DSN", "root:123456@tcp(mysql-deploy.opsplatform:3306)/gke_version_monitor?parseTime=true&charset=utf8mb4"),
		SAKeyPath: getenv("GOOGLE_APPLICATION_CREDENTIALS", "/secrets/key.json"),
	}
	if _, err := os.Stat(cfg.SAKeyPath); err == nil {
		os.Setenv("GOOGLE_APPLICATION_CREDENTIALS", cfg.SAKeyPath)
		cfg.GoogleADCAuto = true
	} else {
		log.Printf("warn: SA key not found at %s, GCP client may fail", cfg.SAKeyPath)
	}
	return cfg
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

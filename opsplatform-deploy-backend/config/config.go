package config

import (
	"os"
)

type Config struct {
	// Server
	Port       string
	HealthPort string
	CORSOrigin string
	DevMode    bool

	// MySQL
	MySQLHost     string
	MySQLPort     string
	MySQLUser     string
	MySQLPassword string
	MySQLDatabase string

	// AES 加密 Key (用于加密 token 等敏感字段)
	AESKey string

	// Git 本地工作目录 (clone 仓库的临时位置)
	GitWorkDir string
}

func Load() *Config {
	return &Config{
		Port:       getEnv("PORT", ":8080"),
		HealthPort: getEnv("HEALTH_PORT", ":8088"),
		CORSOrigin: getEnv("CORS_ALLOWED_ORIGIN", "*"),
		DevMode:    getEnv("DEV_MODE", "false") == "true",

		MySQLHost:     getEnv("MYSQL_HOST", "localhost"),
		MySQLPort:     getEnv("MYSQL_PORT", "3306"),
		MySQLUser:     getEnv("MYSQL_USER", "root"),
		MySQLPassword: getEnv("MYSQL_PASSWORD", ""),
		MySQLDatabase: getEnv("MYSQL_DATABASE", "deploy_center"),

		AESKey: getEnv("AES_KEY", "DeployPlatform@2026#DefaultKey32Byte!"),

		GitWorkDir: getEnv("GIT_WORK_DIR", "/tmp/deploy-git"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

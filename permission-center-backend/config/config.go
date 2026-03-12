package config

import "os"

var (
	Port = getEnv("PC_PORT", ":9100")

	// MySQL
	DBHost     = getEnv("PC_DB_HOST", "mysql-sso.sso.svc.cluster.local")
	DBPort     = getEnv("PC_DB_PORT", "3306")
	DBUser     = getEnv("PC_DB_USER", "sso_user")
	DBPassword = getEnv("PC_DB_PASSWORD", "Sso@2026#Secure")
	DBName     = getEnv("PC_DB_NAME", "permission_center")

	// Redis
	RedisHost     = getEnv("PC_REDIS_HOST", "redis-pc.sso.svc.cluster.local")
	RedisPort     = getEnv("PC_REDIS_PORT", "6379")
	RedisPassword = getEnv("PC_REDIS_PASSWORD", "PcRedis@2026#Secure")
	RedisDB       = getEnv("PC_REDIS_DB", "0")

	// SSO userinfo endpoint for token validation
	SSOUserinfoURL = getEnv("PC_SSO_USERINFO_URL", "http://sso-backend.sso.svc.cluster.local:9000/userinfo")

	// Default admin
	DefaultAdminPassword = getEnv("PC_ADMIN_PASSWORD", "admin123")
)

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

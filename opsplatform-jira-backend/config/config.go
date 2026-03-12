package config

import "os"

var (
	Port    = getEnv("PORT", ":8080")
	DevMode = getEnv("DEV_MODE", "false")

	// MySQL
	DBHost     = getEnv("MYSQL_HOST", "localhost")
	DBPort     = getEnv("MYSQL_PORT", "3306")
	DBUser     = getEnv("MYSQL_USER", "root")
	DBPassword = getEnv("MYSQL_PASSWORD", "")
	DBName     = getEnv("MYSQL_DATABASE", "jira_center")

	// JWT
	JWTSecret      = getEnv("JWT_SECRET", "")
	SessionTimeout = getEnv("SESSION_TIMEOUT", "30")

	// Redis (optional)
	RedisHost     = getEnv("REDIS_HOST", "")
	RedisPort     = getEnv("REDIS_PORT", "6379")
	RedisPassword = getEnv("REDIS_PASSWORD", "")
	RedisDB       = getEnv("REDIS_DB", "0")

	// Cookie
	CookieSecure   = getEnv("COOKIE_SECURE", "false")
	CookieSameSite = getEnv("COOKIE_SAMESITE", "lax")

	// Portal (运维平台) 认证
	PortalAPIURL = getEnv("PORTAL_API_URL", "http://opsplatform-backend.opsplatform.svc.cluster.local:8080")

	// CORS
	CORSAllowedOrigin = getEnv("CORS_ALLOWED_ORIGIN", "*")
)

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

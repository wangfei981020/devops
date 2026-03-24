package config

import (
	"os"
	"strconv"
)

type Config struct {
	// Server
	Port        string
	DevMode     bool
	HealthPort  string
	CORSOrigin  string

	// MySQL
	MySQLHost     string
	MySQLPort     string
	MySQLUser     string
	MySQLPassword string
	MySQLDatabase string

	// Redis
	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDB       int

	// JWT
	JWTSecret      string
	SessionTimeout int // minutes

	// Portal
	PortalAPIURL string

	// Cookie
	CookieSecure   bool
	CookieSameSite string
}

func Load() *Config {
	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))
	sessionTimeout, _ := strconv.Atoi(getEnv("SESSION_TIMEOUT", "180"))

	return &Config{
		Port:        getEnv("PORT", ":8080"),
		DevMode:     getEnv("DEV_MODE", "false") == "true",
		HealthPort:  getEnv("HEALTH_PORT", ":8088"),
		CORSOrigin:  getEnv("CORS_ALLOWED_ORIGIN", "*"),

		MySQLHost:     getEnv("MYSQL_HOST", "localhost"),
		MySQLPort:     getEnv("MYSQL_PORT", "3306"),
		MySQLUser:     getEnv("MYSQL_USER", "root"),
		MySQLPassword: getEnv("MYSQL_PASSWORD", ""),
		MySQLDatabase: getEnv("MYSQL_DATABASE", "alert_center"),

		RedisHost:     getEnv("REDIS_HOST", "localhost"),
		RedisPort:     getEnv("REDIS_PORT", "6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       redisDB,

		JWTSecret:      getEnv("JWT_SECRET", "dev-secret-key-change-in-production"),
		SessionTimeout: sessionTimeout,

		PortalAPIURL: getEnv("PORTAL_API_URL", ""),

		CookieSecure:   getEnv("COOKIE_SECURE", "false") == "true",
		CookieSameSite: getEnv("COOKIE_SAMESITE", "lax"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

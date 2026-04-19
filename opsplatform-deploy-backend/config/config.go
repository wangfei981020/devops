package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port        string // ":8080"
	HealthPort  string // ":8081"
	MySQLDSN    string
	AESKey      string // base64 decoded -> 32 bytes
	CORSOrigin  string // "*"
	GitCacheDir string // "/app/git-cache" (本地调试用 ./git-cache)
}

func Load() *Config {
	c := &Config{
		Port:        getEnv("PORT", ":8080"),
		HealthPort:  getEnv("HEALTH_PORT", ":8081"),
		MySQLDSN:    getEnv("MYSQL_DSN", "deploy_user:123123@tcp(localhost:13307)/deploy_center?parseTime=true&charset=utf8mb4&loc=Local"),
		AESKey:      getEnv("AES_KEY", ""),
		CORSOrigin:  getEnv("CORS_ORIGIN", "*"),
		GitCacheDir: getEnv("GIT_CACHE_DIR", "./git-cache"),
	}
	if c.AESKey == "" {
		fmt.Fprintln(os.Stderr, "WARN: AES_KEY not set, using dev-only fallback (DO NOT USE IN PROD)")
		c.AESKey = "ZGV2LW9ubHktZmFsbGJhY2stZG8tbm90LXVzZS1wcm9k" // base64 "dev-only-fallback-do-not-use-prod"
	}
	return c
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

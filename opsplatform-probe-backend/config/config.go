package config

import (
	"os"
	"strconv"
)

type Config struct {
	// Server
	Port       string
	HealthPort string
	CORSOrigin string

	// MySQL
	MySQLHost     string
	MySQLPort     string
	MySQLUser     string
	MySQLPassword string
	MySQLDatabase string

	// JWT
	JWTSecret      string
	SessionTimeout int // minutes

	// Portal SSO
	PortalAPIURL string

	// Cookie
	CookieSecure   bool
	CookieSameSite string

	// MinIO
	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
	MinIOBucket    string
	MinIOUseSSL    bool
	MinIORegion    string

	// Agent runtime
	AgentHeartbeatTimeout int // seconds, after which agent is marked offline
	AgentTaskFetchLimit   int // max tasks an agent fetches per pull
	AgentApprovalRequired bool
	AgentTokenExpireDays  int // 0 = never expire

	// Image import (for crane)
	ImageRegistryUsername string
	ImageRegistryPassword string
	ImageBinaryPath       string // path inside the image to extract, default /app/probe-agent

	// Security
	DisableDefaultAdmin  bool
	DefaultAdminPassword string // if set (and not disabled), used as initial admin password

	// Code signing for agent upgrades
	RequireSignedUploads bool   // 强制上传时必须带 signature
	AgentPublicKeyPEM    string // 可选: 后端记录的公钥, 用于上传时校验签名 (与 Agent 内置的公钥应一致)

	// Rate limits
	RLRegisterPerMin int // per IP
	RLReportPerSec   int // per agent id
}

func Load() *Config {
	sessionTimeout, _ := strconv.Atoi(getEnv("SESSION_TIMEOUT", "180"))
	hbTimeout, _ := strconv.Atoi(getEnv("AGENT_HEARTBEAT_TIMEOUT", "30"))
	taskLimit, _ := strconv.Atoi(getEnv("AGENT_TASK_FETCH_LIMIT", "20"))
	tokenExpireDays, _ := strconv.Atoi(getEnv("AGENT_TOKEN_EXPIRE_DAYS", "0"))
	rlRegister, _ := strconv.Atoi(getEnv("RATE_LIMIT_REGISTER_PER_MIN", "10"))
	rlReport, _ := strconv.Atoi(getEnv("RATE_LIMIT_REPORT_PER_SEC", "50"))

	return &Config{
		Port:       getEnv("PORT", ":8080"),
		HealthPort: getEnv("HEALTH_PORT", ":8088"),
		CORSOrigin: getEnv("CORS_ALLOWED_ORIGIN", ""),

		MySQLHost:     getEnv("MYSQL_HOST", "localhost"),
		MySQLPort:     getEnv("MYSQL_PORT", "3306"),
		MySQLUser:     getEnv("MYSQL_USER", "root"),
		MySQLPassword: getEnv("MYSQL_PASSWORD", ""),
		MySQLDatabase: getEnv("MYSQL_DATABASE", "opsplatform_probe"),

		JWTSecret:      getEnv("JWT_SECRET", "dev-secret-key-change-in-production"),
		SessionTimeout: sessionTimeout,

		PortalAPIURL: getEnv("PORTAL_API_URL", ""),

		CookieSecure:   getEnv("COOKIE_SECURE", "false") == "true",
		CookieSameSite: getEnv("COOKIE_SAMESITE", "strict"),

		MinIOEndpoint:  getEnv("MINIO_ENDPOINT", ""),
		MinIOAccessKey: getEnv("MINIO_ACCESS_KEY", ""),
		MinIOSecretKey: getEnv("MINIO_SECRET_KEY", ""),
		MinIOBucket:    getEnv("MINIO_BUCKET", "opsplatform-probe-agent"),
		MinIOUseSSL:    getEnv("MINIO_USE_SSL", "false") == "true",
		MinIORegion:    getEnv("MINIO_REGION", "us-east-1"),

		AgentHeartbeatTimeout: hbTimeout,
		AgentTaskFetchLimit:   taskLimit,
		AgentApprovalRequired: getEnv("AGENT_APPROVAL_REQUIRED", "true") == "true",
		AgentTokenExpireDays:  tokenExpireDays,

		ImageRegistryUsername: getEnv("IMAGE_REGISTRY_USERNAME", ""),
		ImageRegistryPassword: getEnv("IMAGE_REGISTRY_PASSWORD", ""),
		ImageBinaryPath:       getEnv("IMAGE_BINARY_PATH", "app/probe-agent"),

		DisableDefaultAdmin:  getEnv("DISABLE_DEFAULT_ADMIN", "false") == "true",
		DefaultAdminPassword: getEnv("DEFAULT_ADMIN_PASSWORD", ""),

		RequireSignedUploads: getEnv("REQUIRE_SIGNED_UPLOADS", "false") == "true",
		AgentPublicKeyPEM:    getEnv("AGENT_PUBLIC_KEY_PEM", ""),

		RLRegisterPerMin: rlRegister,
		RLReportPerSec:   rlReport,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

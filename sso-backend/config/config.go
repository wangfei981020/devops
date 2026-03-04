package config

import "os"

var (
	Port           = getEnv("SSO_PORT", ":9000")
	Issuer         = getEnv("SSO_ISSUER", "http://localhost:9000")
	InternalIssuer = getEnv("SSO_INTERNAL_ISSUER", "")

	// MySQL
	DBHost     = getEnv("SSO_DB_HOST", "mysql-sso.sso.svc.cluster.local")
	DBPort     = getEnv("SSO_DB_PORT", "3306")
	DBUser     = getEnv("SSO_DB_USER", "sso_user")
	DBPassword = getEnv("SSO_DB_PASSWORD", "Sso@2026#Secure")
	DBName     = getEnv("SSO_DB_NAME", "sso")

	// JWT
	JWTSecret = getEnv("SSO_JWT_SECRET", "sso-jwt-secret-key-min-32-chars!!")

	// RSA Key paths (optional - auto-generate if not set)
	RSAPrivateKeyPath = getEnv("SSO_RSA_PRIVATE_KEY_PATH", "")

	// Default admin
	DefaultAdminPassword = getEnv("SSO_ADMIN_PASSWORD", "admin123")
)

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

// GetInternalIssuer returns internal issuer for backend-to-backend calls
func GetInternalIssuer() string {
	if InternalIssuer != "" {
		return InternalIssuer
	}
	return Issuer
}

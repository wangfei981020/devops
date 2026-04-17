package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

func Init() {
	host := getEnv("MYSQL_HOST", "localhost")
	port := getEnv("MYSQL_PORT", "3306")
	user := getEnv("MYSQL_USER", "")
	password := getEnv("MYSQL_PASSWORD", "")
	dbName := getEnv("MYSQL_DATABASE", "domain_platform")
	devMode := getEnv("DEV_MODE", "false")

	if devMode == "true" {
		if user == "" {
			user = "root"
		}
		if password == "" {
			password = "123456"
		}
	}

	// First connect WITHOUT database to create it if needed
	dsnNoDB := fmt.Sprintf("%s:%s@tcp(%s:%s)/?charset=utf8mb4&parseTime=True&loc=Local",
		user, password, host, port)

	db, err := sql.Open("mysql", dsnNoDB)
	if err != nil {
		log.Fatalf("Failed to open MySQL connection: %v", err)
	}

	_, err = db.Exec(fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci",
		dbName,
	))
	if err != nil {
		log.Fatalf("Failed to create database: %v", err)
	}
	db.Close()

	// Reconnect with database name
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		user, password, host, port, dbName)

	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Failed to open MySQL connection with db: %v", err)
	}

	if err = DB.Ping(); err != nil {
		log.Fatalf("Failed to ping MySQL: %v", err)
	}

	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(5)

	createTables()
	migrateColumns()
	log.Println("Database initialized successfully")
}

func createTables() {
	tables := []string{
		`CREATE TABLE IF NOT EXISTS registrar_configs (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			provider VARCHAR(32) NOT NULL,
			api_key VARCHAR(512),
			api_secret VARCHAR(512),
			api_user VARCHAR(255),
			api_token VARCHAR(512),
			base_url VARCHAR(512),
			status VARCHAR(32) DEFAULT 'active',
			last_sync DATETIME,
			created_at DATETIME,
			updated_at DATETIME
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS registrar_domains (
			id VARCHAR(64) PRIMARY KEY,
			config_id VARCHAR(64),
			provider VARCHAR(32),
			root_domain VARCHAR(512),
			host VARCHAR(255),
			full_domain VARCHAR(512),
			record_type VARCHAR(16),
			record_value TEXT,
			expire_time DATE,
			cert_expire_time DATE,
			module VARCHAR(255),
			origin VARCHAR(512),
			origin_ip VARCHAR(255),
			cdn_provider VARCHAR(128),
			status VARCHAR(32) DEFAULT 'active',
			remark TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			INDEX idx_full_domain (full_domain(191)),
			INDEX idx_root_domain (root_domain(191)),
			INDEX idx_config_id (config_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS global_settings (
			setting_key VARCHAR(128) PRIMARY KEY,
			setting_value TEXT,
			updated_at DATETIME
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		`CREATE TABLE IF NOT EXISTS domain_overrides (
			id VARCHAR(64) PRIMARY KEY,
			root_domain VARCHAR(512) UNIQUE,
			exclude_hosts TEXT,
			created_at DATETIME,
			updated_at DATETIME
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}

	for _, q := range tables {
		if _, err := DB.Exec(q); err != nil {
			log.Fatalf("Failed to create table: %v\nSQL: %s", err, q)
		}
	}
}

func migrateColumns() {
	migrations := []string{
		"ALTER TABLE registrar_domains ADD COLUMN project VARCHAR(255) DEFAULT '' AFTER provider",
		"ALTER TABLE registrar_domains ADD COLUMN env VARCHAR(64) DEFAULT '' AFTER project",
		"ALTER TABLE registrar_domains ADD COLUMN source VARCHAR(32) DEFAULT '' AFTER env",
	}
	for _, sql := range migrations {
		DB.Exec(sql) // ignore error if column already exists
	}
}

func Close() {
	if DB != nil {
		DB.Close()
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

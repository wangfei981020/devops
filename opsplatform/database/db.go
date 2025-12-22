package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

// DB 数据库连接
var DB *sql.DB

// InitDB 初始化数据库连接
func InitDB() error {
	// 从环境变量获取配置
	host := getEnv("MYSQL_HOST", "localhost")
	port := getEnv("MYSQL_PORT", "3306")
	user := getEnv("MYSQL_USER", "root")
	password := getEnv("MYSQL_PASSWORD", "123456")
	dbname := getEnv("MYSQL_DATABASE", "netdoc")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local&collation=utf8mb4_unicode_ci",
		user, password, host, port, dbname)

	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("连接数据库失败: %v", err)
	}

	if err = DB.Ping(); err != nil {
		return fmt.Errorf("数据库连接测试失败: %v", err)
	}

	// 设置连接字符集
	if _, err = DB.Exec("SET NAMES utf8mb4"); err != nil {
		log.Printf("设置字符集失败: %v", err)
	}

	// 创建表
	if err = createTables(); err != nil {
		return fmt.Errorf("创建表失败: %v", err)
	}

	log.Println("数据库连接成功")
	return nil
}

func createTables() error {
	// 创建记录表
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS records (
			id VARCHAR(64) PRIMARY KEY,
			connection_id VARCHAR(128) NOT NULL,
			project VARCHAR(255) NOT NULL,
			env VARCHAR(32) NOT NULL,
			vid VARCHAR(255) NOT NULL,
			src_ip VARCHAR(64) NOT NULL,
			dest_ip VARCHAR(64) NOT NULL,
			port VARCHAR(32) NOT NULL,
			status VARCHAR(32) DEFAULT 'active',
			operator VARCHAR(128),
			created_at DATETIME,
			updated_at DATETIME,
			created_by VARCHAR(128),
			updated_by VARCHAR(128),
			UNIQUE KEY uk_connection_id (connection_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 检查并添加 connection_id 列（兼容旧数据库）
	DB.Exec(`ALTER TABLE records ADD COLUMN connection_id VARCHAR(128) NOT NULL DEFAULT '' AFTER id`)
	DB.Exec(`ALTER TABLE records ADD UNIQUE KEY uk_connection_id (connection_id)`)

	// 创建用户表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id VARCHAR(64) PRIMARY KEY,
			username VARCHAR(128) NOT NULL UNIQUE,
			password VARCHAR(255) NOT NULL,
			display_name VARCHAR(128),
			role VARCHAR(32) DEFAULT 'user',
			status VARCHAR(32) DEFAULT 'active',
			permissions TEXT,
			created_at DATETIME
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建审计日志表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS audit_logs (
			id VARCHAR(64) PRIMARY KEY,
			action VARCHAR(32) NOT NULL,
			record_id VARCHAR(64),
			operator VARCHAR(128),
			old_data TEXT,
			new_data TEXT,
			changes TEXT,
			ip VARCHAR(64),
			created_at DATETIME
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建数据源表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS datasources (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			type VARCHAR(32) NOT NULL,
			url VARCHAR(512) NOT NULL,
			username VARCHAR(128),
			password VARCHAR(255),
			token TEXT,
			description TEXT,
			status VARCHAR(32) DEFAULT 'active',
			created_at DATETIME,
			created_by VARCHAR(128)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Close 关闭数据库连接
func Close() {
	if DB != nil {
		DB.Close()
	}
}

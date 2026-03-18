package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/go-sql-driver/mysql"
)

var DB *sql.DB

func InitDB() error {
	host := getEnv("MYSQL_HOST", "localhost")
	port := getEnv("MYSQL_PORT", "3306")
	user := os.Getenv("MYSQL_USER")
	password := os.Getenv("MYSQL_PASSWORD")
	dbname := getEnv("MYSQL_DATABASE", "confluence_locker")

	if os.Getenv("DEV_MODE") == "true" {
		if user == "" {
			user = "root"
		}
		if password == "" {
			password = "123456"
			log.Println("警告: MYSQL_PASSWORD 未设置，使用开发默认密码")
		}
	} else {
		if user == "" {
			return fmt.Errorf("MYSQL_USER 环境变量未设置")
		}
		if password == "" {
			return fmt.Errorf("MYSQL_PASSWORD 环境变量未设置")
		}
	}

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

	if _, err = DB.Exec("SET NAMES utf8mb4"); err != nil {
		log.Printf("设置字符集失败: %v", err)
	}

	if err = createTables(); err != nil {
		return fmt.Errorf("创建表失败: %v", err)
	}

	log.Println("数据库连接成功")
	return nil
}

func Close() {
	if DB != nil {
		DB.Close()
	}
}

func createTables() error {
	// 系统设置表
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS system_settings (
			setting_key VARCHAR(128) PRIMARY KEY,
			setting_value TEXT NOT NULL DEFAULT (''),
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 触发规则表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS trigger_rules (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			rule_name VARCHAR(256) NOT NULL DEFAULT '',
			action ENUM('lock','unlock') NOT NULL,
			project_key VARCHAR(64) NOT NULL DEFAULT '',
			transition_name VARCHAR(256) NOT NULL DEFAULT '',
			enabled TINYINT(1) DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_enabled (enabled),
			INDEX idx_action (action)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 权限规则表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS permission_rules (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			rule_type ENUM('user','group') NOT NULL,
			rule_value VARCHAR(256) NOT NULL,
			description VARCHAR(512) DEFAULT '',
			enabled TINYINT(1) DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_type_value (rule_type, rule_value)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// Webhook 事件日志表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS webhook_events (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			jira_issue_key VARCHAR(64) NOT NULL DEFAULT '',
			jira_issue_summary VARCHAR(512) NOT NULL DEFAULT '',
			confluence_page_id VARCHAR(64) NOT NULL DEFAULT '',
			confluence_page_url TEXT,
			webhook_event VARCHAR(128) NOT NULL DEFAULT '',
			transition_name VARCHAR(256) NOT NULL DEFAULT '',
			action VARCHAR(16) NOT NULL DEFAULT '',
			status ENUM('pending','processing','success','failed','skipped') DEFAULT 'pending',
			retry_count INT DEFAULT 0,
			error_message TEXT,
			raw_payload LONGTEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_issue_key (jira_issue_key),
			INDEX idx_status (status),
			INDEX idx_created (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	return err
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

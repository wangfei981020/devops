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
	dbname := getEnv("MYSQL_DATABASE", "confluence_center")

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
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id VARCHAR(64) PRIMARY KEY,
			username VARCHAR(128) NOT NULL UNIQUE,
			password VARCHAR(255) NOT NULL DEFAULT '',
			display_name VARCHAR(128) NOT NULL DEFAULT '',
			email VARCHAR(255) DEFAULT '',
			role VARCHAR(32) DEFAULT 'user',
			status ENUM('active','disabled') DEFAULT 'active',
			oidc_sub VARCHAR(255) DEFAULT '',
			auth_source VARCHAR(32) DEFAULT 'local',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_oidc_sub (oidc_sub)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64) NOT NULL,
			token_hash VARCHAR(128) NOT NULL,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_token_hash (token_hash),
			INDEX idx_expires (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS system_settings (
			setting_key VARCHAR(128) PRIMARY KEY,
			setting_value TEXT NOT NULL DEFAULT (''),
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS service_connections (
			id INT AUTO_INCREMENT PRIMARY KEY,
			type VARCHAR(32) NOT NULL COMMENT 'confluence or jira',
			name VARCHAR(128) NOT NULL DEFAULT '',
			url VARCHAR(512) NOT NULL DEFAULT '',
			username VARCHAR(128) NOT NULL DEFAULT '',
			password VARCHAR(512) NOT NULL DEFAULT '',
			config JSON COMMENT 'extra config: space_key, root_page, fault_issuetype, etc.',
			is_default TINYINT(1) DEFAULT 0,
			status ENUM('active','disabled') DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_type (type)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS grafana_screenshot_tasks (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(128) NOT NULL DEFAULT '',
			grafana_conn_id INT NOT NULL DEFAULT 0,
			dashboards JSON COMMENT '[{"uid":"xxx","title":"xxx","panels":[1,2]}]',
			variables JSON COMMENT '{"project":"xxx"}',
			lark_conn_ids JSON COMMENT '[1,2,3]',
			cron_expr VARCHAR(64) NOT NULL DEFAULT '0 * * * *',
			time_range VARCHAR(32) NOT NULL DEFAULT '1h',
			width INT NOT NULL DEFAULT 1000,
			height INT NOT NULL DEFAULT 500,
			theme VARCHAR(16) NOT NULL DEFAULT 'light',
			sort_order INT NOT NULL DEFAULT 0,
			enabled TINYINT(1) DEFAULT 1,
			last_run_at DATETIME DEFAULT NULL,
			last_status VARCHAR(32) DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS audit_logs (
			id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64) DEFAULT '',
			username VARCHAR(128) DEFAULT '',
			action VARCHAR(64) NOT NULL,
			target_type VARCHAR(32) DEFAULT '',
			target_id VARCHAR(64) DEFAULT '',
			detail TEXT,
			ip VARCHAR(64) DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_user (user_id),
			INDEX idx_created (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS maintenance_windows (
			id INT AUTO_INCREMENT PRIMARY KEY,
			project VARCHAR(128) NOT NULL DEFAULT '' COMMENT '项目名称',
			environment VARCHAR(32) NOT NULL DEFAULT 'PROD' COMMENT '环境: PROD/UAT',
			repeat_mode VARCHAR(16) NOT NULL DEFAULT 'once' COMMENT '重复模式: once/weekly',
			maintenance_type VARCHAR(32) NOT NULL DEFAULT '例行维护' COMMENT '维护类型',
			weekday INT NOT NULL DEFAULT 0 COMMENT '星期几(1=周一..7=周日), 0=不重复',
			time_start VARCHAR(8) NOT NULL DEFAULT '' COMMENT '每周重复开始时间 HH:MM',
			time_end VARCHAR(8) NOT NULL DEFAULT '' COMMENT '每周重复结束时间 HH:MM',
			start_time DATETIME DEFAULT NULL COMMENT '单次维护开始时间',
			end_time DATETIME DEFAULT NULL COMMENT '单次维护结束时间',
			match_rules VARCHAR(512) NOT NULL DEFAULT '' COMMENT '匹配规则名，逗号分隔',
			operations JSON COMMENT '维护操作记录',
			note TEXT COMMENT '备注',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_project (project)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 迁移: 给 grafana_screenshot_tasks 添加 sort_order 字段（兼容已有数据库）
	DB.Exec(`ALTER TABLE grafana_screenshot_tasks ADD COLUMN sort_order INT NOT NULL DEFAULT 0 AFTER theme`)
	// 迁移: 给 maintenance_windows 添加新字段
	DB.Exec(`ALTER TABLE maintenance_windows ADD COLUMN environment VARCHAR(32) NOT NULL DEFAULT 'PROD' AFTER project`)
	DB.Exec(`ALTER TABLE maintenance_windows ADD COLUMN repeat_mode VARCHAR(16) NOT NULL DEFAULT 'once' AFTER environment`)
	DB.Exec(`ALTER TABLE maintenance_windows ADD COLUMN weekday INT NOT NULL DEFAULT 0 AFTER maintenance_type`)
	DB.Exec(`ALTER TABLE maintenance_windows ADD COLUMN time_start VARCHAR(8) NOT NULL DEFAULT '' AFTER weekday`)
	DB.Exec(`ALTER TABLE maintenance_windows ADD COLUMN time_end VARCHAR(8) NOT NULL DEFAULT '' AFTER time_start`)
	DB.Exec(`ALTER TABLE maintenance_windows ADD COLUMN match_rules VARCHAR(512) NOT NULL DEFAULT '' AFTER end_time`)
	DB.Exec(`ALTER TABLE maintenance_windows ADD COLUMN operations JSON AFTER match_rules`)
	DB.Exec(`ALTER TABLE maintenance_windows MODIFY COLUMN start_time DATETIME DEFAULT NULL`)
	DB.Exec(`ALTER TABLE maintenance_windows MODIFY COLUMN end_time DATETIME DEFAULT NULL`)

	// ===== Jira 状态监听 =====
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS jira_notify_rules (
			id              INT AUTO_INCREMENT PRIMARY KEY,
			name            VARCHAR(128) NOT NULL,
			enabled         TINYINT(1) NOT NULL DEFAULT 1,
			jira_conn_id    INT NOT NULL COMMENT 'service_connections.id (type=jira)',
			lark_conn_id    INT NOT NULL COMMENT 'service_connections.id (type=lark)',
			project_keys    JSON NOT NULL COMMENT '["YX","OPS"]',
			issue_types     JSON COMMENT '["升级单","紧急升级单"]，可空',
			extra_jql       TEXT COMMENT '附加 JQL，AND 拼接，可空',
			created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			KEY idx_enabled (enabled)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`); err != nil {
		return fmt.Errorf("创建 jira_notify_rules 失败: %v", err)
	}

	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS jira_notify_rule_targets (
			id                    INT AUTO_INCREMENT PRIMARY KEY,
			rule_id               INT NOT NULL,
			status_name           VARCHAR(128) NOT NULL,
			use_default_template  TINYINT(1) NOT NULL DEFAULT 1,
			custom_template       TEXT,
			startup_lookback_min  INT NOT NULL DEFAULT 0,
			KEY idx_rule (rule_id),
			KEY idx_status (status_name),
			CONSTRAINT fk_target_rule FOREIGN KEY (rule_id) REFERENCES jira_notify_rules(id) ON DELETE CASCADE
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`); err != nil {
		return fmt.Errorf("创建 jira_notify_rule_targets 失败: %v", err)
	}

	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS jira_notify_status_users (
			id           INT AUTO_INCREMENT PRIMARY KEY,
			project_key  VARCHAR(64) NOT NULL,
			status_name  VARCHAR(128) NOT NULL,
			at_users     JSON NOT NULL COMMENT '[{open_id,user_id,mobile,name}]',
			created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uniq_proj_status (project_key, status_name)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`); err != nil {
		return fmt.Errorf("创建 jira_notify_status_users 失败: %v", err)
	}

	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS jira_notify_history (
			id              BIGINT AUTO_INCREMENT PRIMARY KEY,
			rule_id         INT NOT NULL,
			target_id       INT NOT NULL,
			issue_key       VARCHAR(64) NOT NULL,
			issue_summary   VARCHAR(512),
			from_status     VARCHAR(128),
			to_status       VARCHAR(128) NOT NULL,
			transition_at   DATETIME NOT NULL,
			notified_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			status          ENUM('success','failed','pending') NOT NULL,
			retry_count     INT NOT NULL DEFAULT 0,
			last_error      TEXT,
			lark_message_id VARCHAR(128),
			KEY idx_issue (issue_key),
			KEY idx_rule_time (rule_id, notified_at),
			KEY idx_status (status),
			UNIQUE KEY uniq_dedup (rule_id, issue_key, transition_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`); err != nil {
		return fmt.Errorf("创建 jira_notify_history 失败: %v", err)
	}

	return nil
}

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

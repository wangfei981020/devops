package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"opsplatform-alert-backend/config"
)

var DB *sql.DB

func InitMySQL(cfg *config.Config) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		cfg.MySQLUser, cfg.MySQLPassword, cfg.MySQLHost, cfg.MySQLPort, cfg.MySQLDatabase)

	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	DB.SetMaxOpenConns(25)
	DB.SetMaxIdleConns(10)
	DB.SetConnMaxLifetime(5 * time.Minute)

	// Retry connection
	for i := 0; i < 30; i++ {
		if err = DB.Ping(); err == nil {
			break
		}
		log.Printf("Waiting for database... attempt %d/30", i+1)
		time.Sleep(2 * time.Second)
	}
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	log.Println("Database connected successfully")
	return createTables()
}

func createTables() error {
	tables := []string{
		// ES 连接配置
		`CREATE TABLE IF NOT EXISTS es_connections (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(100) NOT NULL COMMENT '连接名称',
			url VARCHAR(500) NOT NULL COMMENT 'ES地址，多个用逗号分隔',
			version VARCHAR(10) NOT NULL DEFAULT '7' COMMENT 'ES版本: 7 或 8',
			username VARCHAR(100) DEFAULT '' COMMENT '用户名',
			password VARCHAR(200) DEFAULT '' COMMENT '密码',
			api_key VARCHAR(500) DEFAULT '' COMMENT 'API Key (ES8)',
			skip_tls_verify TINYINT DEFAULT 0 COMMENT '跳过TLS证书验证 1=是 0=否',
			description VARCHAR(500) DEFAULT '' COMMENT '描述',
			status TINYINT DEFAULT 1 COMMENT '1=启用 0=禁用',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// Loki 连接配置
		`CREATE TABLE IF NOT EXISTS loki_connections (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(100) NOT NULL COMMENT '连接名称',
			url VARCHAR(500) NOT NULL COMMENT 'Loki地址',
			username VARCHAR(100) DEFAULT '' COMMENT '用户名(Basic Auth)',
			password VARCHAR(200) DEFAULT '' COMMENT '密码',
			org_id VARCHAR(100) DEFAULT '' COMMENT 'X-Scope-OrgID(多租户)',
			skip_tls_verify TINYINT DEFAULT 0 COMMENT '跳过TLS证书验证',
			description VARCHAR(500) DEFAULT '' COMMENT '描述',
			status TINYINT DEFAULT 1 COMMENT '1=启用 0=禁用',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// Lark webhook 配置
		`CREATE TABLE IF NOT EXISTS lark_configs (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(100) NOT NULL COMMENT '配置名称',
			webhook_url VARCHAR(500) NOT NULL COMMENT 'Webhook URL',
			secret VARCHAR(200) DEFAULT '' COMMENT '签名密钥',
			lark_type VARCHAR(20) NOT NULL DEFAULT 'feishu' COMMENT 'feishu=国内版 larksuite=国际版',
			description VARCHAR(500) DEFAULT '' COMMENT '描述',
			status TINYINT DEFAULT 1 COMMENT '1=启用 0=禁用',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 告警规则
		`CREATE TABLE IF NOT EXISTS alert_rules (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(200) NOT NULL COMMENT '规则名称',
			es_connection_id INT NOT NULL COMMENT 'ES连接ID',
			lark_config_id INT NOT NULL COMMENT 'Lark配置ID',
			es_index VARCHAR(200) NOT NULL DEFAULT '*' COMMENT 'ES索引',
			schedule VARCHAR(50) NOT NULL DEFAULT '*/5 * * * *' COMMENT 'Cron表达式',
			time_range VARCHAR(20) NOT NULL DEFAULT '5m' COMMENT '搜索时间范围',
			query_dsl TEXT COMMENT '自定义ES查询DSL(JSON)',
			keyword VARCHAR(500) DEFAULT '' COMMENT '搜索关键词',
			filter_fields TEXT COMMENT '过滤字段JSON: [{"field":"namespace","value":"g32-uat"}]',
			extract_fields TEXT COMMENT '提取字段JSON: [{"name":"round","path":"message","pattern":"Round:\\s*(\\S+)"}]',
			message_title VARCHAR(200) DEFAULT '' COMMENT '告警标题',
			message_template TEXT COMMENT '消息模板，支持 {{.field}} 变量',
			at_users TEXT COMMENT '@用户JSON: [{"name":"Bruce","user_id":"ou_xxx"}]',
			at_all TINYINT DEFAULT 0 COMMENT '是否@所有人',
			alert_mode VARCHAR(20) DEFAULT 'found' COMMENT '告警模式: found=搜到告警 not_found=搜不到告警',
			recovery_enabled TINYINT DEFAULT 0 COMMENT '是否启用恢复通知',
			recovery_title VARCHAR(200) DEFAULT '' COMMENT '恢复通知标题',
			recovery_template TEXT COMMENT '恢复通知模板',
			severity VARCHAR(20) DEFAULT 'warning' COMMENT '告警级别: info/warning/critical',
			dedup_field VARCHAR(200) DEFAULT '' COMMENT '去重字段，多个用逗号分隔',
			group_by VARCHAR(200) DEFAULT '' COMMENT '分组字段,如kubernetes.container_name,多个用逗号分隔',
			dedup_ttl INT DEFAULT 3600 COMMENT '去重TTL(秒)',
			max_alerts INT DEFAULT 10 COMMENT '单次最大告警条数',
			prometheus_config TEXT COMMENT 'Prometheus指标配置JSON',
			status TINYINT DEFAULT 1 COMMENT '1=启用 0=禁用',
			last_run_at TIMESTAMP NULL COMMENT '上次执行时间',
			last_error TEXT COMMENT '上次错误信息',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_status (status),
			INDEX idx_es_connection (es_connection_id),
			INDEX idx_lark_config (lark_config_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 告警日志
		`CREATE TABLE IF NOT EXISTS alert_logs (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			rule_id INT NOT NULL COMMENT '规则ID',
			rule_name VARCHAR(200) DEFAULT '' COMMENT '规则名称',
			severity VARCHAR(20) DEFAULT 'warning' COMMENT '告警级别',
			message TEXT COMMENT '告警消息内容',
			es_raw TEXT COMMENT 'ES原始数据',
			lark_response TEXT COMMENT 'Lark发送响应',
			status VARCHAR(20) DEFAULT 'success' COMMENT 'success/failed',
			error_msg TEXT COMMENT '错误信息',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_rule_id (rule_id),
			INDEX idx_created_at (created_at),
			INDEX idx_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 用户表 (与运维平台联动)
		`CREATE TABLE IF NOT EXISTS users (
			id INT AUTO_INCREMENT PRIMARY KEY,
			username VARCHAR(50) NOT NULL UNIQUE,
			password_hash VARCHAR(200) DEFAULT '',
			display_name VARCHAR(100) DEFAULT '',
			role VARCHAR(20) DEFAULT 'user' COMMENT 'admin/user',
			oidc_subject VARCHAR(200) DEFAULT '',
			status TINYINT DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 会话表
		`CREATE TABLE IF NOT EXISTS sessions (
			id INT AUTO_INCREMENT PRIMARY KEY,
			user_id INT NOT NULL,
			token_hash VARCHAR(64) NOT NULL UNIQUE,
			expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_token (token_hash),
			INDEX idx_expires (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}

	for _, t := range tables {
		if _, err := DB.Exec(t); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Auto-migrate: add columns if missing
	autoMigrate()

	// Ensure default admin exists
	ensureDefaultAdmin()

	log.Println("Database tables initialized")
	return nil
}

func autoMigrate() {
	// Each entry: table, column, column definition
	migrations := []struct {
		table  string
		column string
		ddl    string
	}{
		{"es_connections", "skip_tls_verify", "TINYINT DEFAULT 0 COMMENT '跳过TLS证书验证'"},
		{"alert_rules", "alert_mode", "VARCHAR(20) DEFAULT 'found' COMMENT '告警模式: found/not_found'"},
		{"alert_rules", "recovery_enabled", "TINYINT DEFAULT 0 COMMENT '启用恢复通知'"},
		{"alert_rules", "recovery_title", "VARCHAR(200) DEFAULT '' COMMENT '恢复通知标题'"},
		{"alert_rules", "recovery_template", "TEXT COMMENT '恢复通知模板'"},
		{"alert_rules", "prometheus_config", "TEXT COMMENT 'Prometheus指标配置JSON'"},
		{"alert_rules", "group_by", "VARCHAR(200) DEFAULT '' COMMENT '分组字段'"},
		{"alert_rules", "expected_groups", "TEXT COMMENT '期望的分组列表JSON'"},
		{"alert_rules", "query_concurrency", "INT DEFAULT 5 COMMENT '单规则查询并发数'"},
		{"alert_rules", "data_source_type", "VARCHAR(20) DEFAULT 'es' COMMENT '数据源类型: es/loki'"},
		{"alert_rules", "loki_connection_id", "INT DEFAULT 0 COMMENT 'Loki连接ID'"},
		{"alert_rules", "logql", "TEXT COMMENT 'LogQL查询语句(Loki)'"},
	}

	for _, m := range migrations {
		var count int
		DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`,
			m.table, m.column).Scan(&count)
		if count == 0 {
			sql := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", m.table, m.column, m.ddl)
			if _, err := DB.Exec(sql); err != nil {
				log.Printf("[Migration] Failed to add %s.%s: %v", m.table, m.column, err)
			} else {
				log.Printf("[Migration] Added column %s.%s", m.table, m.column)
			}
		}
	}
}

func ensureDefaultAdmin() {
	var count int
	DB.QueryRow("SELECT COUNT(*) FROM users WHERE username = 'admin'").Scan(&count)
	if count == 0 {
		// Default password: admin123 (bcrypt hash)
		DB.Exec(`INSERT INTO users (username, password_hash, display_name, role)
			VALUES ('admin', '$2a$10$vXhq5Vju4qCuhXhbGNjvyOqrEkXxTkkzyOokD0jKV5d8bjMOpgNQ6', 'Admin', 'admin')`)
		log.Println("Default admin user created (admin/admin123)")
	}
}

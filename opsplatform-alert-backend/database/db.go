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
	// Read and write instants in UTC, end to end. TIMESTAMP columns already
	// keep UTC internally and convert on the way out using the session zone, so
	// pinning the session to +00:00 and telling the driver to label rows UTC
	// makes the value Go sees the actual instant. Leaving these to default
	// meant the session followed the server's zone while the driver labelled
	// rows with the process's — two clocks that only agree by accident, and a
	// timestamp read back shifted by the difference. The zone a person sees is
	// a rendering choice, applied later, by package timezone.
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=UTC&time_zone=%%27%%2B00%%3A00%%27",
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

		// 统一通知渠道表（ID 空间沿用 lark_configs，勿改 AUTO_INCREMENT 起点逻辑）
		`CREATE TABLE IF NOT EXISTS notify_channels (
			id INT AUTO_INCREMENT PRIMARY KEY,
			channel_type VARCHAR(20) NOT NULL DEFAULT 'lark' COMMENT 'lark / telegram',
			name VARCHAR(100) NOT NULL COMMENT '渠道名称',
			webhook_url VARCHAR(500) DEFAULT '' COMMENT 'Lark Webhook URL',
			secret VARCHAR(200) DEFAULT '' COMMENT 'Lark 签名密钥',
			lark_type VARCHAR(20) DEFAULT '' COMMENT 'feishu=国内版 larksuite=国际版',
			bot_token VARCHAR(200) DEFAULT '' COMMENT 'Telegram Bot Token',
			chat_id VARCHAR(64) DEFAULT '' COMMENT 'Telegram chat_id，群组为负数',
			thread_id INT DEFAULT 0 COMMENT 'Telegram 论坛话题 message_thread_id，0=不指定',
			proxy_url VARCHAR(200) DEFAULT '' COMMENT '出网代理，留空直连',
			description VARCHAR(500) DEFAULT '' COMMENT '描述',
			status TINYINT DEFAULT 1 COMMENT '1=启用 0=禁用',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_channel_type (channel_type)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 规则 ↔ 渠道 多对多
		`CREATE TABLE IF NOT EXISTS alert_rule_channels (
			rule_id INT NOT NULL COMMENT '告警规则ID',
			channel_id INT NOT NULL COMMENT '通知渠道ID',
			PRIMARY KEY (rule_id, channel_id),
			INDEX idx_channel (channel_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 一次性迁移标记，防止 autoMigrate 每次启动重复搬数据
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			name VARCHAR(190) PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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

		// 通知人管理
		`CREATE TABLE IF NOT EXISTS alert_contacts (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(100) NOT NULL UNIQUE COMMENT '姓名',
			lark_id VARCHAR(200) NOT NULL COMMENT 'Lark open_id/user_id',
			phone VARCHAR(50) DEFAULT '' COMMENT '手机号',
			email VARCHAR(200) DEFAULT '' COMMENT '邮箱',
			description VARCHAR(500) DEFAULT '' COMMENT '备注',
			status TINYINT DEFAULT 1 COMMENT '1=启用 0=禁用',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 告警屏蔽
		`CREATE TABLE IF NOT EXISTS alert_mutes (
			id INT AUTO_INCREMENT PRIMARY KEY,
			rule_id INT NOT NULL COMMENT '规则ID',
			group_key VARCHAR(200) NOT NULL COMMENT '屏蔽的容器/分组名',
			mute_until TIMESTAMP NOT NULL COMMENT '屏蔽截止时间',
			reason VARCHAR(500) DEFAULT '' COMMENT '屏蔽原因',
			created_by VARCHAR(100) DEFAULT '' COMMENT '操作人',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_rule_group (rule_id, group_key),
			INDEX idx_mute_until (mute_until)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 告警日志
		`CREATE TABLE IF NOT EXISTS alert_logs (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			rule_id INT NOT NULL COMMENT '规则ID',
			rule_name VARCHAR(200) DEFAULT '' COMMENT '规则名称',
			severity VARCHAR(20) DEFAULT 'warning' COMMENT '告警级别',
			message TEXT COMMENT '告警消息内容',
			es_raw TEXT COMMENT 'ES原始数据',
			lark_response TEXT COMMENT '各渠道发送结果JSON数组（列名保留兼容历史数据）',
			status VARCHAR(20) DEFAULT 'success' COMMENT 'success/partial/failed',
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

		// ES 项目分类（项目+环境作为整体单元）
		`CREATE TABLE IF NOT EXISTS es_projects (
			id INT AUTO_INCREMENT PRIMARY KEY,
			code VARCHAR(64) NOT NULL UNIQUE COMMENT '唯一标识: g32-prod',
			display_name VARCHAR(128) NOT NULL COMMENT '显示名',
			match_keywords VARCHAR(255) NOT NULL COMMENT '匹配关键词,逗号分隔,AND语义',
			enabled TINYINT NOT NULL DEFAULT 1,
			sort_order INT NOT NULL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_enabled_sort (enabled, sort_order)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}

	for _, t := range tables {
		if _, err := DB.Exec(t); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Auto-migrate: add columns if missing
	if err := autoMigrate(); err != nil {
		return err
	}

	// Ensure default admin exists
	ensureDefaultAdmin()

	log.Println("Database tables initialized")
	return nil
}

func autoMigrate() error {
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
		{"alert_rules", "alert_interval", "VARCHAR(20) DEFAULT '' COMMENT '告警间隔: once=只告警一次(恢复后再通知)/5m/10m/30m/1h，空则每次都发'"},
		{"alert_rules", "data_source_type", "VARCHAR(20) DEFAULT 'es' COMMENT '数据源类型: es/loki'"},
		{"alert_rules", "loki_connection_id", "INT DEFAULT 0 COMMENT 'Loki连接ID'"},
		{"alert_rules", "logql", "TEXT COMMENT 'LogQL查询语句(Loki)'"},
		{"alert_rules", "route_config", "TEXT COMMENT '字段值路由配置JSON'"},
		{"alert_rules", "namespaces", "TEXT COMMENT '命名空间列表JSON: [\"ns1\",\"ns2\"]'"},
		{"alert_rules", "namespace_concurrency", "INT DEFAULT 3 COMMENT '命名空间并发查询数'"},
		{"alert_rules", "label_filters", "TEXT COMMENT 'Loki selector 额外标签过滤: container!~\"c1|c2\", app=\"api\"（仅 found 模式）'"},
		{"alert_rules", "project_id", "INT DEFAULT 0 COMMENT '所属项目ID'"},
		{"alert_rules", "realtime_enabled", "TINYINT DEFAULT 0 COMMENT '性能告警:实时告警开关'"},
		{"alert_rules", "threshold_ms", "INT DEFAULT 0 COMMENT '性能告警:实时阈值(毫秒)'"},
		{"alert_rules", "report_enabled", "TINYINT DEFAULT 0 COMMENT '性能告警:日报开关'"},
		{"alert_rules", "report_schedule", "VARCHAR(64) DEFAULT '0 1 0 * * *' COMMENT '性能告警:日报cron(6段)'"},
		{"alert_rules", "report_mode", "VARCHAR(16) DEFAULT 'separate' COMMENT '性能告警:日报模式 separate/merged'"},
		{"alert_rules", "report_title", "VARCHAR(255) DEFAULT '' COMMENT '性能告警:日报标题'"},
		{"alert_rules", "report_template", "TEXT COMMENT '性能告警:日报模板'"},
		{"alert_rules", "stack_context_enabled", "TINYINT DEFAULT 0 COMMENT '错误栈上下文: 总开关'"},
		{"alert_rules", "stack_max_lines", "INT DEFAULT 200 COMMENT '错误栈上下文: 采集阶段的安全阈值(防止边界正则不匹配拖入整段日志), 不是展示裁剪'"},
		{"alert_rules", "stack_head_lines", "INT DEFAULT 12 COMMENT '错误栈上下文: 超长时保留的头部行数'"},
		{"alert_rules", "stack_tail_lines", "INT DEFAULT 8 COMMENT '错误栈上下文: 超长时保留的尾部行数'"},
		{"alert_rules", "stack_boundary_pattern", "VARCHAR(500) DEFAULT '' COMMENT '错误栈上下文: 新日志行正则, 空则用内置默认'"},
		{"alert_rules", "stack_window_sec", "INT DEFAULT 5 COMMENT '错误栈上下文: 向后取多少秒'"},
		{"alert_rules", "log_context_enabled", "TINYINT DEFAULT 0 COMMENT '日志上下文: 总开关(独立于错误栈上下文)'"},
		{"alert_rules", "log_context_before", "INT DEFAULT 25 COMMENT '日志上下文: 命中行向前取多少行'"},
		{"alert_rules", "log_context_after", "INT DEFAULT 50 COMMENT '日志上下文: 命中行向后取多少行'"},
		{"alert_rules", "log_context_max_window_sec", "INT DEFAULT 1800 COMMENT '日志上下文: 阶梯时间窗上限(秒), 阶梯 30s->2m->10m->此上限, 取够即停'"},
		{"alert_rules", "log_context_display_lines", "INT DEFAULT 20 COMMENT '日志上下文: 消息里最多展示多少行(超出保留命中行附近, 并注明省略数)'"},
		{"users", "auth_source", "VARCHAR(20) DEFAULT 'local' COMMENT '认证来源: local/portal'"},
		{"users", "portal_token", "TEXT COMMENT '运维平台Portal Token(用于刷新权限)'"},
		{"alert_contacts", "telegram_id", "VARCHAR(64) DEFAULT '' COMMENT 'Telegram user id（数字），用于 @提醒'"},
		// Recorded in the row rather than in Redis: a reminder that fires twice
		// because a cache was cleared is worse than one that arrives a little
		// late, and this has to survive a restart.
		{"alert_mutes", "reminded_at", "TIMESTAMP NULL COMMENT '到期提醒发送时间，NULL 表示未提醒'"},
		// The day a rule's report was last sent for, as a YYYYMMDD key. Claimed
		// with a conditional update so a Redis outage cannot turn one report
		// into one per replica.
		{"alert_rules", "last_report_day", "VARCHAR(8) DEFAULT '' COMMENT '最近已发送日报的日期(YYYYMMDD)，用于跨副本去重'"},
	}

	// Ensure alert_projects table exists
	DB.Exec(`CREATE TABLE IF NOT EXISTS alert_projects (
		id INT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(100) NOT NULL COMMENT '项目名称',
		parent_id INT DEFAULT 0 COMMENT '父项目ID, 0=顶级',
		sort_order INT DEFAULT 0 COMMENT '排序',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		INDEX idx_parent (parent_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)

	// Platform settings, keyed by name. Small and read rarely — the values are
	// cached in process and only re-read when an operator changes one.
	DB.Exec(`CREATE TABLE IF NOT EXISTS system_settings (
		setting_key VARCHAR(64) PRIMARY KEY COMMENT '设置项键名',
		setting_value VARCHAR(200) NOT NULL DEFAULT '' COMMENT '设置值',
		updated_by VARCHAR(100) DEFAULT '' COMMENT '最后修改人',
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)

	// Ensure audit_logs table exists
	DB.Exec(`CREATE TABLE IF NOT EXISTS audit_logs (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		username VARCHAR(100) NOT NULL COMMENT '操作人',
		auth_source VARCHAR(20) DEFAULT 'local' COMMENT '来源: local/portal',
		action VARCHAR(50) NOT NULL COMMENT '操作类型',
		target_type VARCHAR(50) DEFAULT '' COMMENT '目标类型: rule/connection/user/contact/mute',
		target_name VARCHAR(200) DEFAULT '' COMMENT '目标名称',
		detail TEXT COMMENT '操作详情',
		ip VARCHAR(50) DEFAULT '' COMMENT '客户端IP',
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		INDEX idx_username (username),
		INDEX idx_action (action),
		INDEX idx_created_at (created_at)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`)

	// Drop unique constraint on username if exists (allow same username for local and portal)
	DB.Exec("ALTER TABLE users DROP INDEX username")
	DB.Exec("CREATE UNIQUE INDEX uk_username_source ON users(username, auth_source)")

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

	if err := migrateLarkConfigsToChannels(); err != nil {
		return err
	}
	return migrateStackMaxLinesTo200()
}

// migrateLarkConfigsToChannels copies lark_configs into notify_channels keeping
// the original IDs, then backfills alert_rule_channels from alert_rules.lark_config_id.
// Preserving IDs is what keeps existing route_config JSON (which stores lark_id)
// pointing at the right channel.
// All operations are transactional: the marker is recorded only after both data
// migrations succeed, so a failed migration leaves no marker behind and will retry
// on the next boot.
// Every failure path returns an error, and the caller aborts startup on it. That
// is deliberate: booting with an empty notify_channels table looks healthy but
// silently delivers nothing, and an operator rebuilding the channels by hand
// takes ids 1, 2… which collide with the retry on the next boot and misalign the
// id space that route_config's lark_id depends on. A crashloop is visible.
func migrateLarkConfigsToChannels() error {
	migrationName := "2026_09_07_lark_configs_to_notify_channels"

	// Check if migration already ran
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE name = ?", migrationName).Scan(&count); err != nil {
		return fmt.Errorf("[Migration] cannot read schema_migrations: %w", err)
	}
	if count > 0 {
		return nil
	}

	// Start transaction
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("[Migration] cannot start transaction: %w", err)
	}
	defer tx.Rollback()

	// Copy lark_configs to notify_channels. INSERT IGNORE keeps the copy
	// idempotent: a channel row that already carries one of these ids (a retry
	// after a partial run, or a hand-rebuilt table) must not abort the boot.
	res, err := tx.Exec(`INSERT IGNORE INTO notify_channels
		(id, channel_type, name, webhook_url, secret, lark_type, description, status, created_at)
		SELECT id, 'lark', name, webhook_url, secret, lark_type, description, status, created_at
		FROM lark_configs`)
	if err != nil {
		return fmt.Errorf("[Migration] copy lark_configs -> notify_channels failed: %w", err)
	}
	n, _ := res.RowsAffected()
	log.Printf("[Migration] Copied %d lark_configs into notify_channels", n)

	// Backfill alert_rule_channels (JOIN to lark_configs to avoid orphaned rows)
	res, err = tx.Exec(`INSERT IGNORE INTO alert_rule_channels (rule_id, channel_id)
		SELECT ar.id, ar.lark_config_id FROM alert_rules ar
		JOIN lark_configs lc ON lc.id = ar.lark_config_id`)
	if err != nil {
		return fmt.Errorf("[Migration] backfill alert_rule_channels failed: %w", err)
	}
	n, _ = res.RowsAffected()
	log.Printf("[Migration] Backfilled %d alert_rule_channels rows", n)

	// Record the migration as applied (only after data migrations succeed)
	if _, err := tx.Exec("INSERT INTO schema_migrations (name) VALUES (?)", migrationName); err != nil {
		return fmt.Errorf("[Migration] cannot record migration: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("[Migration] cannot commit migration transaction: %w", err)
	}

	log.Printf("[Migration] Migration %s completed successfully", migrationName)
	return nil
}

// migrateStackMaxLinesTo200 raises stack_max_lines from the old default of 30
// to the new default of 200 on rows that still sit at exactly 30.
//
// stack_max_lines is a collection-time safety valve (stop an unmatched boundary
// regex from dragging an entire log stream into one alert), not a presentation
// setting. At 30 it was tight enough to front-truncate a realistic deep Java
// stack before ElideMiddle ever got to keep the tail — throwing away the root
// cause the feature exists to preserve. See alert/stackcontext.go. Changing the
// column's DEFAULT does not touch existing rows, so this backfills them once.
// Only rows still at exactly 30 are touched; an operator who deliberately chose
// a different value keeps it.
func migrateStackMaxLinesTo200() error {
	migrationName := "2026_09_07_stack_max_lines_to_200"

	// Check if migration already ran
	var count int
	if err := DB.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE name = ?", migrationName).Scan(&count); err != nil {
		return fmt.Errorf("[Migration] cannot read schema_migrations: %w", err)
	}
	if count > 0 {
		return nil
	}

	// Start transaction
	tx, err := DB.Begin()
	if err != nil {
		return fmt.Errorf("[Migration] cannot start transaction: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec("UPDATE alert_rules SET stack_max_lines = 200 WHERE stack_max_lines = 30")
	if err != nil {
		return fmt.Errorf("[Migration] raise stack_max_lines default failed: %w", err)
	}
	n, _ := res.RowsAffected()
	log.Printf("[Migration] Raised stack_max_lines 30 -> 200 on %d alert_rules rows", n)

	// Record the migration as applied (only after the data migration succeeds)
	if _, err := tx.Exec("INSERT INTO schema_migrations (name) VALUES (?)", migrationName); err != nil {
		return fmt.Errorf("[Migration] cannot record migration: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("[Migration] cannot commit migration transaction: %w", err)
	}

	log.Printf("[Migration] Migration %s completed successfully", migrationName)
	return nil
}

func ensureDefaultAdmin() {
	var count int
	DB.QueryRow("SELECT COUNT(*) FROM users WHERE username = 'admin' AND auth_source = 'local'").Scan(&count)
	if count == 0 {
		// Default password: admin123 (bcrypt hash)
		DB.Exec(`INSERT INTO users (username, password_hash, display_name, role, auth_source)
			VALUES ('admin', '$2a$10$vXhq5Vju4qCuhXhbGNjvyOqrEkXxTkkzyOokD0jKV5d8bjMOpgNQ6', 'Admin', 'admin', 'local')`)
		log.Println("Default admin user created (admin/admin123)")
	}

	// Fix any local admin that was wrongly set to portal
	DB.Exec("UPDATE users SET auth_source = 'local' WHERE username = 'admin' AND password_hash != '' AND auth_source = 'portal'")
}

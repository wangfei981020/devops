package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"
	"opsplatform-probe-backend/config"
)

// Cfg is kept so other init funcs can access config flags (e.g. ensureDefaultAdmin)
var Cfg *config.Config

var DB *sql.DB

func InitMySQL(cfg *config.Config) error {
	Cfg = cfg
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
		// 用户表 (与运维平台联动)
		`CREATE TABLE IF NOT EXISTS users (
			id INT AUTO_INCREMENT PRIMARY KEY,
			username VARCHAR(50) NOT NULL,
			password_hash VARCHAR(200) DEFAULT '',
			display_name VARCHAR(100) DEFAULT '',
			role VARCHAR(20) DEFAULT 'user' COMMENT 'admin/user',
			auth_source VARCHAR(20) DEFAULT 'local' COMMENT 'local/portal',
			portal_token TEXT COMMENT '运维平台Portal Token',
			status TINYINT DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_username_source (username, auth_source)
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

		// Agent 分组
		`CREATE TABLE IF NOT EXISTS agent_groups (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(100) NOT NULL UNIQUE COMMENT '分组名',
			description VARCHAR(500) DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// Agent 表
		`CREATE TABLE IF NOT EXISTS agents (
			id INT AUTO_INCREMENT PRIMARY KEY,
			agent_id VARCHAR(128) NOT NULL UNIQUE COMMENT '由 Agent 自报，全局唯一',
			hostname VARCHAR(255) DEFAULT '' COMMENT '主机名',
			ip VARCHAR(64) DEFAULT '' COMMENT '上报的 IP',
			version VARCHAR(64) DEFAULT '' COMMENT '当前运行版本',
			os VARCHAR(64) DEFAULT '',
			arch VARCHAR(32) DEFAULT '',
			tags VARCHAR(500) DEFAULT '' COMMENT '逗号分隔标签',
			group_id INT DEFAULT 0 COMMENT '分组ID, 0=未分组',
			token_hash VARCHAR(64) DEFAULT '' COMMENT 'Agent Token的SHA256',
			status VARCHAR(20) DEFAULT 'pending' COMMENT 'pending/online/offline/disabled',
			approved TINYINT DEFAULT 0 COMMENT '是否已审批',
			approved_by VARCHAR(100) DEFAULT '',
			approved_at TIMESTAMP NULL,
			last_heartbeat_at TIMESTAMP NULL,
			last_seen_ip VARCHAR(64) DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_status (status),
			INDEX idx_group (group_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 探测目标
		`CREATE TABLE IF NOT EXISTS probe_targets (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(200) NOT NULL COMMENT '目标名称',
			type VARCHAR(20) NOT NULL COMMENT 'http/tcp/dns',
			target VARCHAR(500) NOT NULL COMMENT 'URL/host:port/host',
			port INT DEFAULT 0 COMMENT 'TCP端口',
			method VARCHAR(10) DEFAULT 'GET' COMMENT 'HTTP方法',
			expected_status INT DEFAULT 0 COMMENT '期望HTTP状态码, 0=2xx即可',
			timeout_sec INT DEFAULT 5 COMMENT '超时秒数',
			group_name VARCHAR(100) DEFAULT '' COMMENT '分组',
			description VARCHAR(500) DEFAULT '',
			agent_scope VARCHAR(20) DEFAULT 'all' COMMENT 'all/group/specific',
			scope_group_id INT DEFAULT 0 COMMENT 'agent_scope=group 时使用',
			enabled TINYINT DEFAULT 1,
			created_by VARCHAR(100) DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_type (type),
			INDEX idx_group (group_name),
			INDEX idx_scope (agent_scope, scope_group_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 目标-Agent 多对多绑定 (scope=specific 时)
		`CREATE TABLE IF NOT EXISTS probe_target_agents (
			target_id INT NOT NULL,
			agent_id VARCHAR(128) NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (target_id, agent_id),
			INDEX idx_target (target_id),
			INDEX idx_agent (agent_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 探测任务批次
		`CREATE TABLE IF NOT EXISTS probe_batches (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			batch_id VARCHAR(64) NOT NULL UNIQUE COMMENT '批次UUID',
			created_by VARCHAR(100) DEFAULT '',
			source VARCHAR(20) DEFAULT 'manual' COMMENT 'manual/scheduled',
			total_tasks INT DEFAULT 0,
			done_tasks INT DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_batch (batch_id),
			INDEX idx_created (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 探测任务 (Agent 拉取的工作单元)
		`CREATE TABLE IF NOT EXISTS probe_tasks (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			batch_id VARCHAR(64) NOT NULL,
			agent_id VARCHAR(128) NOT NULL,
			target_id INT NOT NULL,
			target_type VARCHAR(20) NOT NULL,
			target_addr VARCHAR(500) NOT NULL COMMENT '快照',
			target_port INT DEFAULT 0,
			method VARCHAR(10) DEFAULT '',
			timeout_sec INT DEFAULT 5,
			status VARCHAR(20) DEFAULT 'pending' COMMENT 'pending/running/done/expired',
			fetched_at TIMESTAMP NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_agent_status (agent_id, status),
			INDEX idx_batch (batch_id),
			INDEX idx_created (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 探测结果
		`CREATE TABLE IF NOT EXISTS probe_results (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			task_id BIGINT NOT NULL,
			batch_id VARCHAR(64) NOT NULL,
			agent_id VARCHAR(128) NOT NULL,
			target_id INT NOT NULL,
			target_name VARCHAR(200) DEFAULT '',
			target_type VARCHAR(20) NOT NULL,
			target_addr VARCHAR(500) NOT NULL,
			target_port INT DEFAULT 0,
			success TINYINT NOT NULL,
			latency_ms INT DEFAULT 0,
			status_code INT DEFAULT 0,
			resolved_ip VARCHAR(128) DEFAULT '',
			error VARCHAR(1000) DEFAULT '',
			probed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_batch (batch_id),
			INDEX idx_target (target_id),
			INDEX idx_agent (agent_id),
			INDEX idx_probed (probed_at),
			INDEX idx_success (success)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// Agent 版本元数据
		`CREATE TABLE IF NOT EXISTS agent_versions (
			id INT AUTO_INCREMENT PRIMARY KEY,
			version VARCHAR(64) NOT NULL UNIQUE,
			minio_key VARCHAR(500) NOT NULL,
			sha256 VARCHAR(64) NOT NULL,
			size_bytes BIGINT NOT NULL,
			os VARCHAR(20) DEFAULT 'linux',
			arch VARCHAR(20) DEFAULT 'amd64',
			source VARCHAR(20) DEFAULT 'upload' COMMENT 'upload/image',
			source_image VARCHAR(500) DEFAULT '',
			changelog TEXT,
			uploaded_by VARCHAR(100) DEFAULT '',
			uploaded_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_uploaded (uploaded_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 升级任务
		`CREATE TABLE IF NOT EXISTS agent_upgrade_tasks (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			agent_id VARCHAR(128) NOT NULL,
			from_version VARCHAR(64) DEFAULT '',
			to_version VARCHAR(64) NOT NULL,
			status VARCHAR(20) DEFAULT 'pending' COMMENT 'pending/downloading/upgrading/success/failed/rollback',
			error VARCHAR(1000) DEFAULT '',
			created_by VARCHAR(100) DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			fetched_at TIMESTAMP NULL,
			finished_at TIMESTAMP NULL,
			INDEX idx_agent_status (agent_id, status),
			INDEX idx_created (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 审计日志
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			username VARCHAR(100) NOT NULL,
			auth_source VARCHAR(20) DEFAULT 'local',
			action VARCHAR(50) NOT NULL,
			target_type VARCHAR(50) DEFAULT '',
			target_name VARCHAR(200) DEFAULT '',
			detail TEXT,
			ip VARCHAR(64) DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_username (username),
			INDEX idx_action (action),
			INDEX idx_created (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}

	for _, t := range tables {
		if _, err := DB.Exec(t); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	autoMigrate()
	ensureDefaultAdmin()
	log.Println("Database tables initialized")
	return nil
}

// autoMigrate adds new columns to existing tables without dropping data.
// 规则: 只加不删。要加新字段时在 migrations 清单末尾追加一行即可。
// 检查 information_schema 存在性 → 不存在则 ALTER TABLE ADD COLUMN，幂等。
func autoMigrate() {
	migrations := []struct {
		table  string
		column string
		ddl    string
	}{
		// Agent token 过期管理 (security fix #2)
		{"agents", "token_issued_at", "TIMESTAMP NULL COMMENT 'Agent Token 签发时间'"},
		{"agents", "token_expires_at", "TIMESTAMP NULL COMMENT 'Agent Token 过期时间, NULL=永不'"},

		// 审计日志 hash 链 (security fix #10)
		{"audit_logs", "prev_hash", "VARCHAR(64) NOT NULL DEFAULT '' COMMENT '上一条 row_hash'"},
		{"audit_logs", "row_hash", "VARCHAR(64) NOT NULL DEFAULT '' COMMENT '本行完整性 hash'"},

		// Agent 二进制代码签名 (security fix #A)
		{"agent_versions", "signature", "TEXT COMMENT 'ed25519 签名 (base64), 用于 Agent 验签'"},
	}

	for _, m := range migrations {
		var count int
		DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?`,
			m.table, m.column).Scan(&count)
		if count == 0 {
			sql := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", m.table, m.column, m.ddl)
			if _, err := DB.Exec(sql); err != nil {
				log.Printf("[Migration] Failed %s.%s: %v", m.table, m.column, err)
			} else {
				log.Printf("[Migration] Added column %s.%s", m.table, m.column)
			}
		}
	}
}

func ensureDefaultAdmin() {
	if Cfg != nil && Cfg.DisableDefaultAdmin {
		log.Println("[Admin] DISABLE_DEFAULT_ADMIN=true, 跳过默认管理员创建")
		return
	}
	var count int
	DB.QueryRow("SELECT COUNT(*) FROM users WHERE username = 'admin' AND auth_source = 'local'").Scan(&count)
	if count > 0 {
		return
	}
	password := "admin123"
	if Cfg != nil && Cfg.DefaultAdminPassword != "" {
		password = Cfg.DefaultAdminPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		log.Printf("[Admin] bcrypt failed: %v", err)
		return
	}
	_, err = DB.Exec(`INSERT INTO users (username, password_hash, display_name, role, auth_source)
		VALUES ('admin', ?, 'Admin', 'admin', 'local')`, string(hash))
	if err != nil {
		log.Printf("[Admin] create default admin failed: %v", err)
		return
	}
	if Cfg != nil && Cfg.DefaultAdminPassword != "" {
		log.Println("[Admin] 默认管理员已创建 (admin / 使用 DEFAULT_ADMIN_PASSWORD)")
	} else {
		log.Println("[Admin] 默认管理员已创建 (admin / admin123) — 强烈建议登录后立即修改密码")
	}
}

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
	// 从环境变量获取配置（强制设置，无默认值）
	host := getEnv("MYSQL_HOST", "localhost")
	port := getEnv("MYSQL_PORT", "3306")
	user := os.Getenv("MYSQL_USER")
	password := os.Getenv("MYSQL_PASSWORD")
	dbname := getEnv("MYSQL_DATABASE", "opsplatform")

	// 开发模式允许使用默认值
	if os.Getenv("DEV_MODE") == "true" {
		if user == "" {
			user = "root"
		}
		if password == "" {
			password = "123456"
			log.Println("⚠️  警告: MYSQL_PASSWORD 未设置，使用开发默认密码（仅限开发环境）")
		}
	} else {
		// 生产模式强制检查
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
			connection_id VARCHAR(128) NOT NULL DEFAULT '',
			project VARCHAR(255) NOT NULL,
			env VARCHAR(32) NOT NULL,
			module VARCHAR(255) DEFAULT '',
			vid VARCHAR(255) NOT NULL,
			src_ip VARCHAR(64) NOT NULL,
			src_port VARCHAR(32) NOT NULL DEFAULT '',
			dest_ip VARCHAR(64) NOT NULL,
			dest_port VARCHAR(32) NOT NULL DEFAULT '',
			status VARCHAR(32) DEFAULT 'active',
			operator VARCHAR(128),
			created_at DATETIME,
			updated_at DATETIME,
			created_by VARCHAR(128),
			updated_by VARCHAR(128),
			INDEX idx_connection_id (connection_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 检查并添加 connection_id 列（兼容旧数据库）
	DB.Exec(`ALTER TABLE records ADD COLUMN connection_id VARCHAR(128) NOT NULL DEFAULT '' AFTER id`)
	// 删除旧的 connection_id 唯一约束（如果存在）
	DB.Exec(`ALTER TABLE records DROP INDEX uk_connection_id`)
	// 添加 connection_id 索引（非唯一）
	DB.Exec(`CREATE INDEX idx_connection_id ON records(connection_id)`)
	// 添加 src_port 列（兼容旧数据库）
	DB.Exec(`ALTER TABLE records ADD COLUMN src_port VARCHAR(32) NOT NULL DEFAULT '' AFTER src_ip`)
	// 重命名 port 为 dest_port（兼容旧数据库）
	DB.Exec(`ALTER TABLE records CHANGE COLUMN port dest_port VARCHAR(32) NOT NULL DEFAULT ''`)
	// 删除旧的唯一约束（如果存在）
	DB.Exec(`ALTER TABLE records DROP INDEX uk_network_tuple`)

	// 检查并添加 module 列（兼容旧数据库）
	DB.Exec(`ALTER TABLE records ADD COLUMN module VARCHAR(255) DEFAULT '' AFTER env`)

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
			mfa_enabled TINYINT(1) DEFAULT 0,
			mfa_secret VARCHAR(64),
			created_at DATETIME
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 兼容旧数据库：添加 updated_at 字段
	DB.Exec(`ALTER TABLE users ADD COLUMN updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP`)
	// 兼容旧数据库：添加 MFA 字段
	DB.Exec(`ALTER TABLE users ADD COLUMN mfa_enabled TINYINT(1) DEFAULT 0`)
	DB.Exec(`ALTER TABLE users ADD COLUMN mfa_secret VARCHAR(64)`)
	// 兼容旧数据库：添加 phone, email, description 字段
	DB.Exec(`ALTER TABLE users ADD COLUMN phone VARCHAR(32) DEFAULT ''`)
	DB.Exec(`ALTER TABLE users ADD COLUMN email VARCHAR(128) DEFAULT ''`)
	DB.Exec(`ALTER TABLE users ADD COLUMN description TEXT`)
	// 兼容旧数据库：添加 language 字段（用户界面语言）
	DB.Exec(`ALTER TABLE users ADD COLUMN language VARCHAR(10) DEFAULT 'zh-CN'`)
	// 兼容旧数据库：添加 oidc_sub 字段（OIDC用户标识）
	DB.Exec(`ALTER TABLE users ADD COLUMN oidc_sub VARCHAR(255) DEFAULT ''`)
	DB.Exec(`CREATE INDEX idx_users_oidc_sub ON users(oidc_sub)`)
	// 兼容旧数据库：添加 auth_source 字段（认证来源: local, sso）
	DB.Exec(`ALTER TABLE users ADD COLUMN auth_source VARCHAR(20) DEFAULT 'local'`)
	// 自动修复：把有 oidc_sub 的用户标记为 SSO 账号
	DB.Exec(`UPDATE users SET auth_source = 'sso' WHERE oidc_sub IS NOT NULL AND oidc_sub != '' AND (auth_source IS NULL OR auth_source = 'local')`)

	// 创建自定义表格表（多维表格功能）
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS custom_tables (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(128) NOT NULL,
			description TEXT,
			icon VARCHAR(32) DEFAULT 'table',
			column_config JSON,
			created_by VARCHAR(64),
			created_at DATETIME DEFAULT NOW(),
			updated_at DATETIME DEFAULT NOW()
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		log.Printf("创建 custom_tables 表失败: %v", err)
	}

	// 添加 column_config 字段（如果不存在）- MySQL 不支持 IF NOT EXISTS，需要检查
	var columnExists int
	DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'custom_tables' AND COLUMN_NAME = 'column_config'`).Scan(&columnExists)
	if columnExists == 0 {
		_, err = DB.Exec(`ALTER TABLE custom_tables ADD COLUMN column_config JSON`)
		if err != nil {
			log.Printf("添加 column_config 字段失败: %v", err)
		} else {
			log.Printf("成功添加 column_config 字段")
		}
	}

	// 创建自定义列表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS custom_columns (
			id VARCHAR(64) PRIMARY KEY,
			table_id VARCHAR(64) NOT NULL,
			name VARCHAR(128) NOT NULL,
			field_key VARCHAR(64) NOT NULL,
			field_type VARCHAR(32) NOT NULL,
			options JSON,
			required BOOLEAN DEFAULT FALSE,
			default_value TEXT,
			sort_order INT DEFAULT 0,
			created_at DATETIME DEFAULT NOW(),
			INDEX idx_table_id (table_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		log.Printf("创建 custom_columns 表失败: %v", err)
	}

	// 创建自定义行数据表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS custom_rows (
			id VARCHAR(64) PRIMARY KEY,
			table_id VARCHAR(64) NOT NULL,
			data JSON,
			attachments JSON,
			created_by VARCHAR(64),
			created_at DATETIME DEFAULT NOW(),
			updated_at DATETIME DEFAULT NOW(),
			INDEX idx_table_id (table_id),
			INDEX idx_created_at (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		log.Printf("创建 custom_rows 表失败: %v", err)
	}

	// 创建审计日志表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS audit_logs (
			id VARCHAR(64) PRIMARY KEY,
			trace_id VARCHAR(32),
			action VARCHAR(32) NOT NULL,
			record_id VARCHAR(64),
			target_type VARCHAR(32),
			target_id VARCHAR(64),
			operator VARCHAR(128),
			old_data TEXT,
			new_data TEXT,
			changes TEXT,
			ip VARCHAR(64),
			created_at DATETIME,
			INDEX idx_trace_id (trace_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)

	// 添加 trace_id, target_type, target_id 列（如果不存在）
	DB.Exec("ALTER TABLE audit_logs ADD COLUMN trace_id VARCHAR(32) AFTER id")
	DB.Exec("ALTER TABLE audit_logs ADD COLUMN target_type VARCHAR(32) AFTER record_id")
	DB.Exec("ALTER TABLE audit_logs ADD COLUMN target_id VARCHAR(64) AFTER target_type")
	// 添加 method, path, status_code, duration 列（如果不存在）
	DB.Exec("ALTER TABLE audit_logs ADD COLUMN method VARCHAR(10) AFTER ip")
	DB.Exec("ALTER TABLE audit_logs ADD COLUMN path VARCHAR(255) AFTER method")
	DB.Exec("ALTER TABLE audit_logs ADD COLUMN status_code INT DEFAULT 200 AFTER path")
	DB.Exec("ALTER TABLE audit_logs ADD COLUMN duration BIGINT DEFAULT 0 AFTER status_code")
	if err != nil {
		return err
	}

	// 创建会话表（用于多实例部署的会话共享）
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64) NOT NULL,
			token_hash VARCHAR(128) NOT NULL,
			ip VARCHAR(64),
			user_agent TEXT,
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_user_id (user_id),
			INDEX idx_token_hash (token_hash),
			INDEX idx_expires_at (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建 CSRF 令牌表（用于多实例部署的 CSRF 保护）
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS csrf_tokens (
			token VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64),
			expires_at DATETIME NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_expires_at (expires_at)
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

	// 创建自定义指标表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS metrics (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(128) NOT NULL,
			label VARCHAR(255) NOT NULL,
			promql TEXT NOT NULL,
			unit VARCHAR(32),
			group_name VARCHAR(64),
			description TEXT,
			enabled TINYINT(1) DEFAULT 1,
			sort_order INT DEFAULT 0,
			created_at DATETIME,
			created_by VARCHAR(128)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建域名管理表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS domains (
			id VARCHAR(64) PRIMARY KEY,
			project VARCHAR(255) NOT NULL,
			module VARCHAR(255),
			domain_name VARCHAR(512) NOT NULL,
			origin VARCHAR(512),
			cdn_provider VARCHAR(128),
			expire_time DATE,
			cert_expire_time DATE,
			status VARCHAR(32) DEFAULT 'active',
			remark TEXT,
			created_at DATETIME,
			created_by VARCHAR(128),
			updated_at DATETIME,
			updated_by VARCHAR(128)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 兼容旧数据库：添加 env 列到 domains 表
	DB.Exec(`ALTER TABLE domains ADD COLUMN env VARCHAR(32) DEFAULT 'PROD' AFTER cdn_provider`)
	// 添加源站IP列
	DB.Exec(`ALTER TABLE domains ADD COLUMN origin_ip VARCHAR(512) AFTER origin`)

	// 创建记录历史表（用于修改历史和回滚）
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS record_history (
			id VARCHAR(64) PRIMARY KEY,
			record_id VARCHAR(64) NOT NULL,
			action VARCHAR(32) NOT NULL,
			snapshot TEXT NOT NULL,
			changes TEXT,
			created_at DATETIME,
			created_by VARCHAR(128),
			INDEX idx_record_history_record_id (record_id),
			INDEX idx_record_history_created_at (created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建排班员工表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS schedule_employees (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(64) NOT NULL,
			group_name VARCHAR(64) DEFAULT '' COMMENT '组别',
			role VARCHAR(64) DEFAULT '运维工程师',
			avatar_color VARCHAR(128) DEFAULT 'linear-gradient(135deg, #667eea, #764ba2)',
			sort_order INT DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}
	// 兼容旧数据库：添加 group_name 列
	DB.Exec(`ALTER TABLE schedule_employees ADD COLUMN group_name VARCHAR(64) DEFAULT '' COMMENT '组别' AFTER name`)

	// 创建排班记录表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS schedule_shifts (
			id INT AUTO_INCREMENT PRIMARY KEY,
			employee_id INT NOT NULL,
			shift_date DATE NOT NULL,
			shift_type VARCHAR(8) NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_employee_date (employee_id, shift_date),
			INDEX idx_employee_id (employee_id),
			INDEX idx_shift_date (shift_date)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建班次配置表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS schedule_shift_configs (
			id INT AUTO_INCREMENT PRIMARY KEY,
			code VARCHAR(8) NOT NULL,
			label VARCHAR(16) NOT NULL,
			name VARCHAR(32) NOT NULL,
			time_range VARCHAR(32) DEFAULT '-',
			color VARCHAR(16) DEFAULT '#1890ff',
			is_duty BOOLEAN DEFAULT FALSE,
			sort_order INT DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_code (code)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建联系人电话表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS schedule_contacts (
			id INT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(64) NOT NULL,
			phone VARCHAR(32) NOT NULL,
			department VARCHAR(64) DEFAULT '',
			position VARCHAR(64) DEFAULT '',
			remark TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_name (name),
			INDEX idx_phone (phone)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建密码库主密钥表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS vault_master_keys (
			id INT AUTO_INCREMENT PRIMARY KEY,
			user_id VARCHAR(64) NOT NULL,
			master_password_hash VARCHAR(255) NOT NULL,
			encrypted_dek TEXT NOT NULL,
			encrypted_dek_recovery TEXT NOT NULL,
			recovery_key_hash VARCHAR(255) NOT NULL,
			salt VARCHAR(64) NOT NULL,
			is_initialized BOOLEAN DEFAULT TRUE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_user_id (user_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建密码库条目表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS vault_items (
			id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64) NOT NULL,
			folder_id VARCHAR(64) DEFAULT '',
			name TEXT NOT NULL,
			username TEXT,
			password TEXT NOT NULL,
			url TEXT,
			notes TEXT,
			type VARCHAR(32) DEFAULT 'login',
			favorite BOOLEAN DEFAULT FALSE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_vault_items_user (user_id),
			INDEX idx_vault_items_folder (folder_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建密码库文件夹表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS vault_folders (
			id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64) NOT NULL,
			name VARCHAR(255) NOT NULL,
			parent_id VARCHAR(64) DEFAULT '',
			icon VARCHAR(64) DEFAULT 'folder',
			sort_order INT DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_vault_folders_user (user_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建密码库分享表（文件夹/条目共享给其他用户）
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS vault_shares (
			id VARCHAR(64) PRIMARY KEY,
			owner_id VARCHAR(64) NOT NULL COMMENT '拥有者用户ID',
			target_type ENUM('folder', 'item') NOT NULL COMMENT '共享目标类型',
			target_id VARCHAR(64) NOT NULL COMMENT '文件夹或条目ID',
			shared_with VARCHAR(64) NOT NULL COMMENT '共享给的用户ID',
			permission ENUM('read', 'write', 'admin') DEFAULT 'read' COMMENT '权限级别',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME DEFAULT NULL COMMENT '过期时间，NULL表示永久',
			INDEX idx_vault_shares_owner (owner_id),
			INDEX idx_vault_shares_shared (shared_with),
			INDEX idx_vault_shares_target (target_type, target_id),
			UNIQUE KEY uk_vault_shares (target_type, target_id, shared_with)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建密码库用户组表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS vault_groups (
			id VARCHAR(64) PRIMARY KEY,
			owner_id VARCHAR(64) NOT NULL COMMENT '创建者用户ID',
			name VARCHAR(255) NOT NULL,
			description TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_vault_groups_owner (owner_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建密码库用户组成员表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS vault_group_members (
			id VARCHAR(64) PRIMARY KEY,
			group_id VARCHAR(64) NOT NULL,
			user_id VARCHAR(64) NOT NULL,
			role ENUM('member', 'admin') DEFAULT 'member' COMMENT '组内角色',
			added_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			added_by VARCHAR(64) NOT NULL COMMENT '添加者',
			INDEX idx_vault_gm_group (group_id),
			INDEX idx_vault_gm_user (user_id),
			UNIQUE KEY uk_vault_gm (group_id, user_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// ========== 商户管理表 ==========
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS merchants (
			id VARCHAR(64) PRIMARY KEY,
			project VARCHAR(255) DEFAULT '',
			env VARCHAR(32) DEFAULT 'prod',
			website_name VARCHAR(255) NOT NULL DEFAULT '',
			contact_emails TEXT COMMENT '对接邮箱JSON数组',
			website_urls TEXT COMMENT '网站方网址JSON数组',
			player_regions TEXT COMMENT '玩家地区JSON数组',
			estimated_players VARCHAR(64) DEFAULT '',
			game_types TEXT COMMENT '游戏种类JSON数组',
			handicaps TEXT COMMENT '盘口JSON数组',
			languages TEXT COMMENT '语言JSON数组',
			currencies TEXT COMMENT '币种JSON数组',
			supported_ports TEXT COMMENT '支持端口JSON数组',
			wallet_types TEXT COMMENT '钱包类型JSON数组',
			callback_domains TEXT COMMENT '三方回调域名JSON数组',
			whitelist_ips TEXT COMMENT '三方白名单',
			hall_domains TEXT COMMENT '三方调用厅房域名JSON数组',
			site_domains TEXT COMMENT '厅方站点系统域名JSON数组',
			site_accounts TEXT COMMENT '站点系统账号JSON数组',
			app_keys TEXT COMMENT 'appkey JSON数组',
			app_secrets TEXT COMMENT 'appsecret 密码系统查看',
			game_domains TEXT COMMENT '游戏域名JSON数组',
			redirect_domains TEXT COMMENT '301域名JSON数组',
			custom_fields JSON COMMENT '自定义字段JSON对象',
			remark TEXT,
			status VARCHAR(32) DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_by VARCHAR(128),
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			updated_by VARCHAR(128),
			INDEX idx_merchant_project (project),
			INDEX idx_merchant_env (env),
			INDEX idx_merchant_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}
	// 兼容已有数据库：添加 custom_fields 字段
	DB.Exec(`ALTER TABLE merchants ADD COLUMN custom_fields JSON COMMENT '自定义字段JSON对象' AFTER redirect_domains`)

	// ========== 商户自定义列表（全局共享） ==========
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS merchant_custom_columns (
			id VARCHAR(64) PRIMARY KEY,
			col_key VARCHAR(100) NOT NULL COMMENT '列标识，如 custom_note',
			col_title VARCHAR(100) NOT NULL DEFAULT '' COMMENT '列显示名称',
			col_type VARCHAR(32) DEFAULT 'text' COMMENT '列类型: text, multi, tags, tag',
			col_width VARCHAR(32) DEFAULT '120px' COMMENT '列宽度',
			sort_order INT DEFAULT 0 COMMENT '排序顺序',
			is_active BOOLEAN DEFAULT TRUE COMMENT '是否启用',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_by VARCHAR(128),
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_col_key (col_key)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// ========== 任务池表 ==========
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id VARCHAR(64) PRIMARY KEY,
			project VARCHAR(255) NOT NULL DEFAULT '' COMMENT '项目',
			title TEXT NOT NULL COMMENT '需求描述',
			source VARCHAR(64) DEFAULT 'other' COMMENT '需求来源',
			category VARCHAR(64) DEFAULT 'feature' COMMENT '任务分类',
			priority VARCHAR(8) DEFAULT 'P2' COMMENT '优先级',
			assignee VARCHAR(128) NOT NULL DEFAULT '' COMMENT '负责人',
			start_time DATE DEFAULT NULL COMMENT '开始时间',
			end_time DATE DEFAULT NULL COMMENT '结束时间',
			status VARCHAR(32) DEFAULT 'pending' COMMENT '状态',
			result TEXT COMMENT '结果',
			remark TEXT COMMENT '备注',
			is_delayed TINYINT(1) DEFAULT 0 COMMENT '是否延期',
			delay_reason VARCHAR(64) DEFAULT '' COMMENT '延期分类',
			delay_desc TEXT COMMENT '延期说明',
			delay_end_time DATE DEFAULT NULL COMMENT '延期后结束时间',
			completion_type VARCHAR(32) DEFAULT '' COMMENT '完成分类',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_by VARCHAR(128),
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			updated_by VARCHAR(128),
			INDEX idx_task_project (project),
			INDEX idx_task_assignee (assignee),
			INDEX idx_task_status (status),
			INDEX idx_task_priority (priority),
			INDEX idx_task_delayed (is_delayed)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// ========== 员工失误/异常记录表 ==========
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS incidents (
			id VARCHAR(64) PRIMARY KEY,
			incident_time DATETIME NOT NULL COMMENT '发生时间',
			operator VARCHAR(128) NOT NULL COMMENT '操作人',
			operation_type VARCHAR(64) NOT NULL DEFAULT 'other' COMMENT '操作类型',
			operation_desc TEXT COMMENT '操作描述',
			status VARCHAR(32) DEFAULT 'pending' COMMENT '状态: pending, resolved, closed',
			severity VARCHAR(32) DEFAULT 'medium' COMMENT '严重程度: low, medium, high, critical',
			reason TEXT COMMENT '异常原因',
			impact TEXT COMMENT '影响范围',
			solution TEXT COMMENT '解决方案',
			checker VARCHAR(128) DEFAULT '' COMMENT '检查人',
			check_time DATETIME DEFAULT NULL COMMENT '检查时间',
			check_result TEXT COMMENT '检查结果',
			remark TEXT COMMENT '备注',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_by VARCHAR(128),
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			updated_by VARCHAR(128),
			INDEX idx_incident_time (incident_time),
			INDEX idx_incident_operator (operator),
			INDEX idx_incident_status (status),
			INDEX idx_incident_type (operation_type)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// ========== 值班记录相关表 ==========

	// 值班项目配置表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS duty_projects (
			id VARCHAR(64) PRIMARY KEY,
			name VARCHAR(128) NOT NULL COMMENT '项目名称',
			code VARCHAR(64) NOT NULL COMMENT '项目代码',
			description TEXT COMMENT '项目描述',
			status VARCHAR(32) DEFAULT 'active' COMMENT 'active/disabled',
			sort_order INT DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_by VARCHAR(128),
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_code (code),
			INDEX idx_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 值班记录主表
	_, err = DB.Exec(`
CREATE TABLE IF NOT EXISTS duty_records (
			id VARCHAR(64) PRIMARY KEY,
			duty_date DATETIME NOT NULL COMMENT '值班日期时间',
			duty_person VARCHAR(128) NOT NULL COMMENT '值班人',
			project_id VARCHAR(64) NOT NULL COMMENT '项目ID',
			task_desc TEXT COMMENT '任务描述',
			feedback_type VARCHAR(32) DEFAULT 'customer' COMMENT 'proactive=主动反馈, customer=客户反馈',
			event_type VARCHAR(32) DEFAULT 'customer_feedback' COMMENT '事件类型: inspection=巡检发现, alert=监控告警, customer_feedback=客户反馈, proactive_check=值班人员主动排查',
			handler VARCHAR(128) DEFAULT '' COMMENT '处理人',
			handle_result TEXT COMMENT '处理结果',
			solution TEXT COMMENT '解决方案',
			problem_desc TEXT COMMENT '问题描述',

			first_call_time DATETIME DEFAULT NULL COMMENT '首次拨打时间',
			answer_time DATETIME DEFAULT NULL COMMENT '接听时间',
			call_count INT DEFAULT 0 COMMENT '拨打次数',
			is_answered VARCHAR(16) DEFAULT '无' COMMENT '是否接听: 无/已接听/未接听',
			response_time INT DEFAULT 0 COMMENT '响应时间(分钟)',

			is_escalated TINYINT(1) DEFAULT 0 COMMENT '是否升级问题',
			escalate_to VARCHAR(64) DEFAULT '' COMMENT '升级给谁: leader=组长, hod=HOD',

			has_handover TINYINT(1) DEFAULT 0 COMMENT '是否有工作交接',
			handover_person VARCHAR(128) DEFAULT '' COMMENT '工作交接人',
			handover_content TEXT COMMENT '工作交接内容',

			status VARCHAR(32) DEFAULT 'pending' COMMENT 'pending=待解决, in_progress=正在解决, resolved=已解决, temporary=临时解决',
			planned_fix_time DATETIME DEFAULT NULL COMMENT '计划修复时间',
			planned_fix_time_edited TINYINT(1) DEFAULT 0 COMMENT '计划修复时间是否被编辑过',
			is_overdue TINYINT(1) DEFAULT 0 COMMENT '是否逾期',
			overdue_reason TEXT COMMENT '逾期原因',

			attachments JSON COMMENT '附件列表(图片URL数组)',
			
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_by VARCHAR(128),
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			updated_by VARCHAR(128),
			INDEX idx_duty_date (duty_date),
			INDEX idx_duty_person (duty_person),
			INDEX idx_project (project_id),
			INDEX idx_handler (handler),
			INDEX idx_status (status),
			INDEX idx_overdue (is_overdue)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 兼容已有数据库：添加 event_type 字段和修改 duty_date 为 DATETIME
	DB.Exec(`ALTER TABLE duty_records ADD COLUMN event_type VARCHAR(32) DEFAULT 'customer_feedback' COMMENT '事件类型' AFTER feedback_type`)
	DB.Exec(`ALTER TABLE duty_records MODIFY COLUMN duty_date DATETIME NOT NULL COMMENT '值班日期时间'`)
	// 添加 planned_fix_time_edited 字段跟踪是否被编辑过
	DB.Exec(`ALTER TABLE duty_records ADD COLUMN planned_fix_time_edited TINYINT(1) DEFAULT 0 COMMENT '计划修复时间是否被编辑过' AFTER planned_fix_time`)
	// 将 planned_fix_time 改为 DATETIME 类型以支持时分秒
	DB.Exec(`ALTER TABLE duty_records MODIFY COLUMN planned_fix_time DATETIME DEFAULT NULL COMMENT '计划修复时间'`)
	// 将 is_answered 从 TINYINT 改为 VARCHAR 以支持 无/已接听/未接听
	DB.Exec(`ALTER TABLE duty_records MODIFY COLUMN is_answered VARCHAR(16) DEFAULT '无' COMMENT '是否接听: 无/已接听/未接听'`)
	DB.Exec(`ALTER TABLE duty_records ADD COLUMN solution TEXT COMMENT '解决方案' AFTER handle_result`)
	// 迁移旧数据: 1->已接听, 0->无
	DB.Exec(`UPDATE duty_records SET is_answered='已接听' WHERE is_answered='1'`)
	DB.Exec(`UPDATE duty_records SET is_answered='无' WHERE is_answered='0' OR is_answered=''`)

	// ========== 文件分享表 ==========
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS file_shares (
			id VARCHAR(64) PRIMARY KEY,
			code VARCHAR(32) NOT NULL COMMENT '分享码',
			file_path VARCHAR(512) NOT NULL COMMENT '文件路径（object name）',
			file_name VARCHAR(255) DEFAULT '' COMMENT '原始文件名',
			expires_at DATETIME DEFAULT NULL COMMENT '过期时间，NULL表示永久',
			view_count INT DEFAULT 0 COMMENT '查看次数',
			max_views INT DEFAULT 0 COMMENT '最大查看次数，0表示无限制',
			password VARCHAR(128) DEFAULT '' COMMENT '访问密码（可选）',
			created_by VARCHAR(128) NOT NULL COMMENT '创建人',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE KEY uk_code (code),
			INDEX idx_file_path (file_path),
			INDEX idx_expires (expires_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// ========== 服务配置信息表 ==========

	// 创建服务配置表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS service_configs (
			id VARCHAR(64) PRIMARY KEY,
			project VARCHAR(255) NOT NULL DEFAULT '' COMMENT '项目名称',
			service_name VARCHAR(255) NOT NULL COMMENT '服务名称',
			service_type VARCHAR(64) NOT NULL DEFAULT 'backend' COMMENT '服务类型: web, backend, middleware, database, cache, mq, gateway, third_party',
			domain VARCHAR(512) DEFAULT '' COMMENT '域名',
			port VARCHAR(32) DEFAULT '' COMMENT '端口',
			env VARCHAR(32) DEFAULT 'prod' COMMENT '环境',
			namespace VARCHAR(128) DEFAULT '' COMMENT 'K8s命名空间',
			replicas INT DEFAULT 1 COMMENT '副本数',
			image VARCHAR(512) DEFAULT '' COMMENT '镜像地址',
			remark TEXT COMMENT '备注',
			status VARCHAR(32) DEFAULT 'active',
			sort_order INT DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_by VARCHAR(128),
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			updated_by VARCHAR(128),
			INDEX idx_sc_project (project),
			INDEX idx_sc_env (env),
			INDEX idx_sc_type (service_type)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建服务依赖关系表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS service_dependencies (
			id VARCHAR(64) PRIMARY KEY,
			service_id VARCHAR(64) NOT NULL COMMENT '所属服务ID',
			dependency_type VARCHAR(64) NOT NULL DEFAULT 'other' COMMENT '依赖类型: mysql, redis, mq, api, third_party, mongodb, elasticsearch, other',
			dependency_name VARCHAR(255) NOT NULL COMMENT '依赖名称',
			host VARCHAR(512) DEFAULT '' COMMENT '连接地址',
			port VARCHAR(32) DEFAULT '' COMMENT '端口',
			database_name VARCHAR(128) DEFAULT '' COMMENT '数据库名',
			username VARCHAR(128) DEFAULT '' COMMENT '用户名',
			password VARCHAR(512) DEFAULT '' COMMENT '密码(加密)',
			conn_string TEXT COMMENT '连接串',
			remark TEXT COMMENT '备注',
			status VARCHAR(32) DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_sd_service (service_id),
			INDEX idx_sd_type (dependency_type)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// ========== 权限管理表 ==========

	// 创建角色表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS roles (
			id VARCHAR(64) PRIMARY KEY,
			code VARCHAR(64) NOT NULL UNIQUE COMMENT '角色代码，如 admin, operator',
			name VARCHAR(128) NOT NULL COMMENT '角色名称',
			description TEXT,
			is_system TINYINT(1) DEFAULT 0 COMMENT '是否系统内置角色',
			status ENUM('active', 'disabled') DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建权限表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS permissions (
			id VARCHAR(64) PRIMARY KEY,
			code VARCHAR(128) NOT NULL UNIQUE COMMENT '权限代码，如 user:read, menu:system',
			name VARCHAR(128) NOT NULL COMMENT '权限名称',
			type ENUM('menu', 'button', 'data', 'api') NOT NULL COMMENT '权限类型',
			resource VARCHAR(255) COMMENT '资源路径或标识',
			parent_id VARCHAR(64) DEFAULT '' COMMENT '父权限ID，用于菜单层级',
			icon VARCHAR(64) DEFAULT '' COMMENT '图标',
			sort_order INT DEFAULT 0,
			description TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_permissions_type (type),
			INDEX idx_permissions_parent (parent_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建角色-权限关联表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS role_permissions (
			id VARCHAR(64) PRIMARY KEY,
			role_id VARCHAR(64) NOT NULL,
			permission_id VARCHAR(64) NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_rp_role (role_id),
			INDEX idx_rp_permission (permission_id),
			UNIQUE KEY uk_role_permission (role_id, permission_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建用户-角色关联表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS user_roles (
			id VARCHAR(64) PRIMARY KEY,
			user_id VARCHAR(64) NOT NULL,
			role_id VARCHAR(64) NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_ur_user (user_id),
			INDEX idx_ur_role (role_id),
			UNIQUE KEY uk_user_role (user_id, role_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 创建外部应用表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS external_apps (
			id VARCHAR(64) PRIMARY KEY,
			app_key VARCHAR(64) NOT NULL UNIQUE,
			name VARCHAR(128) NOT NULL,
			url VARCHAR(512) NOT NULL,
			icon_svg TEXT,
			group_name VARCHAR(64) DEFAULT '',
			sort_order INT DEFAULT 0,
			status VARCHAR(16) DEFAULT 'active',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// Auto-migrate: add perm_code column if not exists
	var permCodeCount int
	DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'external_apps' AND COLUMN_NAME = 'perm_code'`).Scan(&permCodeCount)
	if permCodeCount == 0 {
		DB.Exec("ALTER TABLE external_apps ADD COLUMN perm_code VARCHAR(64) DEFAULT '' COMMENT '权限码前缀(如 alert, confluence)' AFTER url")
		log.Println("[Migration] Added column external_apps.perm_code")
	}

	// 创建角色-外部应用关联表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS role_external_apps (
			id VARCHAR(64) PRIMARY KEY,
			role_id VARCHAR(64) NOT NULL,
			app_key VARCHAR(64) NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_rea_role (role_id),
			INDEX idx_rea_app (app_key),
			UNIQUE KEY uk_role_app (role_id, app_key)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 发布中心：角色 → 可访问项目环境（env_name 为发布中心的 project_env.name，如 "g32-uat"）
	// 运维平台只存 env_name 字符串，不存发布中心的 env ID；管理页从发布中心拉实时列表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS role_deploy_envs (
			id INT AUTO_INCREMENT PRIMARY KEY,
			role_id VARCHAR(64) NOT NULL,
			env_name VARCHAR(100) NOT NULL COMMENT '发布中心 project_env.name，如 g32-uat',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_rde_role (role_id),
			UNIQUE KEY uk_role_env (role_id, env_name)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 发布中心：角色 → 可访问项目（项目级权限，与 env 级 AND 关系：必须同时勾才生效）
	// project_name 为发布中心 project_env.name 去掉 -uat/-prod 后缀，如 "g32"
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS role_deploy_projects (
			id INT AUTO_INCREMENT PRIMARY KEY,
			role_id VARCHAR(64) NOT NULL,
			project_name VARCHAR(100) NOT NULL COMMENT '发布中心项目名，如 g32',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_rdp_role (role_id),
			UNIQUE KEY uk_role_project (role_id, project_name)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// 一次性回填：把已存在的 role_deploy_envs 的项目名补到 role_deploy_projects
	// 仅当 role_deploy_projects 完全空时跑（避免覆盖 admin 后续的手动调整）
	var rdpCnt int
	_ = DB.QueryRow(`SELECT COUNT(*) FROM role_deploy_projects`).Scan(&rdpCnt)
	if rdpCnt == 0 {
		// 用 SQL 直接 derive：name 形如 g32-uat / g01-lpt → 去掉 "-uat" / "-prod" / "-lpt" 后缀；不带后缀的整个名当项目名
		// REGEXP_REPLACE: 把末尾的 -(uat|prod|lpt) 切掉
		_, err := DB.Exec(`
			INSERT IGNORE INTO role_deploy_projects (role_id, project_name)
			SELECT DISTINCT role_id, REGEXP_REPLACE(env_name, '-(uat|prod|lpt)$', '') FROM role_deploy_envs
		`)
		if err != nil {
			log.Printf("[migration] backfill role_deploy_projects failed (non-fatal): %v", err)
		}
	}

	// 创建 API Key 表
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS api_keys (
			id VARCHAR(36) PRIMARY KEY,
			name VARCHAR(100) NOT NULL,
			description VARCHAR(500) DEFAULT '',
			key_hash CHAR(64) NOT NULL UNIQUE,
			key_prefix VARCHAR(16) NOT NULL COMMENT '前缀 opsk_ + 8 位明文，共13字符',
			key_suffix VARCHAR(6) NOT NULL COMMENT '后6位明文',
			domain VARCHAR(32) NOT NULL COMMENT '业务域：table_maintenance 等',
			scopes TEXT NOT NULL COMMENT 'JSON 数组：权限码列表',
			allowed_table_ids TEXT COMMENT 'JSON 数组：允许访问的自定义表ID，NULL/空=不限制',
			enabled TINYINT(1) NOT NULL DEFAULT 1,
			expires_at DATETIME NULL COMMENT 'NULL=永久有效',
			last_used_at DATETIME NULL,
			created_by VARCHAR(64) DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			INDEX idx_apikey_hash (key_hash),
			INDEX idx_apikey_domain (domain),
			INDEX idx_apikey_enabled (enabled)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// ===== 桌台管理（新菜单，独立于桌台层级配置）=====
	// 项目数据源配置：每个项目 + 环境挂一份外部 OpenAPI 接口配置
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS external_data_sources (
			id VARCHAR(36) PRIMARY KEY,
			project VARCHAR(100) NOT NULL COMMENT '项目名（name_zh 或 key）',
			env VARCHAR(8) NOT NULL DEFAULT 'PROD' COMMENT '环境：UAT / PROD',
			url TEXT NOT NULL COMMENT '外部 OpenAPI 接口地址',
			method VARCHAR(10) NOT NULL DEFAULT 'POST' COMMENT 'HTTP 方法：GET / POST',
			request_body TEXT NOT NULL COMMENT 'JSON 请求体模板（仅 POST 有效）',
			data_path VARCHAR(128) DEFAULT 'data.data' COMMENT '响应 JSON 里桌台数组的路径，点分（如 data / data.data / data.list）',
			field_map TEXT COMMENT 'JSON 对象：外部字段名 → 内部字段名（platform_id/platform_name/platform_name_zh/room_id/game_type/game_type_name/room_status）',
			status_map TEXT COMMENT 'JSON：外部状态值 → 内部状态（enabled/disabled），格式 {"enabled":["0","Enable"],"disabled":["1","Disable"]}',
			enabled TINYINT(1) NOT NULL DEFAULT 1,
			last_synced_at DATETIME NULL COMMENT '上次成功同步时间',
			last_sync_status VARCHAR(20) DEFAULT '' COMMENT '上次同步状态：success/failed/never',
			last_sync_error TEXT COMMENT '上次失败的错误信息',
			last_sync_count INT DEFAULT 0 COMMENT '上次同步拉到的桌台数',
			created_by VARCHAR(64) DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_project_env (project, env)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}
	// Auto-migrate: 加新字段（兼容 v557/v729 已部署的实例）
	for _, col := range []struct{ name, typ string }{
		{"method", "VARCHAR(10) NOT NULL DEFAULT 'POST'"},
		{"data_path", "VARCHAR(128) DEFAULT 'data.data'"},
		{"field_map", "TEXT"},
		{"status_map", "TEXT"},
		{"env", "VARCHAR(8) NOT NULL DEFAULT 'PROD'"},
	} {
		var n int
		DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'external_data_sources' AND COLUMN_NAME = ?`, col.name).Scan(&n)
		if n == 0 {
			DB.Exec("ALTER TABLE external_data_sources ADD COLUMN " + col.name + " " + col.typ)
			log.Printf("[Migration] external_data_sources add column %s", col.name)
		}
	}
	// 把老的 UNIQUE KEY (project) 升级成 (project, env)
	var oldUkExists int
	DB.QueryRow(`SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='external_data_sources' AND INDEX_NAME='uk_project'`).Scan(&oldUkExists)
	if oldUkExists > 0 {
		DB.Exec(`ALTER TABLE external_data_sources DROP INDEX uk_project`)
		DB.Exec(`ALTER TABLE external_data_sources ADD UNIQUE KEY uk_project_env (project, env)`)
		log.Printf("[Migration] external_data_sources: uk_project → uk_project_env")
	}

	// 同步过来的桌台缓存：每条来自外部 API
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS external_rooms (
			id VARCHAR(36) PRIMARY KEY,
			project VARCHAR(100) NOT NULL COMMENT '项目（关联 external_data_sources.project）',
			env VARCHAR(8) NOT NULL DEFAULT 'PROD' COMMENT '环境：UAT / PROD',
			platform_id VARCHAR(64) NOT NULL COMMENT '外部 platformId',
			platform_name VARCHAR(64) NOT NULL COMMENT '外部 platformName，英文/代号',
			platform_name_zh VARCHAR(128) DEFAULT '' COMMENT '外部返回的中文，可被别名覆盖',
			room_id VARCHAR(64) NOT NULL COMMENT '桌台 ID',
			game_type VARCHAR(64) NOT NULL COMMENT '外部 gameType 英文代号',
			room_status TINYINT NOT NULL DEFAULT 0 COMMENT '外部 roomStatus: 0=enable, 1=disable, 2=maintenance',
			synced_at DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '同步入库时间',
			deleted_at DATETIME NULL COMMENT '软删除：外部 API 已不再返回此桌台时打标',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_room (project, env, platform_id, room_id),
			INDEX idx_ext_room_project (project),
			INDEX idx_ext_room_env (env),
			INDEX idx_ext_room_status (room_status),
			INDEX idx_ext_room_deleted (deleted_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}
	// Auto-migrate: 给老 external_rooms 加 env 字段
	var roomEnvCol int
	DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'external_rooms' AND COLUMN_NAME = 'env'`).Scan(&roomEnvCol)
	if roomEnvCol == 0 {
		DB.Exec(`ALTER TABLE external_rooms ADD COLUMN env VARCHAR(8) NOT NULL DEFAULT 'PROD'`)
		DB.Exec(`ALTER TABLE external_rooms ADD INDEX idx_ext_room_env (env)`)
		log.Printf("[Migration] external_rooms add column env + index")
	}
	// 升级 UNIQUE KEY uk_room (project, platform_id, room_id) → (project, env, platform_id, room_id)
	var oldRoomUk int
	DB.QueryRow(`SELECT COUNT(*) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='external_rooms' AND INDEX_NAME='uk_room' AND COLUMN_NAME='env'`).Scan(&oldRoomUk)
	if oldRoomUk == 0 {
		DB.Exec(`ALTER TABLE external_rooms DROP INDEX uk_room`)
		DB.Exec(`ALTER TABLE external_rooms ADD UNIQUE KEY uk_room (project, env, platform_id, room_id)`)
		log.Printf("[Migration] external_rooms: uk_room 升级带 env")
	}

	// 别名映射表：英文 code → 中文显示名（全局）
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS external_aliases (
			id VARCHAR(36) PRIMARY KEY,
			alias_type VARCHAR(20) NOT NULL COMMENT 'platform / gameType',
			code VARCHAR(64) NOT NULL COMMENT '英文代号，如 BAC / AGEU',
			name_zh VARCHAR(128) NOT NULL DEFAULT '' COMMENT '用户编辑的中文名；空时 UI 回退到 code',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_alias (alias_type, code)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}

	// Auto-migrate: 扩大 api_keys.key_prefix 到 VARCHAR(16)（旧版 12 位不够）
	var keyPrefixLen int
	DB.QueryRow(`SELECT CHARACTER_MAXIMUM_LENGTH FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'api_keys' AND COLUMN_NAME = 'key_prefix'`).Scan(&keyPrefixLen)
	if keyPrefixLen > 0 && keyPrefixLen < 16 {
		DB.Exec("ALTER TABLE api_keys MODIFY key_prefix VARCHAR(16) NOT NULL")
		log.Println("[Migration] Expanded api_keys.key_prefix to VARCHAR(16)")
	}

	// 初始化默认角色和权限
	initDefaultRolesAndPermissions()

	// 创建索引（提升查询性能）
	createIndexes()

	return nil
}

// createIndexes 创建数据库索引（提升查询性能）
func createIndexes() {
	indexes := []string{
		// records 表索引
		"CREATE INDEX idx_records_project ON records(project)",
		"CREATE INDEX idx_records_env ON records(env)",
		"CREATE INDEX idx_records_status ON records(status)",
		"CREATE INDEX idx_records_created_at ON records(created_at)",
		"CREATE INDEX idx_records_updated_at ON records(updated_at)",

		// users 表索引
		"CREATE INDEX idx_users_role ON users(role)",
		"CREATE INDEX idx_users_status ON users(status)",

		// audit_logs 表索引
		"CREATE INDEX idx_audit_action ON audit_logs(action)",
		"CREATE INDEX idx_audit_operator ON audit_logs(operator)",
		"CREATE INDEX idx_audit_created_at ON audit_logs(created_at)",
		"CREATE INDEX idx_audit_record_id ON audit_logs(record_id)",

		// datasources 表索引
		"CREATE INDEX idx_datasources_type ON datasources(type)",
		"CREATE INDEX idx_datasources_status ON datasources(status)",

		// domains 表索引
		"CREATE INDEX idx_domains_project ON domains(project)",
		"CREATE INDEX idx_domains_status ON domains(status)",
		"CREATE INDEX idx_domains_expire_time ON domains(expire_time)",
		"CREATE INDEX idx_domains_cert_expire_time ON domains(cert_expire_time)",
	}

	for _, sql := range indexes {
		// 忽略错误（索引可能已存在，MySQL 会报 Duplicate key name）
		DB.Exec(sql)
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// initDefaultRolesAndPermissions 初始化默认角色和权限
func initDefaultRolesAndPermissions() {
	log.Println("开始初始化默认角色和权限...")

	// 默认角色
	defaultRoles := []struct {
		ID          string
		Code        string
		Name        string
		Description string
		IsSystem    int
	}{
		{"role_super_admin", "super_admin", "超级管理员", "拥有系统所有权限", 1},
		{"role_admin", "admin", "管理员", "系统管理权限，可管理用户和配置", 1},
		{"role_operator", "operator", "运维人员", "资源管理和运维操作权限", 1},
		{"role_viewer", "viewer", "只读用户", "只能查看，不能修改", 1},
	}

	for _, role := range defaultRoles {
		result, err := DB.Exec(`INSERT IGNORE INTO roles (id, code, name, description, is_system) VALUES (?, ?, ?, ?, ?)`,
			role.ID, role.Code, role.Name, role.Description, role.IsSystem)
		if err != nil {
			log.Printf("插入角色失败 %s: %v", role.Code, err)
		} else {
			affected, _ := result.RowsAffected()
			if affected > 0 {
				log.Printf("创建角色: %s", role.Name)
			}
		}
	}

	// 默认权限 - 菜单权限
	menuPermissions := []struct {
		ID       string
		Code     string
		Name     string
		Resource string
		ParentID string
		Icon     string
		Sort     int
	}{
		// 系统管理
		{"perm_menu_system", "menu:system", "系统管理", "/system", "", "system", 1},
		{"perm_menu_welcome", "menu:welcome", "欢迎页", "/system/welcome", "perm_menu_system", "", 10},
		{"perm_menu_users", "menu:users", "用户管理", "/system/users", "perm_menu_system", "", 20},
		{"perm_menu_roles", "menu:roles", "角色管理", "/system/roles", "perm_menu_system", "", 30},
		{"perm_menu_permissions", "menu:permissions", "权限配置", "/system/permissions", "perm_menu_system", "", 40},
		{"perm_menu_audit", "menu:audit", "审计日志", "/system/audit", "perm_menu_system", "", 50},
		{"perm_menu_api", "menu:api", "接口管理", "/system/api", "perm_menu_system", "", 60},
		{"perm_menu_schedule", "menu:schedule", "排班管理", "/system/schedule", "perm_menu_system", "", 70},
		{"perm_menu_taskpool", "menu:taskpool", "任务池", "/system/taskpool", "perm_menu_system", "", 80},
		{"perm_menu_incidents", "menu:incidents", "事件记录", "/system/incidents", "perm_menu_system", "", 90},
		{"perm_menu_duty", "menu:duty", "值班记录", "/system/duty", "perm_menu_system", "", 100},
		{"perm_menu_duty_projects", "menu:duty_projects", "值班项目", "/system/duty-projects", "perm_menu_system", "", 110},
		{"perm_menu_table_maintenance", "menu:table_maintenance", "桌台维护记录", "/system/table-maintenance", "perm_menu_system", "", 120},
		{"perm_menu_table_hierarchy_config", "menu:table_hierarchy_config", "桌台层级配置", "/system/table-hierarchy-config", "perm_menu_system", "", 130},
		{"perm_menu_table_management", "menu:table_management", "桌台管理", "/system/table-management", "perm_menu_system", "", 135},
		{"perm_menu_api_keys", "menu:api_keys", "API Key 管理", "/system/api-keys", "perm_menu_system", "", 140},

		// 资源管理
		{"perm_menu_resource", "menu:resource", "资源管理", "/resource", "", "resource", 2},
		{"perm_menu_assets", "menu:assets", "资产管理", "/resource/assets", "perm_menu_resource", "", 10},
		{"perm_menu_domains", "menu:domains", "域名管理", "/resource/domains", "perm_menu_resource", "", 20},
		{"perm_menu_merchants", "menu:merchants", "商户管理", "/resource/merchants", "perm_menu_resource", "", 25},
		{"perm_menu_network", "menu:network", "网络管理", "/resource/network", "perm_menu_resource", "", 30},
		{"perm_menu_serviceconfig", "menu:serviceconfig", "服务配置", "/resource/serviceconfig", "perm_menu_resource", "", 35},
		{"perm_menu_topology", "menu:topology", "服务拓扑", "/resource/topology", "perm_menu_resource", "", 40},

		// 监控告警
		{"perm_menu_monitor", "menu:monitor", "监控告警", "/monitor", "", "monitor", 3},
		{"perm_menu_metrics", "menu:metrics", "指标监控", "/monitor/metrics", "perm_menu_monitor", "", 10},
		{"perm_menu_alerts", "menu:alerts", "告警管理", "/monitor/alerts", "perm_menu_monitor", "", 20},
		{"perm_menu_alertrules", "menu:alertrules", "告警规则", "/monitor/alertrules", "perm_menu_monitor", "", 30},
		{"perm_menu_alertnotify", "menu:alertnotify", "通知配置", "/monitor/alertnotify", "perm_menu_monitor", "", 40},
		{"perm_menu_dashboard", "menu:dashboard", "大屏展示", "/monitor/dashboard", "perm_menu_monitor", "", 50},

		// K8S运维
		{"perm_menu_k8s", "menu:k8s", "K8S运维", "/k8s", "", "k8s", 4},
		{"perm_menu_clusters", "menu:clusters", "集群管理", "/k8s/clusters", "perm_menu_k8s", "", 10},
		{"perm_menu_workloads", "menu:workloads", "工作负载", "/k8s/workloads", "perm_menu_k8s", "", 20},
		{"perm_menu_configmaps", "menu:configmaps", "配置管理", "/k8s/configmaps", "perm_menu_k8s", "", 30},
		{"perm_menu_storage", "menu:storage", "存储管理", "/k8s/storage", "perm_menu_k8s", "", 40},
		{"perm_menu_terminal", "menu:terminal", "容器终端", "/k8s/terminal", "perm_menu_k8s", "", 50},

		// 工单系统
		{"perm_menu_ticket", "menu:ticket", "工单系统", "/ticket", "", "ticket", 5},
		{"perm_menu_tickets", "menu:tickets", "工单管理", "/ticket/tickets", "perm_menu_ticket", "", 10},
		{"perm_menu_sla", "menu:sla", "SLA管理", "/ticket/sla", "perm_menu_ticket", "", 20},
		{"perm_menu_tickettemplate", "menu:tickettemplate", "工单模板", "/ticket/template", "perm_menu_ticket", "", 30},

		// 自动化运维
		{"perm_menu_automation", "menu:automation", "自动化运维", "/automation", "", "automation", 6},
		{"perm_menu_jobs", "menu:jobs", "作业平台", "/automation/jobs", "perm_menu_automation", "", 10},
		{"perm_menu_crontab", "menu:crontab", "定时任务", "/automation/crontab", "perm_menu_automation", "", 20},
		{"perm_menu_inspection", "menu:inspection", "自动巡检", "/automation/inspection", "perm_menu_automation", "", 30},
		{"perm_menu_selfhealing", "menu:selfhealing", "自愈策略", "/automation/selfhealing", "perm_menu_automation", "", 40},

		// 智能运维
		{"perm_menu_aiops", "menu:aiops", "智能运维", "/aiops", "", "aiops", 7},
		{"perm_menu_anomaly", "menu:anomaly", "异常检测", "/aiops/anomaly", "perm_menu_aiops", "", 10},
		{"perm_menu_rootcause", "menu:rootcause", "根因分析", "/aiops/rootcause", "perm_menu_aiops", "", 20},
		{"perm_menu_predict", "menu:predict", "故障预测", "/aiops/predict", "perm_menu_aiops", "", 30},
		{"perm_menu_smartalert", "menu:smartalert", "智能告警", "/aiops/smartalert", "perm_menu_aiops", "", 40},
		{"perm_menu_capacity", "menu:capacity", "容量预测", "/aiops/capacity", "perm_menu_aiops", "", 50},

		// 变更发布
		{"perm_menu_release", "menu:release", "变更发布", "/release", "", "release", 8},
		{"perm_menu_deploy", "menu:deploy", "发布管理", "/release/deploy", "perm_menu_release", "", 10},
		{"perm_menu_change", "menu:change", "变更管理", "/release/change", "perm_menu_release", "", 20},
		{"perm_menu_rollback", "menu:rollback", "回滚管理", "/release/rollback", "perm_menu_release", "", 30},

		// 日志服务
		{"perm_menu_logs", "menu:logs", "日志服务", "/logs", "", "logs", 9},
		{"perm_menu_logquery", "menu:logquery", "日志查询", "/logs/query", "perm_menu_logs", "", 10},
		{"perm_menu_loganalysis", "menu:loganalysis", "日志分析", "/logs/analysis", "perm_menu_logs", "", 20},
		{"perm_menu_logalert", "menu:logalert", "日志告警", "/logs/alert", "perm_menu_logs", "", 30},

		// 安全工具
		{"perm_menu_security", "menu:security", "安全工具", "/security", "", "security", 10},
		{"perm_menu_vault", "menu:vault", "密码库", "/security/vault", "perm_menu_security", "", 10},
		{"perm_menu_secrets", "menu:secrets", "密钥管理", "/security/secrets", "perm_menu_security", "", 20},
		{"perm_menu_certs", "menu:certs", "证书管理", "/security/certs", "perm_menu_security", "", 30},

		// 系统设置
		{"perm_menu_settings", "menu:settings", "系统设置", "/settings", "", "settings", 11},
		{"perm_menu_datasources", "menu:datasources", "数据源配置", "/settings/datasources", "perm_menu_settings", "", 10},
		{"perm_menu_sysparams", "menu:sysparams", "系统参数", "/settings/sysparams", "perm_menu_settings", "", 20},

		// Jira中心（外部应用权限）
		{"perm_menu_jira", "menu:jira", "Jira中心", "/jira", "", "jira", 12},
		{"perm_menu_jira_dashboard", "menu:jira_dashboard", "Jira仪表盘", "/jira/dashboard", "perm_menu_jira", "", 10},
		{"perm_menu_jira_projects", "menu:jira_projects", "Jira项目列表", "/jira/projects", "perm_menu_jira", "", 20},
		{"perm_menu_jira_issues", "menu:jira_issues", "Jira工单列表", "/jira/issues", "perm_menu_jira", "", 30},
		{"perm_menu_jira_stats", "menu:jira_stats", "Jira统计分析", "/jira/stats", "perm_menu_jira", "", 40},
		{"perm_menu_jira_report", "menu:jira_report", "Jira项目报告", "/jira/report", "perm_menu_jira", "", 50},
		{"perm_menu_jira_settings", "menu:jira_settings", "Jira系统设置", "/jira/settings", "perm_menu_jira", "", 60},

		// Confluence中心（外部应用权限）
		{"perm_menu_confluence", "menu:confluence", "Confluence中心", "/confluence", "", "confluence", 13},
		{"perm_menu_confluence_dashboard", "menu:confluence_dashboard", "Confluence仪表盘", "/confluence/dashboard", "perm_menu_confluence", "", 10},
		{"perm_menu_confluence_spaces", "menu:confluence_spaces", "Confluence空间列表", "/confluence/spaces", "perm_menu_confluence", "", 20},
		{"perm_menu_confluence_search", "menu:confluence_search", "Confluence搜索", "/confluence/search", "perm_menu_confluence", "", 30},
		{"perm_menu_confluence_jira", "menu:confluence_jira", "Confluence Jira工单", "/confluence/jira", "perm_menu_confluence", "", 40},
		{"perm_menu_confluence_report", "menu:confluence_report", "Confluence生成报告", "/confluence/report", "perm_menu_confluence", "", 50},
		{"perm_menu_confluence_settings", "menu:confluence_settings", "Confluence系统设置", "/confluence/settings", "perm_menu_confluence", "", 60},
		{"perm_menu_confluence_alert_stats", "menu:confluence_alert-stats", "Confluence告警统计", "/confluence/alert-stats", "perm_menu_confluence", "", 55},

		// 告警平台（外部应用权限）
		{"perm_menu_alert", "menu:alert", "告警平台", "/alert", "", "alert", 14},
		{"perm_menu_alert_dashboard", "menu:alert_dashboard", "告警仪表盘", "/alert/dashboard", "perm_menu_alert", "", 10},
		{"perm_menu_alert_rules", "menu:alert_rules", "告警规则", "/alert/rules", "perm_menu_alert", "", 20},
		{"perm_menu_alert_explore", "menu:alert_explore", "日志查询", "/alert/explore", "perm_menu_alert", "", 30},
		{"perm_menu_alert_connections", "menu:alert_connections", "连接管理", "/alert/connections", "perm_menu_alert", "", 40},
		{"perm_menu_alert_lark", "menu:alert_lark", "Lark配置", "/alert/lark", "perm_menu_alert", "", 50},
		{"perm_menu_alert_logs", "menu:alert_logs", "告警日志", "/alert/logs", "perm_menu_alert", "", 60},
		{"perm_menu_alert_contacts", "menu:alert_contacts", "通知人管理", "/alert/contacts", "perm_menu_alert", "", 70},
		{"perm_menu_alert_mutes", "menu:alert_mutes", "屏蔽管理", "/alert/mutes", "perm_menu_alert", "", 75},
		{"perm_menu_alert_users", "menu:alert_users", "告警账号管理", "/alert/users", "perm_menu_alert", "", 80},

		// 发布中心（外部应用权限）
		{"perm_menu_deploy_center", "menu:deploy_center", "发布中心", "/deploy-center", "", "deploy_center", 15},
		{"perm_menu_deploy_center_dashboard", "menu:deploy_center_dashboard", "发布概览", "/deploy-center/dashboard", "perm_menu_deploy_center", "", 5},
		{"perm_menu_deploy_center_console", "menu:deploy_center_console", "部署控制台", "/deploy-center/console", "perm_menu_deploy_center", "", 10},
		{"perm_menu_deploy_center_projects", "menu:deploy_center_projects", "项目配置", "/deploy-center/projects", "perm_menu_deploy_center", "", 20},
		{"perm_menu_deploy_center_history", "menu:deploy_center_history", "发布历史", "/deploy-center/history", "perm_menu_deploy_center", "", 30},
		{"perm_menu_deploy_center_settings", "menu:deploy_center_settings", "系统设置", "/deploy-center/settings", "perm_menu_deploy_center", "", 40},

		// 探测平台（外部应用权限）
		{"perm_menu_probe", "menu:probe", "探测平台", "/probe", "", "probe", 16},
		{"perm_menu_probe_dashboard", "menu:probe_dashboard", "探测概览", "/probe/dashboard", "perm_menu_probe", "", 10},
		{"perm_menu_probe_agents", "menu:probe_agents", "Agent 管理", "/probe/agents", "perm_menu_probe", "", 20},
		{"perm_menu_probe_groups", "menu:probe_groups", "Agent 分组", "/probe/agent-groups", "perm_menu_probe", "", 30},
		{"perm_menu_probe_targets", "menu:probe_targets", "探测目标", "/probe/targets", "perm_menu_probe", "", 40},
		{"perm_menu_probe_manual", "menu:probe_manual", "手动探测", "/probe/probe", "perm_menu_probe", "", 50},
		{"perm_menu_probe_results", "menu:probe_results", "探测结果", "/probe/results", "perm_menu_probe", "", 60},
		{"perm_menu_probe_versions", "menu:probe_versions", "版本管理", "/probe/versions", "perm_menu_probe", "", 70},
		{"perm_menu_probe_upgrades", "menu:probe_upgrades", "升级任务", "/probe/upgrades", "perm_menu_probe", "", 80},
		{"perm_menu_probe_audit", "menu:probe_audit", "审计日志", "/probe/audit", "perm_menu_probe", "", 90},
		{"perm_menu_probe_users", "menu:probe_users", "用户管理", "/probe/users", "perm_menu_probe", "", 100},
	}

	for _, perm := range menuPermissions {
		DB.Exec(`INSERT IGNORE INTO permissions (id, code, name, type, resource, parent_id, icon, sort_order) VALUES (?, ?, ?, 'menu', ?, ?, ?, ?)`,
			perm.ID, perm.Code, perm.Name, perm.Resource, perm.ParentID, perm.Icon, perm.Sort)
	}

	// 默认外部应用（INSERT IGNORE，已存在则不覆盖，保留管理员在 UI 里的修改）
	// URL 是默认本地值，生产环境请在 UI 里改成实际域名
	defaultExternalApps := []struct {
		ID        string
		AppKey    string
		Name      string
		URL       string
		PermCode  string
		GroupName string
		SortOrder int
	}{
		{"app_probe_platform", "probe", "探测平台", "http://localhost:30827", "probe", "运维工具", 16},
		{"app_deploy_center", "deploy_center", "发布中心", "http://localhost:30826", "deploy_center", "运维工具", 15},
	}
	for _, a := range defaultExternalApps {
		DB.Exec(`INSERT IGNORE INTO external_apps (id, app_key, name, url, perm_code, group_name, sort_order, status)
			VALUES (?, ?, ?, ?, ?, ?, ?, 'active')`,
			a.ID, a.AppKey, a.Name, a.URL, a.PermCode, a.GroupName, a.SortOrder)
	}

	// 默认权限 - 按钮/操作权限
	buttonPermissions := []struct {
		ID          string
		Code        string
		Name        string
		Description string
	}{
		// 用户管理
		{"perm_btn_user_create", "user:create", "[用户管理] 添加用户", "允许创建新用户"},
		{"perm_btn_user_update", "user:update", "[用户管理] 编辑用户", "允许编辑用户信息"},
		{"perm_btn_user_delete", "user:delete", "[用户管理] 删除用户", "允许删除用户"},
		{"perm_btn_user_reset_pwd", "user:reset_password", "[用户管理] 重置密码", "允许重置用户密码"},

		// 角色管理
		{"perm_btn_role_create", "role:create", "[角色管理] 创建角色", "允许创建新角色"},
		{"perm_btn_role_update", "role:update", "[角色管理] 编辑角色", "允许编辑角色信息"},
		{"perm_btn_role_delete", "role:delete", "[角色管理] 删除角色", "允许删除角色"},
		{"perm_btn_role_assign", "role:assign", "[角色管理] 分配权限", "允许为角色分配权限"},

		// 资产管理
		{"perm_btn_asset_create", "asset:create", "[资产管理] 添加资产", "允许添加新资产"},
		{"perm_btn_asset_update", "asset:update", "[资产管理] 编辑资产", "允许编辑资产信息"},
		{"perm_btn_asset_delete", "asset:delete", "[资产管理] 删除资产", "允许删除资产"},
		{"perm_btn_asset_import", "asset:import", "[资产管理] 导入资产", "允许批量导入资产"},
		{"perm_btn_asset_export", "asset:export", "[资产管理] 导出资产", "允许导出资产列表"},

		// 域名管理
		{"perm_btn_domain_create", "domain:create", "[域名管理] 添加域名", "允许添加新域名"},
		{"perm_btn_domain_update", "domain:update", "[域名管理] 编辑域名", "允许编辑域名信息"},
		{"perm_btn_domain_delete", "domain:delete", "[域名管理] 删除域名", "允许删除域名"},
		{"perm_btn_domain_export", "domain:export", "[域名管理] 导出域名", "允许导出域名列表"},
		{"perm_btn_domain_batch_add", "domain:batch_add", "[域名管理] 批量添加", "允许批量添加域名"},
		{"perm_btn_domain_refresh", "domain:refresh", "[域名管理] 刷新到期时间", "允许刷新域名到期时间"},

		// 密码库
		{"perm_btn_vault_create", "vault:create", "[密码库] 添加密码", "允许添加密码条目"},
		{"perm_btn_vault_update", "vault:update", "[密码库] 编辑密码", "允许编辑密码条目"},
		{"perm_btn_vault_delete", "vault:delete", "[密码库] 删除密码", "允许删除密码条目"},
		{"perm_btn_vault_share", "vault:share", "[密码库] 分享密码", "允许分享密码给其他用户"},

		// 排班管理
		{"perm_btn_schedule_add_employee", "schedule:add_employee", "[排班管理] 添加员工", "允许添加排班员工"},
		{"perm_btn_schedule_edit_employee", "schedule:edit_employee", "[排班管理] 编辑员工", "允许编辑排班员工信息"},
		{"perm_btn_schedule_delete_employee", "schedule:delete_employee", "[排班管理] 删除员工", "允许删除排班员工"},
		{"perm_btn_schedule_batch", "schedule:batch", "[排班管理] 批量排班", "允许批量设置排班"},
		{"perm_btn_schedule_config", "schedule:config", "[排班管理] 班次配置", "允许配置班次类型"},
		{"perm_btn_schedule_export", "schedule:export", "[排班管理] 导出Excel", "允许导出排班表"},
		{"perm_btn_schedule_reset", "schedule:reset", "[排班管理] 重置排班", "允许重置指定月份的排班数据"},
		{"perm_btn_schedule_edit_shift", "schedule:edit_shift", "[排班管理] 编辑班次", "允许编辑单个班次"},

		// 商户管理
		{"perm_btn_merchant_create", "merchant:create", "[商户管理] 添加商户", "允许添加新商户"},
		{"perm_btn_merchant_update", "merchant:update", "[商户管理] 编辑商户", "允许编辑商户信息"},
		{"perm_btn_merchant_delete", "merchant:delete", "[商户管理] 删除商户", "允许删除商户"},
		{"perm_btn_merchant_export", "merchant:export", "[商户管理] 导出商户", "允许导出商户列表"},

		// 网络管理
		{"perm_btn_network_create", "network:create", "[网络管理] 添加记录", "允许添加网络记录"},
		{"perm_btn_network_update", "network:update", "[网络管理] 编辑记录", "允许编辑网络记录"},
		{"perm_btn_network_delete", "network:delete", "[网络管理] 删除记录", "允许删除网络记录"},
		{"perm_btn_network_batch", "network:batch", "[网络管理] 批量导入", "允许批量导入网络记录"},

		// 值班记录
		{"perm_btn_duty_create", "duty:create", "[值班记录] 添加记录", "允许添加值班记录"},
		{"perm_btn_duty_update", "duty:update", "[值班记录] 编辑记录", "允许编辑值班记录"},
		{"perm_btn_duty_edit_planned_fix_time", "duty:edit_planned_fix_time", "[值班记录] 编辑计划修复时间", "允许单独编辑计划修复时间"},
		{"perm_btn_duty_delete", "duty:delete", "[值班记录] 删除记录", "允许删除值班记录"},
		{"perm_btn_duty_export", "duty:export", "[值班记录] 导出记录", "允许导出值班记录"},
		{"perm_btn_duty_upload", "duty:upload", "[值班记录] 上传附件", "允许上传附件"},

		// 值班项目配置
		{"perm_btn_duty_project_create", "duty_project:create", "[值班项目] 添加项目", "允许添加值班项目"},
		{"perm_btn_duty_project_update", "duty_project:update", "[值班项目] 编辑项目", "允许编辑值班项目"},
		{"perm_btn_duty_project_delete", "duty_project:delete", "[值班项目] 删除项目", "允许删除值班项目"},

		// 桌台维护记录
		{"perm_btn_table_maint_create", "table_maintenance:create", "[桌台维护] 添加记录", "允许添加桌台维护记录"},
		{"perm_btn_table_maint_update", "table_maintenance:update", "[桌台维护] 编辑记录", "允许编辑桌台维护记录"},
		{"perm_btn_table_maint_delete", "table_maintenance:delete", "[桌台维护] 删除记录", "允许删除桌台维护记录"},
		{"perm_btn_table_maint_export", "table_maintenance:export", "[桌台维护] 导出记录", "允许导出桌台维护记录"},
		{"perm_btn_table_maint_upload", "table_maintenance:upload", "[桌台维护] 上传附件", "允许上传附件"},
		{"perm_btn_table_maint_read", "table_maintenance:read", "[桌台维护] 查看记录", "允许查看桌台维护记录"},

		// API Key 管理
		{"perm_btn_api_key_create", "api_key:create", "[API Key] 创建", "允许创建 API Key"},
		{"perm_btn_api_key_update", "api_key:update", "[API Key] 编辑", "允许编辑 API Key（改名、权限、过期、启停）"},
		{"perm_btn_api_key_delete", "api_key:delete", "[API Key] 删除", "允许删除 API Key"},

		// 桌台层级配置
		{"perm_btn_table_hierarchy_create", "table_hierarchy:create", "[桌台配置] 添加配置", "允许添加桌台层级配置"},
		{"perm_btn_table_hierarchy_update", "table_hierarchy:update", "[桌台配置] 编辑配置", "允许编辑桌台层级配置"},
		{"perm_btn_table_hierarchy_delete", "table_hierarchy:delete", "[桌台配置] 删除配置", "允许删除桌台层级配置"},
		{"perm_btn_table_hierarchy_manage_project", "table_hierarchy:manage_project", "[桌台配置] 项目管理", "允许查看和管理项目配置"},
		{"perm_btn_table_hierarchy_manage_site", "table_hierarchy:manage_site", "[桌台配置] 现场管理", "允许查看和管理现场配置"},
		{"perm_btn_table_hierarchy_manage_gametype", "table_hierarchy:manage_gametype", "[桌台配置] 游戏类型管理", "允许查看和管理游戏类型配置"},
		{"perm_btn_table_hierarchy_manage_table", "table_hierarchy:manage_table", "[桌台配置] 桌台管理", "允许查看和管理桌台配置"},

		// 桌台管理（新菜单）
		{"perm_btn_table_mgmt_source_create", "table_management:source_create", "[桌台管理] 添加数据源", "允许添加项目外部数据源"},
		{"perm_btn_table_mgmt_source_update", "table_management:source_update", "[桌台管理] 编辑数据源", "允许编辑项目外部数据源"},
		{"perm_btn_table_mgmt_source_delete", "table_management:source_delete", "[桌台管理] 删除数据源", "允许删除项目外部数据源"},
		{"perm_btn_table_mgmt_sync", "table_management:sync", "[桌台管理] 手动同步", "允许手动触发同步外部桌台"},
		{"perm_btn_table_mgmt_alias_update", "table_management:alias_update", "[桌台管理] 编辑别名", "允许编辑游戏类型/现场中文别名"},

		// Jira中心
		{"perm_btn_jira_transition", "jira:transition", "[Jira中心] 工单状态流转", "允许在Jira中心执行工单状态流转"},
		{"perm_btn_jira_config_connection", "jira:config_connection", "[Jira中心] Jira连接配置", "允许配置Jira服务器连接"},
		{"perm_btn_jira_config_sso", "jira:config_sso", "[Jira中心] SSO配置", "允许配置Jira中心的SSO设置"},
		{"perm_btn_jira_manage_users", "jira:manage_users", "[Jira中心] 用户管理", "允许在Jira中心管理用户"},
		{"perm_btn_jira_view_audit", "jira:view_audit", "[Jira中心] 查看审计日志", "允许查看Jira中心审计日志"},

		// Confluence中心
		{"perm_btn_confluence_manage_connections", "confluence:manage_connections", "[Confluence中心] 连接管理", "允许管理Confluence和Jira连接配置"},
		{"perm_btn_confluence_export_report", "confluence:export_report", "[Confluence中心] 导出报告", "允许导出运维报告"},
		{"perm_btn_confluence_manage_settings", "confluence:manage_settings", "[Confluence中心] 系统配置", "允许修改Confluence中心系统设置"},

		// 告警平台
		{"perm_btn_alert_create_rule", "alert:create_rule", "[告警平台] 创建规则", "允许创建告警规则"},
		{"perm_btn_alert_edit_rule", "alert:edit_rule", "[告警平台] 编辑规则", "允许编辑告警规则"},
		{"perm_btn_alert_delete_rule", "alert:delete_rule", "[告警平台] 删除规则", "允许删除告警规则"},
		{"perm_btn_alert_toggle_rule", "alert:toggle_rule", "[告警平台] 启停规则", "允许启用/禁用告警规则"},
		{"perm_btn_alert_test_send", "alert:test_send", "[告警平台] 测试发送", "允许测试发送告警到Lark"},
		{"perm_btn_alert_mute", "alert:mute", "[告警平台] 屏蔽管理", "允许添加/取消告警屏蔽"},
		{"perm_btn_alert_manage_connections", "alert:manage_connections", "[告警平台] 连接管理", "允许管理ES/Loki连接"},
		{"perm_btn_alert_manage_contacts", "alert:manage_contacts", "[告警平台] 通知人管理", "允许管理通知人"},
		{"perm_btn_alert_manage_lark", "alert:manage_lark", "[告警平台] Lark配置", "允许管理Lark配置"},

		// 发布中心
		{"perm_btn_dc_submit_uat", "deploy_center:submit_uat", "[发布中心] 提交 UAT", "允许提交 UAT 环境的镜像发布"},
		{"perm_btn_dc_submit_prod", "deploy_center:submit_prod", "[发布中心] 提交 PROD", "允许提交 PROD 环境的镜像发布"},
		{"perm_btn_dc_restart", "deploy_center:restart", "[发布中心] 重启服务", "允许通过 ArgoCD 重启服务"},
		{"perm_btn_dc_rollback", "deploy_center:rollback", "[发布中心] 回滚发布", "允许回滚到历史版本"},
		{"perm_btn_dc_scan_modules", "deploy_center:scan_modules", "[发布中心] 扫描模块", "允许触发 Git 扫描重建模块列表"},
		{"perm_btn_dc_manage_projects", "deploy_center:manage_projects", "[发布中心] 项目环境管理", "允许增删改项目和环境"},
		{"perm_btn_dc_manage_argocd", "deploy_center:manage_argocd", "[发布中心] ArgoCD 实例", "允许增删改 ArgoCD 实例"},
		{"perm_btn_dc_manage_lark_bots", "deploy_center:manage_lark_bots", "[发布中心] Lark 机器人", "允许增删改 Lark 机器人"},
		{"perm_btn_dc_manage_contacts", "deploy_center:manage_contacts", "[发布中心] 通知人管理", "允许增删改通知人"},
		{"perm_btn_dc_manage_global", "deploy_center:manage_global", "[发布中心] 全局配置", "允许修改全局凭证/轮询策略"},
	}

	for _, perm := range buttonPermissions {
		DB.Exec(`INSERT IGNORE INTO permissions (id, code, name, type, description) VALUES (?, ?, ?, 'button', ?)`,
			perm.ID, perm.Code, perm.Name, perm.Description)
	}

	// 为超级管理员分配所有权限
	rows, _ := DB.Query("SELECT id FROM permissions")
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var permID string
			rows.Scan(&permID)
			DB.Exec(`INSERT IGNORE INTO role_permissions (id, role_id, permission_id) VALUES (?, 'role_super_admin', ?)`,
				"rp_super_admin_"+permID, permID)
		}
	}

	// 为管理员分配系统管理相关权限
	adminPermCodes := []string{
		"menu:system", "menu:welcome", "menu:users", "menu:roles", "menu:audit", "menu:schedule",
		"user:create", "user:update", "user:reset_password",
		"role:create", "role:update", "role:assign",
	}
	for _, code := range adminPermCodes {
		var permID string
		DB.QueryRow("SELECT id FROM permissions WHERE code = ?", code).Scan(&permID)
		if permID != "" {
			DB.Exec(`INSERT IGNORE INTO role_permissions (id, role_id, permission_id) VALUES (?, 'role_admin', ?)`,
				"rp_admin_"+permID, permID)
		}
	}

	// 为运维人员分配资源管理权限
	operatorPermCodes := []string{
		"menu:resource", "menu:assets", "menu:domains", "menu:network", "menu:topology",
		"menu:monitor", "menu:security", "menu:vault",
		"asset:create", "asset:update", "asset:import", "asset:export",
		"domain:create", "domain:update",
		"vault:create", "vault:update", "vault:share",
		"menu:duty",
		"duty:create", "duty:update", "duty:delete", "duty:export", "duty:upload",
	}
	for _, code := range operatorPermCodes {
		var permID string
		DB.QueryRow("SELECT id FROM permissions WHERE code = ?", code).Scan(&permID)
		if permID != "" {
			DB.Exec(`INSERT IGNORE INTO role_permissions (id, role_id, permission_id) VALUES (?, 'role_operator', ?)`,
				"rp_operator_"+permID, permID)
		}
	}

	// 为业务运维类角色分配值班记录权限
	dutyPermCodes := []string{
		"menu:duty",
		"duty:create", "duty:update", "duty:delete", "duty:export", "duty:upload",
	}
	ywRows, _ := DB.Query("SELECT id FROM roles WHERE code LIKE '%yw%' OR name LIKE '%运维%' OR code LIKE '%duty%'")
	if ywRows != nil {
		var ywRoleIDs []string
		for ywRows.Next() {
			var rid string
			ywRows.Scan(&rid)
			ywRoleIDs = append(ywRoleIDs, rid)
		}
		ywRows.Close()
		for _, roleID := range ywRoleIDs {
			for _, code := range dutyPermCodes {
				var permID string
				DB.QueryRow("SELECT id FROM permissions WHERE code = ?", code).Scan(&permID)
				if permID != "" {
					rpID := fmt.Sprintf("rp_%s_%s", roleID, permID)
					DB.Exec(`INSERT IGNORE INTO role_permissions (id, role_id, permission_id) VALUES (?, ?, ?)`,
						rpID, roleID, permID)
				}
			}
		}
		log.Printf("已为 %d 个业务运维类角色分配值班记录权限", len(ywRoleIDs))
	}

	// 为只读用户分配查看权限（所有菜单权限）
	viewerRows, _ := DB.Query("SELECT id FROM permissions WHERE type = 'menu'")
	if viewerRows != nil {
		defer viewerRows.Close()
		for viewerRows.Next() {
			var permID string
			viewerRows.Scan(&permID)
			DB.Exec(`INSERT IGNORE INTO role_permissions (id, role_id, permission_id) VALUES (?, 'role_viewer', ?)`,
				"rp_viewer_"+permID, permID)
		}
	}
}

// Close 关闭数据库连接
func Close() {
	if DB != nil {
		DB.Close()
	}
}

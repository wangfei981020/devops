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

	// 兼容旧数据库：添加 MFA 字段
	DB.Exec(`ALTER TABLE users ADD COLUMN mfa_enabled TINYINT(1) DEFAULT 0`)
	DB.Exec(`ALTER TABLE users ADD COLUMN mfa_secret VARCHAR(64)`)
	// 兼容旧数据库：添加 phone, email, description 字段
	DB.Exec(`ALTER TABLE users ADD COLUMN phone VARCHAR(32) DEFAULT ''`)
	DB.Exec(`ALTER TABLE users ADD COLUMN email VARCHAR(128) DEFAULT ''`)
	DB.Exec(`ALTER TABLE users ADD COLUMN description TEXT`)

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

		// 资源管理
		{"perm_menu_resource", "menu:resource", "资源管理", "/resource", "", "resource", 2},
		{"perm_menu_assets", "menu:assets", "资产管理", "/resource/assets", "perm_menu_resource", "", 10},
		{"perm_menu_domains", "menu:domains", "域名管理", "/resource/domains", "perm_menu_resource", "", 20},
		{"perm_menu_network", "menu:network", "网络管理", "/resource/network", "perm_menu_resource", "", 30},
		{"perm_menu_topology", "menu:topology", "服务拓扑", "/resource/topology", "perm_menu_resource", "", 40},

		// 监控告警
		{"perm_menu_monitor", "menu:monitor", "监控告警", "/monitor", "", "monitor", 3},

		// 安全管理
		{"perm_menu_security", "menu:security", "安全管理", "/security", "", "security", 4},
		{"perm_menu_vault", "menu:vault", "密码库", "/security/vault", "perm_menu_security", "", 10},
		{"perm_menu_secrets", "menu:secrets", "密钥管理", "/security/secrets", "perm_menu_security", "", 20},
		{"perm_menu_certs", "menu:certs", "证书管理", "/security/certs", "perm_menu_security", "", 30},
	}

	for _, perm := range menuPermissions {
		DB.Exec(`INSERT IGNORE INTO permissions (id, code, name, type, resource, parent_id, icon, sort_order) VALUES (?, ?, ?, 'menu', ?, ?, ?, ?)`,
			perm.ID, perm.Code, perm.Name, perm.Resource, perm.ParentID, perm.Icon, perm.Sort)
	}

	// 默认权限 - 按钮/操作权限
	buttonPermissions := []struct {
		ID          string
		Code        string
		Name        string
		Description string
	}{
		// 用户管理
		{"perm_btn_user_create", "user:create", "创建用户", "允许创建新用户"},
		{"perm_btn_user_update", "user:update", "编辑用户", "允许编辑用户信息"},
		{"perm_btn_user_delete", "user:delete", "删除用户", "允许删除用户"},
		{"perm_btn_user_reset_pwd", "user:reset_password", "重置密码", "允许重置用户密码"},

		// 角色管理
		{"perm_btn_role_create", "role:create", "创建角色", "允许创建新角色"},
		{"perm_btn_role_update", "role:update", "编辑角色", "允许编辑角色信息"},
		{"perm_btn_role_delete", "role:delete", "删除角色", "允许删除角色"},
		{"perm_btn_role_assign", "role:assign", "分配权限", "允许为角色分配权限"},

		// 资产管理
		{"perm_btn_asset_create", "asset:create", "添加资产", "允许添加新资产"},
		{"perm_btn_asset_update", "asset:update", "编辑资产", "允许编辑资产信息"},
		{"perm_btn_asset_delete", "asset:delete", "删除资产", "允许删除资产"},
		{"perm_btn_asset_import", "asset:import", "导入资产", "允许批量导入资产"},
		{"perm_btn_asset_export", "asset:export", "导出资产", "允许导出资产列表"},

		// 域名管理
		{"perm_btn_domain_create", "domain:create", "添加域名", "允许添加新域名"},
		{"perm_btn_domain_update", "domain:update", "编辑域名", "允许编辑域名信息"},
		{"perm_btn_domain_delete", "domain:delete", "删除域名", "允许删除域名"},

		// 密码库
		{"perm_btn_vault_create", "vault:create", "添加密码", "允许添加密码条目"},
		{"perm_btn_vault_update", "vault:update", "编辑密码", "允许编辑密码条目"},
		{"perm_btn_vault_delete", "vault:delete", "删除密码", "允许删除密码条目"},
		{"perm_btn_vault_share", "vault:share", "分享密码", "允许分享密码给其他用户"},
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
	}
	for _, code := range operatorPermCodes {
		var permID string
		DB.QueryRow("SELECT id FROM permissions WHERE code = ?", code).Scan(&permID)
		if permID != "" {
			DB.Exec(`INSERT IGNORE INTO role_permissions (id, role_id, permission_id) VALUES (?, 'role_operator', ?)`,
				"rp_operator_"+permID, permID)
		}
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

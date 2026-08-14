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

	// v749: 修复历史数据
	//  step 1) data 列里 string-wrapped JSON 规整成 object（前端 JSON.stringify 多包一层）
	//  step 2) start_time/end_time 把 T 分隔符替换成空格（早期 datetime-local 没 normalize 的遗留数据）
	//  step 3) 按 start_time 反推 date 修 UTC 时差 bug
	// 注意 step 3 用 REGEXP 严格过滤，避免 MySQL 8.4 严格模式下 STR_TO_DATE 抛 Error 1411 整批中止。
	if res, err := DB.Exec(`
		UPDATE custom_rows
		SET data = CAST(JSON_UNQUOTE(data) AS JSON)
		WHERE JSON_TYPE(data) = 'STRING'
	`); err != nil {
		log.Printf("v749: step1 string→object 失败: %v", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("v749: step1 custom_rows.data string→object 规整 %d 行", n)
	}

	// step 2a: 归一化 start_time 的 T 分隔符
	if res, err := DB.Exec(`
		UPDATE custom_rows
		SET data = JSON_SET(data, '$.start_time',
			REPLACE(JSON_UNQUOTE(JSON_EXTRACT(data, '$.start_time')), 'T', ' '))
		WHERE JSON_TYPE(data) = 'OBJECT'
		  AND JSON_UNQUOTE(JSON_EXTRACT(data, '$.start_time')) LIKE '%T%'
	`); err != nil {
		log.Printf("v749: step2a 归一化 start_time T→空格 失败: %v", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("v749: step2a 归一化 start_time T→空格 %d 行", n)
	}

	// step 2b: 归一化 end_time 的 T 分隔符
	if res, err := DB.Exec(`
		UPDATE custom_rows
		SET data = JSON_SET(data, '$.end_time',
			REPLACE(JSON_UNQUOTE(JSON_EXTRACT(data, '$.end_time')), 'T', ' '))
		WHERE JSON_TYPE(data) = 'OBJECT'
		  AND JSON_EXTRACT(data, '$.end_time') IS NOT NULL
		  AND JSON_UNQUOTE(JSON_EXTRACT(data, '$.end_time')) LIKE '%T%'
	`); err != nil {
		log.Printf("v749: step2b 归一化 end_time T→空格 失败: %v", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("v749: step2b 归一化 end_time T→空格 %d 行", n)
	}

	// step 3: 按 start_time 反推 date（REGEXP 严格过滤防 STR_TO_DATE 抛错，兼容带秒/不带秒）
	if res, err := DB.Exec(`
		UPDATE custom_rows
		SET data = JSON_SET(data, '$.date',
			DATE_FORMAT(STR_TO_DATE(JSON_UNQUOTE(JSON_EXTRACT(data, '$.start_time')),
								   '%Y-%m-%d %H:%i'), '%Y-%m-%d'))
		WHERE JSON_TYPE(data) = 'OBJECT'
		  AND JSON_UNQUOTE(JSON_EXTRACT(data, '$.start_time'))
			  REGEXP '^[0-9]{4}-[0-9]{2}-[0-9]{2} [0-9]{2}:[0-9]{2}'
		  AND JSON_UNQUOTE(JSON_EXTRACT(data, '$.date')) <>
			  DATE_FORMAT(STR_TO_DATE(JSON_UNQUOTE(JSON_EXTRACT(data, '$.start_time')),
									 '%Y-%m-%d %H:%i'), '%Y-%m-%d')
	`); err != nil {
		log.Printf("v749: step3 修复 date 失败: %v", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("v749: step3 修复 custom_rows.date 时差 bug %d 行", n)
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
	// v763: 职位英文名（导出 Excel 用），存量按中文职位回填：组长 -> YW Leader，其余 -> YW Team
	DB.Exec(`ALTER TABLE schedule_employees ADD COLUMN role_en VARCHAR(64) DEFAULT '' COMMENT '职位英文名(导出Excel用)' AFTER role`)
	if res, err := DB.Exec(`
		UPDATE schedule_employees
		SET role_en = CASE WHEN role LIKE '%组长%' THEN 'YW Leader' ELSE 'YW Team' END
		WHERE role_en IS NULL OR role_en = ''
	`); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[排班] 回填员工职位英文名 role_en: %d 行", n)
		}
	} else {
		log.Printf("[排班] WARN 回填 role_en 失败: %v", err)
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
	// v763: 班次英文说明（导出 Excel 图例用）。有时间段的班次回填时间段本身，休假类回填英文词
	DB.Exec(`ALTER TABLE schedule_shift_configs ADD COLUMN time_en VARCHAR(64) DEFAULT '' COMMENT '英文说明(导出Excel图例用)' AFTER time_range`)
	shiftTimeEnDefaults := map[string]string{
		"OD":  "Weekend off",
		"OFF": "Scheduled off",
		"H":   "Holidays",
		"PL":  "Personal Leave",
		"SL":  "Sick Leave",
		"AL":  "Annual Leave",
		"CT":  "Change Shift",
	}
	for code, timeEn := range shiftTimeEnDefaults {
		if _, err := DB.Exec(`
			UPDATE schedule_shift_configs SET time_en = ? WHERE code = ? AND (time_en IS NULL OR time_en = '')
		`, timeEn, code); err != nil {
			log.Printf("[排班] WARN 回填班次 %s 的 time_en 失败: %v", code, err)
		}
	}
	// 其余班次（早/中/晚班、值班等）英文说明直接用时间段，值班类没时间段的兜底成 Duty
	if _, err := DB.Exec(`
		UPDATE schedule_shift_configs
		SET time_en = CASE WHEN time_range = '' OR time_range = '-' THEN 'Duty' ELSE REPLACE(time_range, '-', ' - ') END
		WHERE time_en IS NULL OR time_en = ''
	`); err != nil {
		log.Printf("[排班] WARN 回填班次 time_en 兜底失败: %v", err)
	}

	// v772: 「是否在岗」标记。跨时区覆盖空档检查要判断哪些班次算有人在岗，
	// 原有的 is_duty 语义是「值班」不是「在岗」，不能拿来当判据。
	// 回填规则：time_range 是有效时间段（含「全天」）-> 在岗；'-' / 空 -> 不在岗（OD/OFF/H/PL/SL/AL/CT）。
	DB.Exec(`ALTER TABLE schedule_shift_configs ADD COLUMN is_working BOOLEAN DEFAULT NULL COMMENT '是否算在岗(覆盖空档检查用)' AFTER is_duty`)
	if res, err := DB.Exec(`
		UPDATE schedule_shift_configs
		SET is_working = CASE
			WHEN time_range IS NULL OR time_range = '' OR time_range = '-' THEN 0
			ELSE 1 END
		WHERE is_working IS NULL
	`); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			// 回填结果打出来让人能核对，别让判据静默生效
			rows, qerr := DB.Query(`SELECT code, time_range, is_working FROM schedule_shift_configs ORDER BY sort_order, id`)
			if qerr == nil {
				var working, idle []string
				for rows.Next() {
					var code, tr string
					var isWorking bool
					if rows.Scan(&code, &tr, &isWorking) != nil {
						continue
					}
					if isWorking {
						working = append(working, fmt.Sprintf("%s(%s)", code, tr))
					} else {
						idle = append(idle, code)
					}
				}
				rows.Close()
				log.Printf("[排班] 回填班次在岗标记 is_working: %d 行 | 在岗=%v | 不在岗=%v", n, working, idle)
			}
		}
	} else {
		log.Printf("[排班] WARN 回填班次 is_working 失败: %v", err)
	}

	// v772: 员工时区历史表（跨时区排班）
	// ⚠️ 存 IANA 时区名（Europe/Belgrade），不存 UTC+2 这种固定偏移——冬夏令时切换后固定偏移会静默错 1 小时。
	// ⚠️ 时区带生效日期：某人从欧洲搬回北京只是新增一条记录，历史排班的换算结果不会被追溯改写。
	// 判定某天用哪个时区 = effective_from <= 该日 的最后一条。
	// 员工表刻意不存「当前时区」冗余列——两处各存一份必然分叉。
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS schedule_employee_timezones (
			id INT AUTO_INCREMENT PRIMARY KEY,
			employee_id INT NOT NULL,
			timezone VARCHAR(64) NOT NULL COMMENT 'IANA 时区名，如 Europe/Belgrade',
			effective_from DATE NOT NULL COMMENT '从这天起生效（员工本人当地日期）',
			created_by VARCHAR(64) DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_emp_from (employee_id, effective_from),
			INDEX idx_emp (employee_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}
	seedScheduleTimezones()

	// 创建排班月度应工作天数配置表（v733: 排班统计分析功能）
	// 每月一行，管理员手动填写本月应工作天数（用于统计页"达成 / 缺勤 / 超勤"判定）
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS schedule_month_target (
			year INT NOT NULL,
			month INT NOT NULL,
			expected_work_days INT NOT NULL,
			updated_by VARCHAR(64) DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			PRIMARY KEY (year, month)
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

	// ========== 响应记录预设原因表（v745：admin 自定义未响应/仅响应原因，供下拉选择）==========
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS response_reasons (
			id INT AUTO_INCREMENT PRIMARY KEY,
			label VARCHAR(64) NOT NULL UNIQUE COMMENT '原因显示文字',
			category VARCHAR(16) DEFAULT 'all' COMMENT 'no_reply | reply_only | all',
			sort_order INT DEFAULT 0,
			status VARCHAR(16) DEFAULT 'active' COMMENT 'active | disabled',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}
	// 默认原因。INSERT IGNORE 不覆盖 admin 修改
	DB.Exec(`INSERT IGNORE INTO response_reasons (label, category, sort_order) VALUES
		('在开会',         'no_reply',   1),
		('在医院',         'no_reply',   2),
		('出差中',         'no_reply',   3),
		('在路上',         'no_reply',   4),
		('在加班别的事',   'no_reply',   5),
		('我没权限',       'reply_only', 10),
		('转交他人',       'reply_only', 11),
		('技术不熟',       'reply_only', 12),
		('已通知开发',     'reply_only', 13),
		('其他',           'all',        99)`)

	// ========== 响应记录来源配置表（v739：admin 自定义消息来源）==========
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS response_record_sources (
			id INT AUTO_INCREMENT PRIMARY KEY,
			code VARCHAR(32) NOT NULL UNIQUE COMMENT '业务 code，如 lark/alert/...',
			label VARCHAR(64) NOT NULL COMMENT '显示名',
			color VARCHAR(16) DEFAULT '#94a3b8' COMMENT '徽章颜色',
			sort_order INT DEFAULT 0,
			status VARCHAR(16) DEFAULT 'active' COMMENT 'active | disabled',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}
	// 默认 6 个来源；INSERT IGNORE 不会覆盖 admin 的修改
	DB.Exec(`INSERT IGNORE INTO response_record_sources (code, label, color, sort_order) VALUES
		('lark',   'Lark', '#3a84ff', 1),
		('alert',  '告警', '#ea3636', 2),
		('phone',  '电话', '#10b981', 3),
		('email',  '邮件', '#8b5cf6', 4),
		('ticket', '工单', '#ff9c01', 5),
		('other',  '其它', '#94a3b8', 6)`)

	// ========== 响应记录表（v738：员工消息响应度量；v740：支持多响应人）==========
	// 每条记录 = 一次任务，可由多个员工响应（responders JSON 数组）
	// 时间轴：mentioned_at(T0 艾特) → 每个响应人各自的 responded_at(T1) / completed_at(T2)
	// 老字段 responder/responded_at/completed_at 保留为"首响应人快速查询"，跟 responders[0] 同步
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS response_records (
			id INT AUTO_INCREMENT PRIMARY KEY,
			responder VARCHAR(64) NOT NULL COMMENT '首响应人 = responders[0].responder（兼容字段）',
			responders TEXT COMMENT 'v740: 多响应人 JSON [{responder, responded_at, completed_at}]',
			message_source VARCHAR(32) NOT NULL DEFAULT 'lark' COMMENT '消息来源 lark/alert/phone/email/ticket/other',
			message_content TEXT NOT NULL COMMENT '消息内容',
			mentioned_at DATETIME NOT NULL COMMENT 'T0 艾特/消息发出时间',
			responded_at DATETIME NOT NULL COMMENT '首响应时间 = responders[0].responded_at（兼容字段）',
			completed_at DATETIME DEFAULT NULL COMMENT '末完成时间 = max(responders[*].completed_at)（兼容字段）',
			has_incident TINYINT(1) DEFAULT 0 COMMENT '是否产生故障',
			incident_ticket VARCHAR(64) DEFAULT '' COMMENT '故障单号（仅 has_incident=1 时有值）',
			handle_result TEXT COMMENT '处理结果',
			remark TEXT COMMENT '备注',
			attachments TEXT COMMENT '附件 JSON [{name,size,path}]',
			status VARCHAR(16) DEFAULT 'processing' COMMENT 'processing | completed (所有响应人都填了完成时间)',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_by VARCHAR(64) DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			updated_by VARCHAR(64) DEFAULT '',
			INDEX idx_rr_responder (responder),
			INDEX idx_rr_mentioned (mentioned_at),
			INDEX idx_rr_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
	`)
	if err != nil {
		return err
	}
	// v740: 旧表升级（IF NOT EXISTS 不支持 ADD COLUMN，用 IGNORE 错误兜底）
	DB.Exec(`ALTER TABLE response_records ADD COLUMN responders TEXT COMMENT 'v740 多响应人 JSON' AFTER responder`)
	// v740: 把 responders 为 NULL 的老数据用 responder/responded_at/completed_at 包装成单元素数组
	DB.Exec(`UPDATE response_records
		SET responders = JSON_ARRAY(JSON_OBJECT(
			'responder', responder,
			'responded_at', DATE_FORMAT(responded_at, '%Y-%m-%d %H:%i:%s'),
			'completed_at', IFNULL(DATE_FORMAT(completed_at, '%Y-%m-%d %H:%i:%s'), '')
		))
		WHERE responders IS NULL OR responders = ''`)
	// v743: 给 responders 数组里没有 mentioned_at 字段的老数据补全（拷贝主表 mentioned_at）
	// MySQL JSON_SET 对每个数组元素加字段（如果已存在则不变）
	DB.Exec(`UPDATE response_records
		SET responders = JSON_SET(responders, '$[0].mentioned_at',
			IFNULL(JSON_UNQUOTE(JSON_EXTRACT(responders, '$[0].mentioned_at')), DATE_FORMAT(mentioned_at, '%Y-%m-%d %H:%i:%s')))
		WHERE JSON_EXTRACT(responders, '$[0].mentioned_at') IS NULL`)

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
		{"perm_menu_schedule_analytics", "menu:schedule_analytics", "排班统计分析", "/system/schedule-analytics", "perm_menu_system", "", 75},
		{"perm_menu_taskpool", "menu:taskpool", "任务池", "/system/taskpool", "perm_menu_system", "", 80},
		{"perm_menu_incidents", "menu:incidents", "响应记录", "/system/incidents", "perm_menu_system", "", 90},
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
		{"perm_menu_deploy_center_orchestration", "menu:deploy_center_orchestration", "服务编排", "/deploy-center/orchestration", "perm_menu_deploy_center", "", 12},
		{"perm_menu_deploy_center_projects", "menu:deploy_center_projects", "项目配置", "/deploy-center/projects", "perm_menu_deploy_center", "", 20},
		{"perm_menu_deploy_center_history", "menu:deploy_center_history", "发布历史", "/deploy-center/history", "perm_menu_deploy_center", "", 30},
		{"perm_menu_deploy_center_templates", "menu:deploy_center_templates", "模板库", "/deploy-center/templates", "perm_menu_deploy_center", "", 32},
		{"perm_menu_deploy_center_environments", "menu:deploy_center_environments", "环境管理", "/deploy-center/environments", "perm_menu_deploy_center", "", 34},
		{"perm_menu_deploy_center_envparams", "menu:deploy_center_envparams", "项目参数", "/deploy-center/envparams", "perm_menu_deploy_center", "", 36},
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

		// CMDB 配置管理库（外部应用权限）
		//
		//	⚠️ 只能做两级：角色配置页 RolesView.vue 的菜单权限树只渲染 parent + children，
		//	第三级不递归。CMDB 侧边栏是三层（分组→页），所以这里压平成两级，
		//	分组信息放进名称前缀（[K8s] 节点），靠命名分组不靠层级。
		//	新增菜单页时记得同步加一条，否则该页在 CMDB 里会因 fail-closed 打不开。
		{"perm_menu_cmdb", "menu:cmdb", "CMDB", "/cmdb", "", "cmdb", 17},
		{"perm_menu_cmdb_overview", "menu:cmdb_overview", "[总览] 总览", "/cmdb/overview", "perm_menu_cmdb", "", 10},
		{"perm_menu_cmdb_hosts", "menu:cmdb_hosts", "[云资源] 主机", "/cmdb/hosts", "perm_menu_cmdb", "", 20},
		{"perm_menu_cmdb_cloud_ips", "menu:cmdb_cloud_ips", "[云资源] IP 地址", "/cmdb/cloud-ips", "perm_menu_cmdb", "", 22},
		{"perm_menu_cmdb_cloud_networks", "menu:cmdb_cloud_networks", "[云资源] VPC 网络", "/cmdb/cloud-networks", "perm_menu_cmdb", "", 24},
		{"perm_menu_cmdb_cloud_firewalls", "menu:cmdb_cloud_firewalls", "[云资源] 防火墙", "/cmdb/cloud-firewalls", "perm_menu_cmdb", "", 26},
		{"perm_menu_cmdb_cloud_lbs", "menu:cmdb_cloud_lbs", "[云资源] 负载均衡", "/cmdb/cloud-lbs", "perm_menu_cmdb", "", 28},
		{"perm_menu_cmdb_cloud_audit", "menu:cmdb_cloud_audit", "[云资源] 云平台审计", "/cmdb/cloud-audit", "perm_menu_cmdb", "", 30},
		{"perm_menu_cmdb_domains", "menu:cmdb_domains", "[资产] 域名", "/cmdb/domains", "perm_menu_cmdb", "", 40},
		{"perm_menu_cmdb_dns_records", "menu:cmdb_dns_records", "[资产] DNS 记录", "/cmdb/dns-records", "perm_menu_cmdb", "", 42},
		{"perm_menu_cmdb_cdn_sites", "menu:cmdb_cdn_sites", "[资产] CDN 站点", "/cmdb/cdn-sites", "perm_menu_cmdb", "", 44},
		{"perm_menu_cmdb_certs", "menu:cmdb_certs", "[资产] 证书", "/cmdb/certs", "perm_menu_cmdb", "", 46},
		{"perm_menu_cmdb_cert_inspect", "menu:cmdb_cert_inspect", "[资产] 到期巡检", "/cmdb/cert-inspect", "perm_menu_cmdb", "", 48},
		{"perm_menu_cmdb_relations", "menu:cmdb_relations", "[资产] 关系图谱", "/cmdb/relations", "perm_menu_cmdb", "", 49},
		{"perm_menu_cmdb_k8s_clusters", "menu:cmdb_k8s_clusters", "[K8s] 集群管理", "/cmdb/k8s-clusters", "perm_menu_cmdb", "", 60},
		{"perm_menu_cmdb_version_upgrade", "menu:cmdb_version_upgrade", "[K8s] 版本与升级", "/cmdb/version-upgrade", "perm_menu_cmdb", "", 62},
		{"perm_menu_cmdb_k8s_nodes", "menu:cmdb_k8s_nodes", "[K8s] 节点", "/cmdb/k8s-nodes", "perm_menu_cmdb", "", 64},
		{"perm_menu_cmdb_k8s_workloads", "menu:cmdb_k8s_workloads", "[K8s] 工作负载", "/cmdb/k8s-workloads", "perm_menu_cmdb", "", 66},
		{"perm_menu_cmdb_k8s_pods", "menu:cmdb_k8s_pods", "[K8s] Pod", "/cmdb/k8s-pods", "perm_menu_cmdb", "", 68},
		{"perm_menu_cmdb_k8s_networking", "menu:cmdb_k8s_networking", "[K8s] 网络", "/cmdb/k8s-networking", "perm_menu_cmdb", "", 70},
		{"perm_menu_cmdb_k8s_storage", "menu:cmdb_k8s_storage", "[K8s] 存储/伸缩", "/cmdb/k8s-storage", "perm_menu_cmdb", "", 72},
		{"perm_menu_cmdb_k8s_events", "menu:cmdb_k8s_events", "[K8s] 事件", "/cmdb/k8s-events", "perm_menu_cmdb", "", 74},
		{"perm_menu_cmdb_k8s_health", "menu:cmdb_k8s_health", "[K8s] 集群体检", "/cmdb/k8s-health", "perm_menu_cmdb", "", 76},
		{"perm_menu_cmdb_k8s_topology", "menu:cmdb_k8s_topology", "[K8s] 全链路", "/cmdb/k8s-topology", "perm_menu_cmdb", "", 78},
		{"perm_menu_cmdb_k8s_ns_project", "menu:cmdb_k8s_ns_project", "[K8s] 命名空间归属", "/cmdb/k8s-ns-project", "perm_menu_cmdb", "", 80},
		{"perm_menu_cmdb_event_center", "menu:cmdb_event_center", "[观测] 事件中心", "/cmdb/event-center", "perm_menu_cmdb", "", 90},
		{"perm_menu_cmdb_alerts", "menu:cmdb_alerts", "[观测] 告警", "/cmdb/alerts", "perm_menu_cmdb", "", 91},
		{"perm_menu_cmdb_k8s_usage", "menu:cmdb_k8s_usage", "[观测] 资源使用率", "/cmdb/k8s-usage", "perm_menu_cmdb", "", 92},
		{"perm_menu_cmdb_cost", "menu:cmdb_cost", "[观测] 云成本", "/cmdb/cost", "perm_menu_cmdb", "", 94},
		{"perm_menu_cmdb_basic", "menu:cmdb_basic", "[系统] 基础配置", "/cmdb/basic", "perm_menu_cmdb", "", 100},
		{"perm_menu_cmdb_integrations", "menu:cmdb_integrations", "[系统] 接入管理", "/cmdb/integrations", "perm_menu_cmdb", "", 102},
		{"perm_menu_cmdb_notify", "menu:cmdb_notify", "[系统] 通知", "/cmdb/notify", "perm_menu_cmdb", "", 104},
		{"perm_menu_cmdb_cron", "menu:cmdb_cron", "[系统] 定时任务", "/cmdb/cron", "perm_menu_cmdb", "", 106},
		{"perm_menu_cmdb_task_runs", "menu:cmdb_task_runs", "[系统] 执行记录", "/cmdb/task-runs", "perm_menu_cmdb", "", 108},
		{"perm_menu_cmdb_users", "menu:cmdb_users", "[系统] 用户管理", "/cmdb/users", "perm_menu_cmdb", "", 109},
		{"perm_menu_cmdb_audit", "menu:cmdb_audit", "[系统] 操作审计", "/cmdb/audit", "perm_menu_cmdb", "", 110},
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
		{"app_cmdb", "cmdb", "CMDB", "http://localhost:30829", "cmdb", "运维工具", 17},
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
		{"perm_btn_schedule_edit_timezone", "schedule:edit_timezone", "[排班管理] 设置员工时区", "允许设置员工所在时区及其生效日期（影响跨时区视图与覆盖空档判定）"},

		// 排班统计分析
		{"perm_btn_schedule_analytics_export", "schedule_analytics:export", "[排班统计] 导出Excel", "允许导出排班统计 Excel"},
		{"perm_btn_schedule_analytics_set_target", "schedule_analytics:set_target", "[排班统计] 修改应工作天数", "允许配置每月应工作天数（决定达成/缺勤判定）"},

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

		// 响应记录（v738）
		{"perm_btn_response_create", "response_record:create", "[响应记录] 添加记录", "允许添加响应记录"},
		{"perm_btn_response_update", "response_record:update", "[响应记录] 编辑记录", "允许编辑响应记录"},
		{"perm_btn_response_delete", "response_record:delete", "[响应记录] 删除记录", "允许删除响应记录"},
		{"perm_btn_response_export", "response_record:export", "[响应记录] 导出Excel", "允许导出响应记录"},
		{"perm_btn_response_source_manage", "response_source:manage", "[响应记录] 管理消息来源", "允许增删改自定义消息来源"},
		{"perm_btn_response_reason_manage", "response_reason:manage", "[响应记录] 管理预设原因", "允许增删改未响应/仅响应的预设原因"},

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
		{"perm_btn_dc_manage_templates", "deploy_center:manage_templates", "[发布中心] 模板库管理", "允许增删改服务编排模板"},
		{"perm_btn_dc_manage_projects", "deploy_center:manage_projects", "[发布中心] 项目环境管理", "允许增删改项目和环境"},
		{"perm_btn_dc_manage_argocd", "deploy_center:manage_argocd", "[发布中心] ArgoCD 实例", "允许增删改 ArgoCD 实例"},
		{"perm_btn_dc_manage_lark_bots", "deploy_center:manage_lark_bots", "[发布中心] Lark 机器人", "允许增删改 Lark 机器人"},
		{"perm_btn_dc_manage_contacts", "deploy_center:manage_contacts", "[发布中心] 通知人管理", "允许增删改通知人"},
		{"perm_btn_dc_manage_global", "deploy_center:manage_global", "[发布中心] 全局配置", "允许修改全局凭证/轮询策略"},
		{"perm_btn_dc_prod_auto_sync", "deploy_center:prod_auto_sync", "[发布中心] PROD Auto Sync", "允许开关 PROD 环境的 Auto Sync（开启后 PROD 提交后自动触发 ArgoCD sync，不再需要手动同步）"},

		// CMDB — 资产域
		//
		//	⚠️ manage_dns 和 issue_cert 单独成码、不并进 manage_domains：
		//	前者直接写云端 DNS（改错就是线上解析挂），后者真去 CA 签发（有速率限制，
		//	滥用会把域名打进 Let's Encrypt 的封禁窗口）。给"域名管理"不等于给这两个。
		{"perm_btn_cmdb_manage_domains", "cmdb:manage_domains", "[CMDB] 域名管理", "允许增删改域名台账、续费、自动续费开关、批量状态/忽略"},
		{"perm_btn_cmdb_sync_domains", "cmdb:sync_domains", "[CMDB] 域名同步刷新", "允许触发注册商同步与域名信息刷新"},
		{"perm_btn_cmdb_manage_dns", "cmdb:manage_dns", "[CMDB] DNS 记录写入", "允许增删改 DNS 记录（含批量）。⚠️ 会直接写入云端 DNS，改错会导致线上解析异常"},
		{"perm_btn_cmdb_manage_certs", "cmdb:manage_certs", "[CMDB] 证书管理", "允许录入/删除证书、复检、巡检忽略"},
		{"perm_btn_cmdb_issue_cert", "cmdb:issue_cert", "[CMDB] 证书签发/续签", "允许发起 ACME 签发与续签。⚠️ 会真实调用 CA，受签发速率限制"},
		{"perm_btn_cmdb_manage_cdn", "cmdb:manage_cdn", "[CMDB] CDN 配置管理", "允许增删改 CDN 站点与回源规则"},
		{"perm_btn_cmdb_sync_cdn", "cmdb:sync_cdn", "[CMDB] CDN 同步", "允许触发 CDN 账号同步与连通性验证"},
		{"perm_btn_cmdb_manage_records", "cmdb:manage_records", "[CMDB] 解析记录台账", "允许修改/删除解析记录台账，含批量操作"},

		// CMDB — 云资源域
		{"perm_btn_cmdb_manage_hosts", "cmdb:manage_hosts", "[CMDB] 主机/CI 管理", "允许增删改主机等配置项及其标签"},
		{"perm_btn_cmdb_sync_cloud", "cmdb:sync_cloud", "[CMDB] 云资源同步", "允许触发云账号与云项目的资源同步"},
		{"perm_btn_cmdb_manage_cloud_projects", "cmdb:manage_cloud_projects", "[CMDB] 云项目归属", "允许增删改云项目及其归属关系"},

		// CMDB — K8s 域
		{"perm_btn_cmdb_manage_clusters", "cmdb:manage_clusters", "[CMDB] 集群纳管", "允许增删改 K8s 集群接入。⚠️ 需填写 kubeconfig/ServiceAccount 凭据"},
		{"perm_btn_cmdb_sync_k8s", "cmdb:sync_k8s", "[CMDB] 集群手动同步", "允许手动触发集群资源全量同步"},
		{"perm_btn_cmdb_manage_ns_project", "cmdb:manage_ns_project", "[CMDB] 命名空间归属维护", "允许维护命名空间与项目的归属关系"},
		{"perm_btn_cmdb_manage_upgrade", "cmdb:manage_upgrade", "[CMDB] 升级基线/计划", "允许设置 GKE 升级基线快照与版本计划覆盖"},
		{"perm_btn_cmdb_k8s_diag", "cmdb:k8s_diag", "[CMDB] 实时诊断", "允许拉取实时日志/事件/Pod 详情。虽是只读，但绕过快照直连生产 apiserver，日志可能含业务数据"},

		// CMDB — 成本域
		{"perm_btn_cmdb_manage_cost_rates", "cmdb:manage_cost_rates", "[CMDB] 计价配置", "允许维护计算/磁盘费率、节点单价覆盖、重算成本快照"},

		// CMDB — 接入/凭据域（最高危）
		//
		//	manage_mcp 单独成码：MCP Token 是机器身份、不走 RBAC，
		//	拿到它等于拿到 CMDB 全量只读数据（跨集群、跨云账号）。
		{"perm_btn_cmdb_manage_integrations", "cmdb:manage_integrations", "[CMDB] 接入凭据管理", "允许增删改注册商/云账号/CDN/ACME/观测数据源/Harbor 的接入凭据。⚠️ 等于持有各系统的钥匙"},
		{"perm_btn_cmdb_manage_mcp", "cmdb:manage_mcp", "[CMDB] MCP Token 管理", "允许查看与重置 MCP Token。⚠️ Token 不受 RBAC 约束，持有即可读取全量数据"},

		// CMDB — 系统域
		//
		//	manage_cron 与 run_task 分开：值班同学常要手动补跑一次同步，
		//	但不该能改 cron 表达式把任务改成每分钟跑去打爆云厂商 API 配额。
		{"perm_btn_cmdb_manage_basic", "cmdb:manage_basic", "[CMDB] 基础配置", "允许维护环境/项目/生命周期状态/CI 类型与关系定义/系统设置"},
		{"perm_btn_cmdb_manage_notify", "cmdb:manage_notify", "[CMDB] 通知配置", "允许增删改通知人与 Lark 群，并发送测试消息"},
		{"perm_btn_cmdb_manage_cron", "cmdb:manage_cron", "[CMDB] 定时任务配置", "允许修改定时任务的启停、周期与参数"},
		{"perm_btn_cmdb_run_task", "cmdb:run_task", "[CMDB] 手动执行任务", "允许手动触发任务、取消执行、重试失败项"},

		// CMDB — 审计域
		//
		//	能改 ≠ 能把别人的改动撤掉，所以回滚独立成码。
		{"perm_btn_cmdb_manage_users", "cmdb:manage_users", "[CMDB] 用户管理", "允许改本地账号密码、踢用户下线、删除 SSO 影子账号。⚠️ 运维平台的 SSO 账号本体不在 CMDB 管理"},
		{"perm_btn_cmdb_revert_change", "cmdb:revert_change", "[CMDB] 变更回滚", "允许把一次变更回滚到变更前的内容。⚠️ 部分回滚会写入外部系统（如 Cloudflare DNS）"},
	}

	for _, perm := range buttonPermissions {
		DB.Exec(`INSERT IGNORE INTO permissions (id, code, name, type, description) VALUES (?, ?, ?, 'button', ?)`,
			perm.ID, perm.Code, perm.Name, perm.Description)
	}

	// 给 CMDB 的按钮权限排序。
	//
	//	角色配置页按 permissions.sort_order 排序，而上面的按钮种子统一不带 sort_order
	//	（全是默认 0），同一批插入的 created_at 又同秒，结果退化成 MySQL 的返回顺序——
	//	24 项乱序铺在弹窗里，勾"域名管理"得满屏找。
	//	用 UPDATE 而不是让种子带上：种子是 INSERT IGNORE，行已存在时不会更新 sort_order。
	for i, code := range cmdbButtonCodes {
		DB.Exec(`UPDATE permissions SET sort_order = ? WHERE code = ? AND type = 'button'`,
			(i+1)*10, code)
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

	initCMDBRoleTemplates()

	// ===== 桌台维护记录 · API Key 行级权限改造（2026-05-27） =====
	// 1) custom_rows 加 source_api_key_id 列
	if _, err := DB.Exec(`ALTER TABLE custom_rows ADD COLUMN source_api_key_id VARCHAR(36) NULL`); err == nil {
		log.Printf("[Migration] custom_rows: add column source_api_key_id")
	}
	// 2) 建索引
	if _, err := DB.Exec(`CREATE INDEX idx_custom_rows_source_api_key ON custom_rows(source_api_key_id)`); err == nil {
		log.Printf("[Migration] custom_rows: add index idx_custom_rows_source_api_key")
	}
	// 3) api_keys.name 唯一约束（重名场景下会失败，留日志由运维清理）
	if _, err := DB.Exec(`ALTER TABLE api_keys ADD UNIQUE KEY uk_api_keys_name (name)`); err == nil {
		log.Printf("[Migration] api_keys: add unique key uk_api_keys_name")
	}
	// 4) 插入两条新权限码（name 必须遵循 "[页面名] 动作" 格式，前端 RolesView 按此正则分组）
	DB.Exec(`INSERT IGNORE INTO permissions (id, code, name, type, sort_order, description) VALUES (?, ?, ?, ?, ?, ?)`,
		"perm_tm_edit_api_record", "table_maintenance:edit_api_record", "[桌台维护] 编辑 API Key 创建的记录", "button", 100,
		"允许编辑由 API Key 创建的桌台维护记录")
	DB.Exec(`INSERT IGNORE INTO permissions (id, code, name, type, sort_order, description) VALUES (?, ?, ?, ?, ?, ?)`,
		"perm_tm_delete_api_record", "table_maintenance:delete_api_record", "[桌台维护] 删除 API Key 创建的记录", "button", 101,
		"允许删除由 API Key 创建的桌台维护记录")
	// 4.1) 若早期版本插入时漏了 [桌台维护] 前缀，回填修正
	DB.Exec(`UPDATE permissions SET name = '[桌台维护] 编辑 API Key 创建的记录' WHERE code = 'table_maintenance:edit_api_record' AND name NOT LIKE '[%'`)
	DB.Exec(`UPDATE permissions SET name = '[桌台维护] 删除 API Key 创建的记录' WHERE code = 'table_maintenance:delete_api_record' AND name NOT LIKE '[%'`)
	// 5) backfill：按 keyName 反查 api_keys.id，填进 source_api_key_id
	if res, err := DB.Exec(`
		UPDATE custom_rows r
		JOIN api_keys k ON r.created_by = CONCAT('apikey:', k.name)
		SET r.source_api_key_id = k.id
		WHERE r.created_by LIKE 'apikey:%' AND r.source_api_key_id IS NULL
	`); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[Migration] custom_rows backfill source_api_key_id from api_keys: %d rows", n)
		}
	}
	// 6) backfill：apikey 创建但反查不到 key 的，填 sentinel UUID
	if res, err := DB.Exec(`
		UPDATE custom_rows
		SET source_api_key_id = '00000000-0000-0000-0000-000000000000'
		WHERE created_by LIKE 'apikey:%' AND source_api_key_id IS NULL
	`); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[Migration] custom_rows backfill source_api_key_id sentinel: %d rows", n)
		}
	}

	// ===== SSO app_roles 角色同步（2026-07-20） =====
	// SSO 下发 app_roles: ["infra_team"] (字符串数组, id_token 与 userinfo 均有)
	// 登录时按组名同步角色, 需要区分「SSO 同步来的」与「管理员手动配的」:
	//   - source=sso    : 每次登录重新同步, SSO 移除时打 sso_removed_at 标记(不删记录)
	//   - source=manual : 永不被同步逻辑触碰, 保证手动配置不被覆盖
	if _, err := DB.Exec(`ALTER TABLE roles ADD COLUMN source VARCHAR(16) DEFAULT 'manual'`); err == nil {
		log.Printf("[Migration] roles: add column source")
	}
	if _, err := DB.Exec(`ALTER TABLE roles ADD COLUMN sso_removed_at DATETIME NULL`); err == nil {
		log.Printf("[Migration] roles: add column sso_removed_at")
	}
	if _, err := DB.Exec(`ALTER TABLE user_roles ADD COLUMN source VARCHAR(16) DEFAULT 'manual'`); err == nil {
		log.Printf("[Migration] user_roles: add column source")
	}
	if _, err := DB.Exec(`ALTER TABLE user_roles ADD COLUMN sso_removed_at DATETIME NULL`); err == nil {
		log.Printf("[Migration] user_roles: add column sso_removed_at")
	}
	// 存量数据一律视为手动配置 —— 绝不能让首次上线的同步逻辑把管理员配好的角色当成
	// 「SSO 已移除」而打上标记
	DB.Exec(`UPDATE roles SET source = 'manual' WHERE source IS NULL OR source = ''`)
	DB.Exec(`UPDATE user_roles SET source = 'manual' WHERE source IS NULL OR source = ''`)

	// 一次性清理孤儿：历史上删用户只删了 users 表、没清 user_roles，残留行会让角色人数虚高。
	// 只删「user_id 已不在 users 表」的行，绝对安全（对应用户已不存在）。
	if res, err := DB.Exec(`DELETE ur FROM user_roles ur
		LEFT JOIN users u ON ur.user_id = u.id WHERE u.id IS NULL`); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[Migration] user_roles: 清理已删用户的孤儿行 %d 条", n)
		}
	}
}

// cmdbMenuCodes / cmdbButtonCodes 是 CMDB 全部权限码，顺序即角色配置页的展示顺序。
// 新增 CMDB 菜单页或操作时，这里和上面的种子两处都要加。
var cmdbMenuCodes = []string{
	"menu:cmdb_overview",
	"menu:cmdb_hosts", "menu:cmdb_cloud_ips", "menu:cmdb_cloud_networks",
	"menu:cmdb_cloud_firewalls", "menu:cmdb_cloud_lbs", "menu:cmdb_cloud_audit",
	"menu:cmdb_domains", "menu:cmdb_dns_records", "menu:cmdb_cdn_sites",
	"menu:cmdb_certs", "menu:cmdb_cert_inspect",
	// 关系图谱：原来跟「基础配置」共用 menu:cmdb_basic，只读角色因此自动获得
	// 访问权。它展示的是资产间关系，该单独授权（CMDB-045）。
	"menu:cmdb_relations",
	"menu:cmdb_k8s_clusters", "menu:cmdb_version_upgrade", "menu:cmdb_k8s_nodes",
	"menu:cmdb_k8s_workloads", "menu:cmdb_k8s_pods", "menu:cmdb_k8s_networking",
	"menu:cmdb_k8s_storage", "menu:cmdb_k8s_events", "menu:cmdb_k8s_health",
	"menu:cmdb_k8s_topology", "menu:cmdb_k8s_ns_project",
	"menu:cmdb_event_center", "menu:cmdb_alerts", "menu:cmdb_k8s_usage", "menu:cmdb_cost",
	"menu:cmdb_basic", "menu:cmdb_integrations", "menu:cmdb_notify",
	"menu:cmdb_cron", "menu:cmdb_task_runs", "menu:cmdb_users", "menu:cmdb_audit",
}

var cmdbButtonCodes = []string{
	"cmdb:manage_domains", "cmdb:sync_domains", "cmdb:manage_dns",
	"cmdb:manage_certs", "cmdb:issue_cert", "cmdb:manage_cdn",
	"cmdb:sync_cdn", "cmdb:manage_records",
	"cmdb:manage_hosts", "cmdb:sync_cloud", "cmdb:manage_cloud_projects",
	"cmdb:manage_clusters", "cmdb:sync_k8s", "cmdb:manage_ns_project",
	"cmdb:manage_upgrade", "cmdb:k8s_diag",
	"cmdb:manage_cost_rates",
	"cmdb:manage_integrations", "cmdb:manage_mcp",
	"cmdb:manage_basic", "cmdb:manage_notify", "cmdb:manage_cron", "cmdb:run_task",
	"cmdb:manage_users", "cmdb:revert_change",
}

// initCMDBRoleTemplates 预置 CMDB 的 5 个角色模板。
//
//	和上面 role_admin / role_operator 的做法有一处**故意的不同**：权限只在角色
//	「首次创建」时灌一次。INSERT IGNORE 只挡得住重复插入，挡不住"管理员在 UI 上
//	取消了某条权限"——每次启动重灌会把人家的调整悄悄改回去，而且毫无提示。
//	模板的职责是给一个起点，不是持续纠正。
//
//	is_system 取 0：这几个是模板不是内核角色，管理员应该能改名、能删掉不用的
//	（rbac.go 里 is_system=1 的角色禁止改名和删除）。
//
//	⚠️ 角色名不能带"运维"二字：上面那段值班权限分配用 `name LIKE '%运维%'`
//	匹配角色，叫"K8s 运维"会被顺带塞进值班记录权限。所以这里用"集群管理员"。
func initCMDBRoleTemplates() {
	allMenus, allButtons := cmdbMenuCodes, cmdbButtonCodes

	// codes 拼接：第一项永远是父权限 menu:cmdb，缺了它 CMDB 侧 portal-auth 直接 403
	codes := func(parts ...[]string) []string {
		out := []string{"menu:cmdb"}
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	except := func(src []string, drop ...string) []string {
		out := make([]string, 0, len(src))
		for _, s := range src {
			skip := false
			for _, d := range drop {
				if s == d {
					skip = true
					break
				}
			}
			if !skip {
				out = append(out, s)
			}
		}
		return out
	}

	templates := []struct {
		ID, Code, Name, Desc string
		Perms                []string
	}{
		{"role_cmdb_viewer", "cmdb_viewer", "CMDB 只读",
			"看得到全部台账但改不了任何东西；不含云成本、接入凭据与操作审计",
			codes(except(allMenus, "menu:cmdb_cost", "menu:cmdb_integrations", "menu:cmdb_users", "menu:cmdb_audit"))},

		{"role_cmdb_asset", "cmdb_asset", "CMDB 资产管理员",
			"域名/证书/CDN 与云资源台账的日常维护。不含 DNS 写入与证书签发（那两项会直接改动线上解析和调用 CA）",
			codes([]string{
				"menu:cmdb_overview",
				"menu:cmdb_domains", "menu:cmdb_dns_records", "menu:cmdb_cdn_sites",
				"menu:cmdb_certs", "menu:cmdb_cert_inspect",
				"menu:cmdb_hosts", "menu:cmdb_cloud_ips", "menu:cmdb_cloud_networks",
				"menu:cmdb_cloud_firewalls", "menu:cmdb_cloud_lbs", "menu:cmdb_cloud_audit",
			}, []string{
				"cmdb:manage_domains", "cmdb:sync_domains", "cmdb:manage_certs",
				"cmdb:manage_records", "cmdb:manage_cdn", "cmdb:sync_cdn",
			})},

		{"role_cmdb_cluster", "cmdb_cluster", "CMDB 集群管理员",
			"K8s 多集群查看、体检与升级计划维护。不含集群纳管（纳管需要填写集群凭据）",
			codes([]string{
				"menu:cmdb_overview",
				"menu:cmdb_k8s_clusters", "menu:cmdb_version_upgrade", "menu:cmdb_k8s_nodes",
				"menu:cmdb_k8s_workloads", "menu:cmdb_k8s_pods", "menu:cmdb_k8s_networking",
				"menu:cmdb_k8s_storage", "menu:cmdb_k8s_events", "menu:cmdb_k8s_health",
				"menu:cmdb_k8s_topology", "menu:cmdb_k8s_ns_project",
				"menu:cmdb_event_center", "menu:cmdb_alerts", "menu:cmdb_k8s_usage", "menu:cmdb_cost",
			}, []string{
				"cmdb:sync_k8s", "cmdb:manage_ns_project", "cmdb:manage_upgrade", "cmdb:k8s_diag",
			})},

		{"role_cmdb_cost", "cmdb_cost", "CMDB 成本分析",
			"只看云成本与资源使用率，不涉及任何资产明细与配置",
			codes([]string{"menu:cmdb_overview", "menu:cmdb_cost", "menu:cmdb_k8s_usage"})},

		{"role_cmdb_admin", "cmdb_admin", "CMDB 管理员",
			"CMDB 全部菜单与操作权限，含接入凭据、MCP Token 与变更回滚",
			codes(allMenus, allButtons)},
	}

	for _, t := range templates {
		res, err := DB.Exec(
			`INSERT IGNORE INTO roles (id, code, name, description, is_system) VALUES (?, ?, ?, ?, 0)`,
			t.ID, t.Code, t.Name, t.Desc)
		if err != nil {
			log.Printf("[CMDB] 创建角色模板失败 %s: %v", t.Code, err)
			continue
		}
		if affected, _ := res.RowsAffected(); affected == 0 {
			continue // 角色已存在：不碰它的权限，避免覆盖管理员的调整
		}

		granted := 0
		for _, code := range t.Perms {
			var permID string
			if err := DB.QueryRow("SELECT id FROM permissions WHERE code = ?", code).Scan(&permID); err != nil || permID == "" {
				// 权限码写错或种子漏了，静默跳过等于角色少一块权限却没人知道
				log.Printf("[CMDB] WARN 角色模板 %s 引用了不存在的权限码 %s，已跳过", t.Code, code)
				continue
			}
			DB.Exec(`INSERT IGNORE INTO role_permissions (id, role_id, permission_id) VALUES (?, ?, ?)`,
				"rp_"+t.ID+"_"+permID, t.ID, permID)
			granted++
		}

		// 应用级准入：没有这条关联，用户就算有 menu:cmdb 也进不去
		// （CMDB 侧 portal-auth 会调 external-apps/my 确认角色关联了 cmdb 应用）
		DB.Exec(`INSERT IGNORE INTO role_external_apps (id, role_id, app_key) VALUES (?, ?, 'cmdb')`,
			"rea_"+t.ID+"_cmdb", t.ID)

		log.Printf("[CMDB] 创建角色模板: %s（%d 项权限）", t.Name, granted)
	}
}

// Close 关闭数据库连接
func Close() {
	if DB != nil {
		DB.Close()
	}
}

// ScheduleDefaultTimezone 排班默认时区。员工没有任何时区记录时按这个算，
// 保证加时区功能之前的历史排班语义完全不变。
const ScheduleDefaultTimezone = "Asia/Shanghai"

// scheduleSeedGroupBelgrade v772: ig 组在塞尔维亚，一次性种子用。
const scheduleSeedGroupBelgrade = "ig"

// seedScheduleTimezones 时区历史的初始回填。
//
//	① 每个员工兜底一条 1970-01-01 -> Asia/Shanghai，保证任何日期都能解析出时区；
//	② ig 组一次性种子成 Europe/Belgrade，生效日期取该组最早的排班记录日期。
//
// ② 只在「还没有任何人配过非默认时区」时执行一次，之后管理员在界面上怎么改都不会被覆盖。
func seedScheduleTimezones() {
	// ① 兜底基线。唯一键 (employee_id, effective_from) 保证重复执行无副作用。
	if res, err := DB.Exec(`
		INSERT IGNORE INTO schedule_employee_timezones (employee_id, timezone, effective_from, created_by)
		SELECT id, ?, '1970-01-01', 'system' FROM schedule_employees
	`, ScheduleDefaultTimezone); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[排班] 时区基线回填: %d 名员工 -> %s (1970-01-01 起)", n, ScheduleDefaultTimezone)
		}
	} else {
		log.Printf("[排班] WARN 时区基线回填失败: %v", err)
	}

	// ② ig 组种子。已经有人配过非默认时区就不再自动写，避免覆盖人工配置。
	var configured int
	if err := DB.QueryRow(`
		SELECT COUNT(*) FROM schedule_employee_timezones WHERE timezone <> ?
	`, ScheduleDefaultTimezone).Scan(&configured); err != nil {
		log.Printf("[排班] WARN 检查已配置时区失败，跳过 ig 组种子: %v", err)
		return
	}
	if configured > 0 {
		return
	}

	// 生效日期统一取 ig 组最早的排班记录日期；该组一条排班都没有就用今天
	var effectiveFrom string
	if err := DB.QueryRow(`
		SELECT COALESCE(DATE_FORMAT(MIN(s.shift_date), '%Y-%m-%d'), DATE_FORMAT(CURDATE(), '%Y-%m-%d'))
		FROM schedule_employees e
		JOIN schedule_shifts s ON s.employee_id = e.id
		WHERE LOWER(TRIM(e.group_name)) = ?
	`, scheduleSeedGroupBelgrade).Scan(&effectiveFrom); err != nil || effectiveFrom == "" {
		log.Printf("[排班] WARN 取 %s 组最早排班日期失败，跳过时区种子: %v", scheduleSeedGroupBelgrade, err)
		return
	}

	res, err := DB.Exec(`
		INSERT IGNORE INTO schedule_employee_timezones (employee_id, timezone, effective_from, created_by)
		SELECT id, 'Europe/Belgrade', ?, 'system'
		FROM schedule_employees WHERE LOWER(TRIM(group_name)) = ?
	`, effectiveFrom, scheduleSeedGroupBelgrade)
	if err != nil {
		log.Printf("[排班] WARN %s 组时区种子写入失败: %v", scheduleSeedGroupBelgrade, err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("[排班] %s 组时区种子: %d 人 -> Europe/Belgrade (%s 起)。"+
			"⚠️ 这是按组别自动推断的，请在排班页「员工 -> 时区」逐人核对生效日期与归属",
			scheduleSeedGroupBelgrade, n, effectiveFrom)
	}
}

package database

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"opsplatform-deploy-backend/config"
)

var DB *sql.DB

func InitMySQL(cfg *config.Config) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		cfg.MySQLUser, cfg.MySQLPassword, cfg.MySQLHost, cfg.MySQLPort, cfg.MySQLDatabase)

	var err error
	DB, err = sql.Open("mysql", dsn)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
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
		return fmt.Errorf("ping db: %w", err)
	}
	log.Println("Database connected")
	return createTables()
}

func createTables() error {
	tables := []string{
		// 1. 项目
		`CREATE TABLE IF NOT EXISTS projects (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(64) NOT NULL UNIQUE COMMENT '项目代号: g50/g32/opsplatform',
			display_name VARCHAR(128) DEFAULT '' COMMENT '显示名',
			description VARCHAR(255) DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 2. 环境字典 (每个环境独立 ArgoCD 实例)
		`CREATE TABLE IF NOT EXISTS environments (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(32) NOT NULL UNIQUE COMMENT '环境代号: dev/test/uat/prod',
			display_name VARCHAR(64) DEFAULT '' COMMENT '显示名',
			auto_sync TINYINT DEFAULT 0 COMMENT '1=发布后自动调ArgoCD sync',
			argocd_url VARCHAR(500) DEFAULT '' COMMENT '该环境 ArgoCD 地址',
			argocd_token TEXT COMMENT 'ArgoCD token (AES加密)',
			description VARCHAR(255) DEFAULT '',
			sort_order INT DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 3. 项目-环境 (对应 GitLab 里某项目某环境的目录, 如 g50-uat)
		`CREATE TABLE IF NOT EXISTS project_envs (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			project_id BIGINT NOT NULL,
			env_id BIGINT NOT NULL,
			git_repo VARCHAR(500) NOT NULL COMMENT 'Git仓库地址',
			git_branch VARCHAR(64) DEFAULT 'main',
			git_base_path VARCHAR(255) DEFAULT '' COMMENT '项目-环境在Git仓库的基础路径, 如 charts/g50-uat',
			namespace VARCHAR(64) DEFAULT '' COMMENT 'K8s命名空间',
			argocd_project VARCHAR(64) DEFAULT 'default' COMMENT 'ArgoCD project',
			argocd_cluster VARCHAR(128) DEFAULT 'in-cluster' COMMENT 'ArgoCD目标集群',
			status VARCHAR(20) DEFAULT 'active',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_proj_env (project_id, env_id),
			INDEX idx_project (project_id),
			INDEX idx_env (env_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 4. Helm Chart 模板
		`CREATE TABLE IF NOT EXISTS chart_templates (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(64) NOT NULL UNIQUE COMMENT '模板名: test1/test2/test3 用户自定义',
			type VARCHAR(20) NOT NULL DEFAULT 'backend' COMMENT 'backend/frontend',
			description VARCHAR(255) DEFAULT '',
			source_type VARCHAR(20) DEFAULT 'git' COMMENT 'git/embedded',
			git_repo VARCHAR(500) DEFAULT '' COMMENT '模板chart所在仓库',
			chart_path VARCHAR(255) DEFAULT '' COMMENT '模板chart路径',
			default_values MEDIUMTEXT COMMENT '默认 values.yaml 全文',
			probe_config TEXT COMMENT '默认探针 JSON',
			configmap_schema TEXT COMMENT '前端 config.js schema JSON',
			version VARCHAR(32) DEFAULT 'v1',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 5. 模块 (核心)
		`CREATE TABLE IF NOT EXISTS modules (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			project_env_id BIGINT NOT NULL,
			name VARCHAR(128) NOT NULL COMMENT '模块名 (用户自填, 不加前缀)',
			template_id BIGINT NOT NULL,
			template_version VARCHAR(32) DEFAULT 'v1' COMMENT '基于模板的版本',
			image_repo VARCHAR(500) DEFAULT '' COMMENT 'Harbor 镜像仓库',
			current_tag VARCHAR(128) DEFAULT '',
			replicas INT DEFAULT 1 COMMENT '副本数, 0=软下线',
			autoscaling TEXT COMMENT 'HPA配置 JSON',
			resources TEXT COMMENT 'requests/limits JSON',
			rolling_update TEXT COMMENT 'maxSurge/maxUnavailable JSON',
			revision_history_limit INT DEFAULT 1,
			env_vars TEXT COMMENT '普通环境变量 JSON',
			extra_env_vars TEXT COMMENT 'Secret引用名数组 JSON',
			tidb_secrets TEXT COMMENT 'tidbSecrets JSON',
			probe_override TEXT COMMENT '探针覆盖 JSON (空则用模板默认)',
			configmap_data TEXT COMMENT '前端 config.js 实际值 JSON',
			git_chart_path VARCHAR(500) DEFAULT '' COMMENT 'chart在Git里的相对路径',
			argocd_app_name VARCHAR(255) DEFAULT '' COMMENT 'ArgoCD Application name',
			status VARCHAR(20) DEFAULT 'active' COMMENT 'active/scaled_zero/disabled/deleted',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_pe_name (project_env_id, name),
			INDEX idx_template (template_id),
			INDEX idx_status (status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 6. Secret (DB 加密存储, 同步渲染到 z-kv-secrets)
		`CREATE TABLE IF NOT EXISTS secrets (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			project_env_id BIGINT NOT NULL,
			name VARCHAR(128) NOT NULL COMMENT 'Secret 名',
			type VARCHAR(32) DEFAULT 'Opaque',
			data MEDIUMTEXT COMMENT 'JSON: {key:AES加密value}',
			description VARCHAR(255) DEFAULT '',
			synced_at TIMESTAMP NULL COMMENT '最后同步到Git的时间',
			sync_status VARCHAR(20) DEFAULT 'pending' COMMENT 'pending/synced/failed',
			sync_error TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
			UNIQUE KEY uk_pe_name (project_env_id, name),
			INDEX idx_pe (project_env_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 7. 通知人 (对齐告警系统简化版)
		`CREATE TABLE IF NOT EXISTS contacts (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(64) NOT NULL,
			lark_id VARCHAR(200) NOT NULL COMMENT 'Lark open_id',
			remark VARCHAR(255) DEFAULT '',
			status TINYINT DEFAULT 1 COMMENT '1=启用 0=禁用',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 8. Lark 群配置
		`CREATE TABLE IF NOT EXISTS lark_configs (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(64) NOT NULL UNIQUE,
			webhook_url VARCHAR(500) NOT NULL,
			secret VARCHAR(255) DEFAULT '' COMMENT '签名密钥',
			lark_type VARCHAR(20) DEFAULT 'feishu' COMMENT 'feishu/larksuite',
			description VARCHAR(255) DEFAULT '',
			status TINYINT DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 9. 项目-环境 通知绑定
		`CREATE TABLE IF NOT EXISTS project_env_notify (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			project_env_id BIGINT NOT NULL UNIQUE,
			lark_config_id BIGINT DEFAULT 0,
			contact_ids TEXT COMMENT 'JSON 数组 [1,3,5]',
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 10. 发布历史
		`CREATE TABLE IF NOT EXISTS deployments (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			module_id BIGINT NOT NULL,
			module_name VARCHAR(128) DEFAULT '' COMMENT '冗余模块名(模块删除后仍可查)',
			project_env_id BIGINT DEFAULT 0,
			action VARCHAR(32) NOT NULL COMMENT 'create/update_image/update_config/update_secret/restart/scale_zero/scale_up/delete/rollback',
			from_tag VARCHAR(128) DEFAULT '',
			to_tag VARCHAR(128) DEFAULT '',
			values_before MEDIUMTEXT,
			values_after MEDIUMTEXT,
			git_commit VARCHAR(64) DEFAULT '',
			git_commit_url VARCHAR(500) DEFAULT '',
			argocd_sync_status VARCHAR(20) DEFAULT 'pending' COMMENT 'skipped/pending/success/failed',
			argocd_sync_msg TEXT,
			notify_status VARCHAR(20) DEFAULT 'pending' COMMENT 'skipped/pending/success/failed',
			notify_msg TEXT,
			operator VARCHAR(64) DEFAULT '',
			status VARCHAR(20) DEFAULT 'pending' COMMENT 'pending/success/failed',
			error_msg TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			INDEX idx_module (module_id),
			INDEX idx_pe (project_env_id),
			INDEX idx_created (created_at),
			INDEX idx_action (action)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 11. 环境变量模板
		`CREATE TABLE IF NOT EXISTS env_templates (
			id BIGINT AUTO_INCREMENT PRIMARY KEY,
			name VARCHAR(64) NOT NULL UNIQUE,
			env_vars TEXT COMMENT '[{"key":"K","value":"V"}]',
			description VARCHAR(255) DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,

		// 12. 全局配置 (单行)
		`CREATE TABLE IF NOT EXISTS global_config (
			id BIGINT PRIMARY KEY DEFAULT 1,
			gitlab_url VARCHAR(500) DEFAULT '',
			gitlab_token TEXT COMMENT 'AES加密',
			gitlab_user VARCHAR(64) DEFAULT 'deploy-bot',
			gitlab_email VARCHAR(128) DEFAULT 'deploy-bot@local',
			harbor_url VARCHAR(500) DEFAULT '',
			harbor_user VARCHAR(64) DEFAULT '',
			harbor_password TEXT COMMENT 'AES加密',
			argocd_url VARCHAR(500) DEFAULT '',
			argocd_token TEXT COMMENT 'AES加密',
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`,
	}

	for _, t := range tables {
		if _, err := DB.Exec(t); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}

	if err := seedDefaults(); err != nil {
		log.Printf("seedDefaults error: %v", err)
	}

	log.Println("Database tables initialized")
	return nil
}

// seedDefaults 种子数据: 默认环境、空全局配置行 + 为已有 environments 表自动补充字段
func seedDefaults() error {
	// Auto-migrate: 补充 environments 表新字段
	addColumnIfMissing("environments", "argocd_url", "VARCHAR(500) DEFAULT '' COMMENT '该环境 ArgoCD 地址'")
	addColumnIfMissing("environments", "argocd_token", "TEXT COMMENT 'ArgoCD token (AES加密)'")
	addColumnIfMissing("environments", "description", "VARCHAR(255) DEFAULT ''")

	// 默认环境
	envs := []struct {
		name      string
		display   string
		autoSync  int
		sortOrder int
	}{
		{"dev", "开发", 1, 1},
		{"test", "测试", 1, 2},
		{"uat", "预发布", 1, 3},
		{"prod", "生产", 0, 4},
	}
	for _, e := range envs {
		var c int
		DB.QueryRow("SELECT COUNT(*) FROM environments WHERE name=?", e.name).Scan(&c)
		if c == 0 {
			DB.Exec(`INSERT INTO environments (name, display_name, auto_sync, sort_order) VALUES (?,?,?,?)`,
				e.name, e.display, e.autoSync, e.sortOrder)
		}
	}

	// 全局配置默认行
	var gc int
	DB.QueryRow("SELECT COUNT(*) FROM global_config WHERE id=1").Scan(&gc)
	if gc == 0 {
		DB.Exec(`INSERT INTO global_config (id) VALUES (1)`)
	}

	return nil
}

// addColumnIfMissing 如果字段不存在则添加
func addColumnIfMissing(table, column, ddl string) {
	var cnt int
	DB.QueryRow(`SELECT COUNT(*) FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`, table, column).Scan(&cnt)
	if cnt == 0 {
		sqlStr := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column, ddl)
		if _, err := DB.Exec(sqlStr); err != nil {
			log.Printf("[Migrate] failed to add %s.%s: %v", table, column, err)
		} else {
			log.Printf("[Migrate] added column %s.%s", table, column)
		}
	}
}

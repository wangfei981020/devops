-- Deploy Center V1 schema
-- Date: 2026-04-19

CREATE TABLE IF NOT EXISTS global_config (
  id BIGINT NOT NULL PRIMARY KEY,
  gitlab_url VARCHAR(255) NOT NULL DEFAULT '',
  gitlab_user VARCHAR(64) NOT NULL DEFAULT '',
  gitlab_email VARCHAR(128) NOT NULL DEFAULT '',
  gitlab_token VARCHAR(512) NOT NULL DEFAULT '',
  lark_default_webhook VARCHAR(512) NOT NULL DEFAULT '',
  lark_default_secret VARCHAR(256) NOT NULL DEFAULT '',
  poll_interval_sec INT NOT NULL DEFAULT 10,
  poll_timeout_min INT NOT NULL DEFAULT 3,
  git_retry_count INT NOT NULL DEFAULT 3,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO global_config (id) VALUES (1);

CREATE TABLE IF NOT EXISTS project_env (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  display_name VARCHAR(128) NOT NULL DEFAULT '',
  env_type ENUM('uat','prod') NOT NULL DEFAULT 'uat',
  git_repo VARCHAR(255) NOT NULL DEFAULT '',
  git_branch VARCHAR(64) NOT NULL DEFAULT 'main',
  chart_base_path VARCHAR(255) NOT NULL DEFAULT '',
  namespace VARCHAR(64) NOT NULL DEFAULT '',
  argocd_url VARCHAR(255) NOT NULL DEFAULT '',
  argocd_token VARCHAR(512) NOT NULL DEFAULT '',
  lark_webhook VARCHAR(512) NOT NULL DEFAULT '',
  lark_secret VARCHAR(256) NOT NULL DEFAULT '',
  auto_sync TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS module (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  project_env_id BIGINT NOT NULL,
  name VARCHAR(128) NOT NULL,
  current_tag VARCHAR(128) NOT NULL DEFAULT '',
  image_repository VARCHAR(255) NOT NULL DEFAULT '',
  argocd_app_name VARCHAR(192) NOT NULL DEFAULT '',
  last_scanned_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_env_name (project_env_id, name),
  KEY idx_env (project_env_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS deployment (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  project_env_id BIGINT NOT NULL,
  action ENUM('update_image','restart','rollback') NOT NULL,
  ref_deployment_id BIGINT NULL,
  module_names JSON NULL,
  changes JSON NULL,
  git_commit VARCHAR(64) NOT NULL DEFAULT '',
  git_commit_url VARCHAR(512) NOT NULL DEFAULT '',
  argocd_results JSON NULL,
  lark_notify ENUM('success','failed','skipped') NOT NULL DEFAULT 'skipped',
  operator VARCHAR(64) NOT NULL DEFAULT 'system',
  status ENUM('pending','success','partial','failed','no_change') NOT NULL DEFAULT 'pending',
  error_msg TEXT NULL,
  duration_sec INT NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_env_time (project_env_id, created_at),
  KEY idx_time (created_at),
  KEY idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS schema_migrations (
  version VARCHAR(64) NOT NULL PRIMARY KEY,
  applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

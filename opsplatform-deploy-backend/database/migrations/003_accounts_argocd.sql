-- Migration 003: accounts + argocd_instance + multi-namespace
-- Date: 2026-04-21

-- 账号表（身份选择 / Lark 艾特）
CREATE TABLE IF NOT EXISTS account (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(64) NOT NULL,
  display_name VARCHAR(128) NOT NULL DEFAULT '',
  lark_id VARCHAR(128) NOT NULL DEFAULT '',
  email VARCHAR(128) NOT NULL DEFAULT '',
  remark VARCHAR(500) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ArgoCD 实例表（全局可管理多个）
CREATE TABLE IF NOT EXISTS argocd_instance (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  url VARCHAR(255) NOT NULL,
  token TEXT NOT NULL,
  description VARCHAR(500) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- project_env 关联 argocd_instance
ALTER TABLE project_env ADD COLUMN argocd_instance_id BIGINT NULL DEFAULT NULL;

-- module 支持 namespace（从 -apps/values.yaml 扫描得到）
ALTER TABLE module ADD COLUMN namespace VARCHAR(64) NOT NULL DEFAULT '';

-- 数据迁移：每个不同的 argocd_url 创建一个 argocd_instance
INSERT IGNORE INTO argocd_instance (name, url, token, description)
SELECT CONCAT('instance-', LEFT(MD5(argocd_url), 6)), argocd_url, MAX(argocd_token), '从 project_env 自动迁移'
FROM project_env WHERE argocd_url != '' GROUP BY argocd_url;

-- project_env 链接到 argocd_instance
UPDATE project_env pe JOIN argocd_instance ai ON pe.argocd_url = ai.url
SET pe.argocd_instance_id = ai.id WHERE pe.argocd_url != '';

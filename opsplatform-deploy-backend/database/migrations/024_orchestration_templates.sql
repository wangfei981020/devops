-- 服务编排（Service Orchestration）
-- Date: 2026-07-10
-- 1) orchestration_template：参照模板 = 指向 git 里某个样板服务（存指针，不存 YAML 正文）
-- 2) deploy_environment：可配置环境列表（dev/test/uat/prod…），每环境独立权限档
-- 3) 放开 project_env.env_type（ENUM('uat','prod') → VARCHAR），兼容 dev/test 等新环境

-- ========== 1. 参照模板 ==========
CREATE TABLE IF NOT EXISTS orchestration_template (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(128) NOT NULL,                               -- 模板显示名
  project VARCHAR(64) NOT NULL DEFAULT '',                  -- 绑定项目；空=全局可用
  module_type ENUM('frontend','backend') NOT NULL DEFAULT 'backend',
  src_env VARCHAR(64) NOT NULL,                             -- 样板所在 project_env.name（借它的 git 配置）
  src_service VARCHAR(128) NOT NULL,                        -- 样板服务目录名（chart_base_path 下）
  description VARCHAR(500) NOT NULL DEFAULT '',
  config JSON NULL,                                         -- 预留：推导规则 / 默认覆盖
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_by VARCHAR(64) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_name (name),
  KEY idx_project_type (project, module_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ========== 2. 可配置环境 ==========
CREATE TABLE IF NOT EXISTS deploy_environment (
  name VARCHAR(32) NOT NULL PRIMARY KEY,                    -- dev/test/uat/prod...
  display_name VARCHAR(64) NOT NULL DEFAULT '',
  permission_code VARCHAR(64) NOT NULL DEFAULT '',          -- 每环境独立权限档，如 submit_uat / submit_dev
  sort_order INT NOT NULL DEFAULT 0,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 兜底两档（跟现有硬编码权限 submit_uat/submit_prod 对齐），dev/test 由用户在页面上加
INSERT IGNORE INTO deploy_environment (name, display_name, permission_code, sort_order) VALUES
  ('uat',  'UAT',  'submit_uat',  10),
  ('prod', 'PROD', 'submit_prod', 20);

-- ========== 3. 放开 project_env.env_type ==========
-- ENUM('uat','prod') → VARCHAR(16)：现有 'uat'/'prod' 值原样保留，字符串比较（models.EnvPROD="prod"）不变
ALTER TABLE project_env MODIFY COLUMN env_type VARCHAR(16) NOT NULL DEFAULT 'uat';

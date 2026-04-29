-- Migration 018: 引入 VM 目标类型，并行支持 ansible 部署
--
-- 设计：
--   project 加 target_type 总开关：'k8s' (现有) 或 'vm' (新)
--   K8s 现有表 project_env / module 完全不动
--   VM 用独立表 vm_project_env / vm_service，schema 干净，将来扩展第三种 target 容易
--   发布历史共用 deployment 表，加 target_type 区分；agent_task_id 关联 agent 那边的任务
--   list-version API 全局共用一个 token，放 global_config

-- ---------------------------------------------------------------
-- 1. project 加 target_type
-- ---------------------------------------------------------------
ALTER TABLE project ADD COLUMN target_type ENUM('k8s','vm') NOT NULL DEFAULT 'k8s';

-- ---------------------------------------------------------------
-- 2. deployment 加 target_type + agent_task_id
-- ---------------------------------------------------------------
ALTER TABLE deployment ADD COLUMN target_type ENUM('k8s','vm') NOT NULL DEFAULT 'k8s';
ALTER TABLE deployment ADD COLUMN agent_task_id VARCHAR(64) NULL COMMENT 'agent 那边的任务 ID，VM 部署用';

-- ---------------------------------------------------------------
-- 3. global_config 加 list-version API + token（VM 部署用）
-- ---------------------------------------------------------------
ALTER TABLE global_config ADD COLUMN list_version_api VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE global_config ADD COLUMN list_version_token TEXT COMMENT '加密存储';

-- ---------------------------------------------------------------
-- 4. deploy_agent 表（agent 元数据，类似 argocd_instance）
-- ---------------------------------------------------------------
CREATE TABLE deploy_agent (
  id INT AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(64) NOT NULL UNIQUE COMMENT 'main-ansible 等可读名',
  url VARCHAR(255) NOT NULL COMMENT 'https://10.x.x.x:8443',
  token TEXT NOT NULL COMMENT '加密存储；agent 鉴权用',
  description VARCHAR(255),
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---------------------------------------------------------------
-- 5. vm_project_env 表（独立于 K8s 的 project_env）
-- ---------------------------------------------------------------
CREATE TABLE vm_project_env (
  id INT AUTO_INCREMENT PRIMARY KEY,
  project_id INT NOT NULL,
  name VARCHAR(64) NOT NULL UNIQUE COMMENT 'G01-UAT / G01-LPT / G02-PROD',
  display_name VARCHAR(128),
  env_type ENUM('LPT','UAT','PROD') NOT NULL,
  agent_id INT NOT NULL COMMENT '关联 deploy_agent.id',
  -- ansible 仓库路径（在 agent 机器上）
  ansible_root VARCHAR(255) NOT NULL DEFAULT '/etc/ansible',
  project_code VARCHAR(32) NOT NULL COMMENT 'G01 / G02 → 拼 ansible 路径用',
  -- 推断的路径（不存 DB，agent 按约定拼）：
  --   playbook:    {ansible_root}/{project_code}/{env_type}/<service>.yaml
  --   inventory:   {ansible_root}/inventory/{project_code}/{env_type}/{project_code}_{env_type}_hosts
  --   rsync 脚本:  {ansible_root}/{project_code}/{project_code}.py
  --   版本根目录:  /data/vcs/{project_code}/tidb/<service>/
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_project (project_id),
  INDEX idx_agent (agent_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---------------------------------------------------------------
-- 6. vm_service 表（= ansible playbook 文件）
-- ---------------------------------------------------------------
CREATE TABLE vm_service (
  id INT AUTO_INCREMENT PRIMARY KEY,
  vm_project_env_id INT NOT NULL,
  name VARCHAR(128) NOT NULL COMMENT '= playbook 文件名（去 .yaml）= list-version path 的 service 段',
  ansible_group VARCHAR(128) COMMENT 'playbook 第一行 hosts 解析出来',
  hosts JSON COMMENT 'inventory 解析出的 IP 列表',
  current_version VARCHAR(64) COMMENT '上次 update_version 成功后写入',
  last_scanned_at DATETIME COMMENT 'agent 最后一次扫服务列表的时间',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_env_service (vm_project_env_id, name),
  INDEX idx_env (vm_project_env_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

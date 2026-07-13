-- Migration 028: 服务编排增强
--   1) project_env 加 harbor_project 列——「项目参数」页每环境配 Harbor 项目名（跟 ingress_gateway 同套路）。
--      留空=新增模块时自动用项目名（从环境名去掉 -env 后缀推）。镜像仓库 = 全局 harbor 域名 / harbor_project / 服务名。
--   2) orchestration_task 表——新增模块提交改后台异步执行，任务状态+commit 在「新增历史」页展示。

ALTER TABLE project_env ADD COLUMN harbor_project VARCHAR(255) NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS orchestration_task (
  id             BIGINT AUTO_INCREMENT PRIMARY KEY,
  project_env_id BIGINT       NOT NULL,
  env_name       VARCHAR(128) NOT NULL DEFAULT '',
  module_name    VARCHAR(512) NOT NULL DEFAULT '',   -- 单个=模块名；批量=逗号拼接/摘要
  kind           ENUM('single','batch') NOT NULL DEFAULT 'single',
  operator       VARCHAR(128) NOT NULL DEFAULT '',
  status         ENUM('pending','success','failed') NOT NULL DEFAULT 'pending',
  commit_sha     VARCHAR(64)  NOT NULL DEFAULT '',
  commit_url     VARCHAR(512) NOT NULL DEFAULT '',
  error_msg      TEXT,
  created_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_orch_task_created (created_at),
  INDEX idx_orch_task_env (project_env_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

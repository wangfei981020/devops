-- Migration 013: 发布历史自动清理配置 + 同模块互斥锁

-- 1. global_config 加发布历史保留天数 + 上次清理时间
ALTER TABLE global_config ADD COLUMN history_retention_days INT NOT NULL DEFAULT 180;

ALTER TABLE global_config ADD COLUMN last_history_cleanup_at DATETIME NULL;

-- 2. 同模块互斥锁
-- 每个 (env_name, module_name) 同时只能有一个活跃锁
-- expires_at 兜底：发布卡死或 deploy-backend 崩溃，锁不会永久占住，每 5 分钟清理一次
CREATE TABLE IF NOT EXISTS module_deploy_lock (
  env_name      VARCHAR(100) NOT NULL,
  module_name   VARCHAR(100) NOT NULL,
  deployment_id BIGINT NOT NULL,
  operator      VARCHAR(100) NOT NULL DEFAULT '',
  locked_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at    DATETIME NOT NULL,
  PRIMARY KEY (env_name, module_name),
  INDEX idx_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

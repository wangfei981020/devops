-- Migration 012: pod 失败日志归档
-- 1. 新表 deployment_pod_logs：每条失败的 deploy 模块对应一份 minio 上的归档日志
-- 2. global_config 加 minio 配置（endpoint / bucket / access key / secret 加密 / 保留天数）

CREATE TABLE IF NOT EXISTS deployment_pod_logs (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  deployment_id BIGINT NOT NULL,
  argocd_app    VARCHAR(255) NOT NULL,
  pod_name      VARCHAR(255) NOT NULL,
  container     VARCHAR(128) NOT NULL DEFAULT '',
  object_key    VARCHAR(512) NOT NULL,
  size_bytes    INT NOT NULL DEFAULT 0,
  captured_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_dep (deployment_id),
  INDEX idx_captured (captured_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE global_config ADD COLUMN minio_endpoint VARCHAR(255) NOT NULL DEFAULT '';

ALTER TABLE global_config ADD COLUMN minio_bucket VARCHAR(128) NOT NULL DEFAULT 'deploy-logs';

ALTER TABLE global_config ADD COLUMN minio_access_key VARCHAR(255) NOT NULL DEFAULT '';

ALTER TABLE global_config ADD COLUMN minio_secret_key TEXT;

ALTER TABLE global_config ADD COLUMN minio_region VARCHAR(64) NOT NULL DEFAULT 'us-east-1';

ALTER TABLE global_config ADD COLUMN minio_retention_days INT NOT NULL DEFAULT 90;

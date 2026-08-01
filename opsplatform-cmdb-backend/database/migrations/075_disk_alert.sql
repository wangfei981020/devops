-- 磁盘水位告警（CMDB-012）。
-- 2026-07-31 CMDB 自己的 MySQL 数据盘写满打垮全站，全程没有任何告警——
-- 是人工登进 Pod 执行 df -h 才发现的。这张表用于告警抑制，避免每轮巡检重复推送。
CREATE TABLE IF NOT EXISTS disk_alert_state (
  target      VARCHAR(255) NOT NULL PRIMARY KEY COMMENT '集群 · PVC ns/name 或 节点名',
  level       VARCHAR(16)  NOT NULL COMMENT 'warning / critical',
  pct         DOUBLE       NOT NULL DEFAULT 0,
  notified_at DATETIME     NOT NULL,
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 默认每 30 分钟巡检一次。磁盘从 85% 到写满通常以天计，30 分钟的粒度足够，
-- 又不至于给 Prometheus 增加可观负载。
INSERT IGNORE INTO scheduled_tasks (task_key, name, enabled, schedule) VALUES
  ('disk_watch', '磁盘水位巡检', 1, '*/30 * * * *');

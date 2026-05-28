-- upgrade_events: 真实 GKE 升级事件（来自 GCP Container API Operations）
-- 与 version_history 的区别：
--   version_history: 我们 scraper 观察到的版本变化（监控以来）
--   upgrade_events:  GCP 真实记录的 UPGRADE_MASTER/UPGRADE_NODES 操作（包括监控前的历史）
--
-- 数据策略：
--   - operation_id 是 GCP operation name，全局唯一，作为去重 key
--   - 每次 scrape 调 Operations.List 拉所有 UPGRADE_* 事件，UPSERT
--   - to_version 优先从 operation.detail 文本正则提取（甲）
--   - from_version 从 version_history 配对推断（乙，只能补监控期间的事件）
--   - 监控前的 from_version 留空（前端显示为 "-"）

CREATE TABLE IF NOT EXISTS upgrade_events (
  id              BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id      INT NOT NULL,
  -- 节点池升级时填 pool 名；master 升级填 ''（不用 NULL，方便 UNIQUE KEY）
  nodepool_name   VARCHAR(128) NOT NULL DEFAULT '',
  -- GCP Operation 名，形如 "operation-1716878400-abc"，全局唯一
  operation_id    VARCHAR(128) NOT NULL,
  -- UPGRADE_MASTER / UPGRADE_NODES / CREATE_CLUSTER（用于"初始版本"事件）
  operation_type  VARCHAR(64)  NOT NULL,
  from_version    VARCHAR(64)  NOT NULL DEFAULT '',
  to_version      VARCHAR(64)  NOT NULL DEFAULT '',
  -- 标记 from/to 数据来源：'snapshot'(乙) / 'detail'(甲) / 'empty'(未知)
  from_source     VARCHAR(32)  NOT NULL DEFAULT 'empty',
  to_source       VARCHAR(32)  NOT NULL DEFAULT 'empty',
  status          VARCHAR(32)  NOT NULL,
  started_at      DATETIME     NULL,
  ended_at        DATETIME     NULL,
  raw_detail      VARCHAR(1024) NULL,
  scraped_at      DATETIME     NOT NULL,
  UNIQUE KEY uniq_op (cluster_id, operation_id),
  INDEX idx_lookup (cluster_id, nodepool_name, ended_at),
  FOREIGN KEY (cluster_id) REFERENCES clusters(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

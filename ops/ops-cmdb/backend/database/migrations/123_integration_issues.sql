-- OPSCMDB-044：接入自检结果表。
--
-- 「集群的指标标签值」与「观测端点」是一对，配错会让所有带集群条件的查询
-- 静默返回空。判定逻辑一直有（verifyClusterValue），但只在有人去查那个集群时才跑 ——
-- 生产上挂了多久没人知道，因为没人会逐个点开每个集群去看那个字段。
--
-- 这张表存的是定时任务跑出来的结论，总览页直接读它，不再重跑探测
-- （否则打开首页会去打一圈外部数据源）。
CREATE TABLE IF NOT EXISTS integration_issues (
  id            INT AUTO_INCREMENT PRIMARY KEY,
  tenant_id     INT NOT NULL,
  kind          VARCHAR(32)  NOT NULL COMMENT 'cluster_label=标签值对不上 / no_endpoint=没绑指标源',
  cluster_id    INT          NOT NULL DEFAULT 0,
  cluster_name  VARCHAR(128) NOT NULL DEFAULT '',
  detail        VARCHAR(512) NOT NULL DEFAULT '',
  created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_tenant (tenant_id),
  KEY idx_kind (tenant_id, kind)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 任务本体。默认**启用**：这条检查不打业务系统，只读观测端点的 label values，
-- 代价极低；而它防的是"静默返回空"，恰恰是没人会主动去查的那类问题。
INSERT INTO scheduled_tasks (task_key, name, enabled, schedule)
VALUES ('integration_check', '接入自检（集群标签值 ↔ 观测端点）', 1, '17 */6 * * *')
ON DUPLICATE KEY UPDATE name = VALUES(name);

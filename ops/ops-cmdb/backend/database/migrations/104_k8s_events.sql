-- k8s 事件。
--
-- 为什么要落库而不是每次现查 apiserver：
-- **事件在 etcd 里只保留 1 小时**（默认 --event-ttl=1h）。排障时最想看的
-- 恰恰是"刚才那一下"发生了什么，而等人打开界面时它常常已经没了。
-- 采下来存着，才谈得上"回看"。
--
-- ⚠️ 只存 Warning：Normal 事件量是 Warning 的几十倍（每次拉镜像、每次调度
-- 都有），全存会把库撑爆，而排障时没人看 Normal。
-- 要看全量就去 kubectl，那是实时场景。
CREATE TABLE IF NOT EXISTS k8s_events (
  id          BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id  INT          NOT NULL,
  namespace   VARCHAR(255) NOT NULL DEFAULT '',
  kind        VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '涉及对象的类型：Pod/Node/...',
  obj_name    VARCHAR(255) NOT NULL DEFAULT '' COMMENT '涉及对象名',
  reason      VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'k8s 原值：FailedScheduling/BackOff/...',
  message     VARCHAR(1024) NOT NULL DEFAULT '',
  type        VARCHAR(16)  NOT NULL DEFAULT 'Warning',
  count       INT          NOT NULL DEFAULT 1 COMMENT '同一事件重复次数（k8s 自己聚合的）',
  first_at    DATETIME     NULL,
  last_at     DATETIME     NULL,
  synced_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  tenant_id   INT          NOT NULL DEFAULT 1,
  -- 幂等键：同一个事件被多轮采集重复看到时更新而不是插新行。
  -- 不加这个索引的话，每 5 分钟采一次 = 同一条 FailedScheduling 一天出现 288 行
  UNIQUE KEY uniq_event (cluster_id, namespace, kind, obj_name, reason, first_at),
  KEY idx_cluster_last (cluster_id, last_at),
  KEY idx_reason (reason)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='k8s Warning 事件';

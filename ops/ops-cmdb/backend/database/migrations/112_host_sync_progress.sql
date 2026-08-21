-- 主机同步进度落库。
--
-- # 为什么必须落库
--
-- 原来这份进度只在**发起请求的那个 Pod 的内存里**（handlers/hosts.go 的 hostSyncStore）。
-- 单副本够用；多副本下：A 副本接了 POST 并开始同步，前端下一次轮询被负载均衡
-- 打到 B 副本，B 的那份 map 是空的 → 返回 `started:false, running:false`。
--
-- 这个回答在多副本下是**假的**：同步正跑着，既没有"没开始"也没有"已结束"。
-- 而界面据此判定"不在跑"，于是按钮提前解禁、进度消失 ——
-- 用户看到的就是"点了没反应"，然后再点一次，白烧一遍云厂商配额。
--
-- ⚠️ 与 110_renew_job_progress 是**同一个问题的同一个解法**：那边是批量续费的进度，
-- 这边是主机同步的进度，症状和成因一模一样。新写后台任务时先看这两张表。
--
-- ⚠️ 与 leases 表的跨副本互斥（internal/cluster/mutex.go）是一类问题的两个面：
-- 那个防的是"两个副本同时拉同一个 project"，这个防的是"看不到自己那次跑到哪了"。
--
-- # 为什么不上 Redis
--
-- 协调层已经是 MySQL（leases 表），进度数据量极小（一次同步几十到上千次计数，
-- 且已节流到每秒最多一次写）。为这点量引入一个中间件，等于给每个客户环境
-- 增加一份安装、HA、备份、升级和故障排查的负担 —— 而这是要交付出去的产品。
-- 另外 Redis 默认不持久化，重启即丢，反而解决不了"进程重启后进度消失"。
--
-- 进度是**临时数据**：真正的追溯看 task_runs 和 cloud_account_projects.last_result。
CREATE TABLE IF NOT EXISTS host_sync_progress (
  project_id BIGINT       NOT NULL COMMENT 'cloud_account_projects.id，进度按项目粒度',
  tenant_id  BIGINT       NOT NULL,
  account_id INT          NOT NULL COMMENT '按账号聚合时用',
  project    VARCHAR(128) NOT NULL DEFAULT '' COMMENT '项目显示名，免得读进度还要再 join 一次',
  running    TINYINT      NOT NULL DEFAULT 0,
  -- 实例总数 / 已写入。⚠️ total 要等 ListInstances 返回才有值，
  -- 在那之前两个都是 0：这是"正在拉清单"，不是"0/0 卡住了"，界面要分开说
  total      INT          NOT NULL DEFAULT 0,
  done       INT          NOT NULL DEFAULT 0,
  synced     INT          NOT NULL DEFAULT 0 COMMENT '本轮在用实例数',
  stale      INT          NOT NULL DEFAULT 0 COMMENT '本轮标记为云上已消失的数量',
  err        VARCHAR(500) NOT NULL DEFAULT '',
  started_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  -- ⚠️ 判"是不是还真在跑"要看它：进程被 kill 时 running 会永远停在 1。
  -- 读取侧用 updated_at 超过锁 TTL（30 分钟）就当作已死，与 leases 的过期时间对齐
  updated_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (project_id),
  KEY idx_account (tenant_id, account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='主机同步进度，跨副本可见';

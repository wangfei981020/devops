-- 版本变更事件补上控制面（migration 072 只覆盖了节点）
--
-- 缺口：控制面版本存在 gke_cluster_upgrade.current_master_version，是覆盖式的，
-- 升级前后没有任何痕迹。唯一的控制面历史来自 GCP 的 gke_upgrade_history——
-- 而那份恰恰是最脆弱的：官方保留期约两周且会滚动，
-- 「同一对象两次采集之间连升两次时，可能只留下后一次」。
-- 结果是最该被记住的对象（控制面）反而没有本地兜底。
--
-- 复用 k8s_node_version_events 而不是另开一张表：升级过程看板要把控制面和
-- 各节点池放在同一条时间线上看，分两张表每次查询都得 UNION。
-- 加 scope 列区分，node_name/pool 在 control_plane 行里为空。
--
-- ⚠️ 两种 scope 的 detected_at 精度差一个数量级，用途也不同：
--
--   scope=node           k8s 采集每 120 秒一轮 → 误差 ±2 分钟，可用于算耗时
--   scope=control_plane  gke_upgrade_sync 每 6 小时一轮 → 误差 ±6 小时，
--                        **不能**用来算耗时
--
-- 所以控制面这行的价值不是「升了多久」，而是「这次升级发生过、从哪版到哪版」
-- 永久留底，GCP 把记录滚掉之后仍然查得到。
-- 控制面的**耗时**始终应该取 gke_upgrade_history 的 started_at/ended_at
-- （GCP 给的真实时刻，精确到秒），趁它还在的时候读。

ALTER TABLE k8s_node_version_events
  ADD COLUMN scope VARCHAR(16) NOT NULL DEFAULT 'node'
    COMMENT 'node=节点增删/换版；control_plane=控制面版本变更(detected_at 精度仅 6 小时，不可用于算耗时)'
    AFTER cluster_id,
  ADD KEY idx_scope_time (cluster_id, scope, detected_at);

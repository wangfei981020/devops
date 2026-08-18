-- 磁盘用量采集接入定时任务。
--
-- ⚠️ scheduler.go 里注册了处理函数，还必须在这张表里登记，任务才会真的被调度。
-- 只做其中一半的后果很隐蔽：代码看着有、界面上找不到、也不会报错，
-- 只是那份数据永远不更新。
--
-- 频率比 host_sync（每天 03:00）密得多，因为两者性质不同：
--   host_sync       拉云 API，有配额、变化慢（机器不会一小时换一批）
--   host_disk_usage 查 Prometheus，无配额压力，而磁盘写满是**快速变化**的 ——
--                   每天一次的话，一块盘从 70% 涨到 100% 全程无感知。
-- 每 30 分钟一次，代价是几次 PromQL 查询。

INSERT IGNORE INTO scheduled_tasks (task_key, name, enabled, schedule)
VALUES ('host_disk_usage', '磁盘用量采集（Prometheus）', 1, '*/30 * * * *');

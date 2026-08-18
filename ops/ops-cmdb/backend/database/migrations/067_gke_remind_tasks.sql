-- GKE 阶段 4：升级提醒 + 节点健康预警两个定时任务
--
-- 为什么分成两个任务、且默认分开配群：
--   gke_upgrade_remind 是「日级」的——每天 09:00 汇总一次，内容是计划性的（30 天后要升级）
--   node_health_watch  是「分钟级」的——90 秒一轮，内容是急迫的（3 分钟后节点会被重建）
-- 混在一个群里，日常的升级提醒会被高频告警淹没，紧急告警也会因为噪音被忽略。
-- 群在前端「系统管理 → 定时任务」里各自配置。
--
-- node_health_watch 用 robfig/cron 的 @every 语法：现有 cron.New() 没开 WithSeconds，
-- 标准 5 字段表达式最细只能到 1 分钟，@every 90s 能绕开这个限制。
--
-- ⚠️ node_health_watch 默认关闭（enabled=0）：它会每 90 秒对每个启用集群 list 一次 nodes，
-- 需要用户确认 apiserver 压力可接受、并配好告警群之后再手动打开。

INSERT IGNORE INTO scheduled_tasks (task_key, name, enabled, schedule) VALUES
  ('gke_upgrade_remind', 'GKE 升级提醒(T-30/T-7)', 1, '0 9 * * *'),
  ('node_health_watch',  '节点健康预警(90秒)',      0, '@every 90s');

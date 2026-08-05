-- 清理长期失效主机的定时任务。
--
-- 失效记录（stale=1）此前从不清理，生产上已积 65 条，几乎全是 GKE 节点——
-- 节点池轮换/扩缩容/升级换机时节点销毁重建，名字带随机后缀，只增不减。
--
-- ⚠️ 5 段 cron（调度器用的是标准解析器，6 段会静默不注册，见迁移 089）。
-- 每天 4:00 跑，排在 3:00 的主机同步之后——先同步再清理，
-- 避免拿"同步前的旧状态"做删除判断。
INSERT INTO scheduled_tasks (task_key, name, schedule, enabled)
VALUES ('stale_host_purge', '清理长期失效的主机记录', '0 4 * * *', 1)
ON DUPLICATE KEY UPDATE name=VALUES(name);

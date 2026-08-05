-- 关系边的来源标记 + 自动建边定时任务。
--
-- 关系图谱页一直只有 5 条边（全是申请证书时写的 certificate→domain），
-- 因为除此之外没有任何自动建边机制，要连线只能人工 POST（CMDB-009）。
--
-- ⚠️ origin 这一列是**自动边能否安全重建的前提**：
-- 自动任务每轮重建时要先清掉自己上一轮建的，如果分不清来源，
-- 就会把人手工连的线一起删掉——那是人的判断，删了对方还不知道。
-- 存量数据一律算 manual（它们确实都是手工/证书流程建的）。
ALTER TABLE ci_relations ADD COLUMN origin VARCHAR(16) NOT NULL DEFAULT 'manual';
ALTER TABLE ci_relations ADD KEY idx_origin (origin);

-- 自动建边要给负载均衡建 CI 记录（ci_relations 两端都得是 ci_id，而 LB 原来
-- 完全不在 CI 台账里）。类型必须同时登记进 ci_types——迁移 080 记的就是这个教训：
-- 往 cis 里写了没登记的类型，模型管理页和总览的配置项数会对不上，
-- 差额没人解释得清。
INSERT IGNORE INTO ci_types (code, name, icon, sort_order) VALUES ('loadbalancer', '负载均衡', 'Share', 4);

-- ⚠️ 5 段 cron（调度器用标准解析器，6 段会静默不注册，见迁移 089）。
-- 每天 5:00：排在主机同步(3:00)和失效清理(4:00)之后——
-- 建边依赖主机/LB/解析记录都是当轮最新的，早于它们跑会连出上一轮的旧关系。
INSERT INTO scheduled_tasks (task_key, name, schedule, enabled)
VALUES ('relations_auto_link', '关系图谱自动建边', '0 5 * * *', 1)
ON DUPLICATE KEY UPDATE name=VALUES(name);

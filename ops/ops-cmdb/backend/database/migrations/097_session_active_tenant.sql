-- 会话里记录「当前活跃租户」。
--
-- ⚠️ 列名刻意叫 active_tenant_id 而不是 tenant_id：
--
--	auth_sessions 是**平台级表**（会话属于用户，不属于任何租户）。
--	如果这里也叫 tenant_id，会与隔离列同名，让人以为会话表也按租户隔离——
--	那是错误的理解，会导致有人给它加上隔离过滤，结果所有人都登不进来。
--
--	这个字段的语义是「这个会话现在正在看哪个租户」，是**状态**不是**归属**。
--
-- 0 表示尚未选择（平台管理员登录后未代入任何租户）。
-- 第一期只有默认租户，登录时统一写 1。
ALTER TABLE auth_sessions ADD COLUMN active_tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 0
  COMMENT '当前活跃租户；0=未选择（平台管理员未代入）';

-- 存量会话归入默认租户，避免升级后在线的人全部失去租户上下文。
UPDATE auth_sessions SET active_tenant_id = 1 WHERE active_tenant_id = 0;

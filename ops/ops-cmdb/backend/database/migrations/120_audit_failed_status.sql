-- 修正存量审计记录里被记成 success 的失败动作。
--
-- 🔴 audit_logs.status 的列默认值是 'success'（migration 083），
--    而 WriteAuditAs 一直**不写这一列** —— 于是所有失败类动作
--    （auth.login.failed / auth.portal.failed / auth.portal.denied）
--    在审计页上都是绿色的 success。
--
--    实测本地 308 条审计里就有这样的行：
--        动作 auth.login.failed · 对象「用户名或密码错误」· 结果 ● success
--
--    代价不是难看：**暴力破解在审计上看起来是一串正常登录**。
--    审计的用途就是事后追溯，把失败记成成功等于这段记录是假的。
--
-- 代码侧已由 auditStatusOfAction() 按动作名后缀推导（见 handlers/common.go），
-- 这里只修历史数据。
--
-- ⚠️ 只改 status，不动 action/target —— 那两列记录的是"当时发生了什么"，
--    是证据，任何情况下都不该被后来的迁移改写。
UPDATE audit_logs
   SET status = 'fail'
 WHERE status = 'success'
   AND (action LIKE '%.failed' OR action LIKE '%.denied' OR action LIKE '%.error');

-- 新增任务：从注册商 API 同步域名到期日
--
-- 原有的 refresh_expiry 走 RDAP/WHOIS，有两个硬伤：
--   1. 续费后不会立刻反映——WHOIS 是注册局公开数据，往往滞后几小时到一两天。
--      刚续完费看台账还是旧到期日，容易被当成"续费没成功"而重复操作（真金白银）。
--   2. 很多 TLD 的 WHOIS 对频繁查询直接拒绝，一批域名刷不出来。
--
-- 注册商 API 给的是它自己账本上的到期日，续费当场生效，一次请求拿回全部域名。
-- 两个任务分工：本任务管数据源里的域名，WHOIS 那个兜底手工录入的域名。
INSERT IGNORE INTO scheduled_tasks (task_key, name, schedule, enabled)
VALUES ('registrar_expiry_sync', '域名到期同步（注册商 API）', '0 30 3 * * *', 1);


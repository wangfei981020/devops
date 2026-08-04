-- users 的唯一键从 username 改成 (username, auth_source)
--
-- 起因：运维平台里也有 admin，SSO 登录时要建一条 auth_source='portal' 的影子记录，
-- 撞上本地 admin 的 UNIQUE(username) → 报"创建用户失败"，该用户永远登不进来。
--
-- 为什么不复用同名的本地账号：本地账号在权限中间件里是**全放行**的兜底身份
-- （运维平台不可用时的入口）。让 SSO 用户挂到那条记录上，等于谁在运维平台
-- 叫 admin 谁就拿到 CMDB 的完整权限——直接提权。
-- 两条记录必须彼此独立：同名不同源 = 两个不同的人。
ALTER TABLE users DROP INDEX username;
ALTER TABLE users ADD UNIQUE KEY uk_user_source (username, auth_source);

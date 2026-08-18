-- 会话里记下角色。
--
-- 为什么需要：portal_auth 把 role 传给了 issueSession，却没有落库，
-- 于是鉴权中间件只能靠 auth_source 判断身份，`is_admin` 被写成
-- `auth_source == "local"`——也就是**通过运维平台单点登录的超管不算管理员**。
--
-- 叠加上 portal_auth 里"admin 跳过拉权限"那段，结果是 SSO 超管的会话
-- 权限为空且不被豁免：前端菜单全隐、后端每个接口 403，整个人被锁死在
-- 「无权访问」页上。生产上就是这么坏的。
ALTER TABLE auth_sessions ADD COLUMN role VARCHAR(32) NOT NULL DEFAULT '' AFTER auth_source;

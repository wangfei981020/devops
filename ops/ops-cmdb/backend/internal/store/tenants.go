package store

// ActiveTenantsQuery 列出所有可用租户的标准查询。
//
// # 为什么要单独定成一个常量
//
// 这条查询原本在两处各写了一遍，都写成了 `WHERE enabled=1` ——
// 而 tenants 表根本没有 enabled 列（用的是 status + deleted_at 软删）。
// 结果：定时任务的自愈、成本快照对**所有租户都不执行**，
// 而现象只有一行日志，功能就那么静静地不工作。
//
// 列名写错是编译器抓不到的（SQL 是字符串），
// 没有真库的单测也抓不到。收敛成一处之后，
// 至少「写错」只会错一个地方，且 schema 测试能盯住它。
//
//	deleted_at IS NULL 不能省：软删的租户不该再跑定时任务。
const ActiveTenantsQuery = `SELECT id FROM tenants WHERE status = 'active' AND deleted_at IS NULL ORDER BY id`

// SeedAdminTenantQuery 把 admin 挂到默认租户下（幂等）。
//
//	同样收进这里、由真库测试覆盖：这条 SQL 的第一版把列名写成了 role
//	（实际是 role_code），后果是每个接口都 403「当前账号不属于该租户」。
//	和 ActiveTenantsQuery 的 enabled 是同一类错 —— 猜列名，
//	编译器不管，没有真库的测试也不管。
const SeedAdminTenantQuery = `INSERT IGNORE INTO user_tenants (user_id, tenant_id, role_code)
	SELECT u.id, t.id, 'admin' FROM users u
	JOIN tenants t ON t.status='active' AND t.deleted_at IS NULL
	WHERE u.username='admin'
	ORDER BY t.id LIMIT 1`

// SeedUserTenantQuery 把某个用户挂到默认租户下（幂等，参数为 user_id）。
//
//	和 SeedAdminTenantQuery 同一类：登录时 resolveTenant 查不到归属会**回落**
//	到默认租户并照常发会话，而请求时的租户中间件按 user_tenants 严格校验 ——
//	两边判据不一致，结果是账号登得进去、每个接口 403。
//	角色给 'member'：租户内的普通成员，具体能干什么由 users.role_code 决定。
const SeedUserTenantQuery = `INSERT IGNORE INTO user_tenants (user_id, tenant_id, role_code)
	SELECT ?, t.id, 'member' FROM tenants t
	WHERE t.status='active' AND t.deleted_at IS NULL
	ORDER BY t.id LIMIT 1`

// BackfillLocalUserTenantsQuery 给所有缺归属的本地账号补上默认租户（幂等）。
//
//	⚠️ 必须每次启动都跑，理由和 EnsureAdminTenant 一样：
//	写在"首次初始化"分支里的补数据逻辑，对已经存在的安装永远不会执行 ——
//	全新部署一切正常，升级上来的库里那些账号继续每个接口 403。
//	只补 auth_source='local'：SSO 影子账号的归属该由对接方决定，不能替它猜。
const BackfillLocalUserTenantsQuery = `INSERT IGNORE INTO user_tenants (user_id, tenant_id, role_code)
	SELECT u.id, (SELECT t.id FROM tenants t
	              WHERE t.status='active' AND t.deleted_at IS NULL
	              ORDER BY t.id LIMIT 1), 'member'
	FROM users u
	WHERE u.auth_source='local'
	  AND NOT EXISTS (SELECT 1 FROM user_tenants ut WHERE ut.user_id = u.id)`

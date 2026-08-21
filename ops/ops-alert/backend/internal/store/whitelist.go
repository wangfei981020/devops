package store

// platformTables 平台级表白名单 —— 这些表**不带 tenant_id**，跨租户共享。
//
// # 增加一条的门槛
//
// 往这里加表等于宣布「这份数据所有租户共享」。加错一条就是跨租户泄露，
// 且不会有任何报错 —— 数据就那么静静地被别的租户看见了。
//
// 判断标准：问一句「租户 A 的管理员看到这行数据，合理吗？」
//   - 用户表：合理（用户是平台级实体，归属关系在 user_tenants）
//   - 规则、事件、数据源：不合理 → 必须带 tenant_id
//
// 拿不准就**不要**加进来。带 tenant_id 的代价只是多一个字段，
// 漏带的代价是把 A 公司的生产日志给 B 公司看。
var platformTables = map[string]bool{
	// ── 租户与身份 ──
	"tenants":       true, // 租户本身
	"users":         true, // 用户是平台级实体，归属在 user_tenants
	"user_tenants":  true, // 用户 × 租户 × 角色
	"auth_sessions": true, // 会话属于用户

	// ── 角色与权限 ──
	// 角色定义对所有租户是同一份：内置角色由迁移种下，权限码的真相在
	// internal/api/perm.go 的映射表里。给它们加 tenant_id 会变成
	// 每个租户各有一套同名角色，而权限码是全局的，两者对不上。
	"roles":            true,
	"role_permissions": true,

	// ── 授权 ──
	"licenses": true, // 授权是给整套部署的，不是给某个租户的
	// 安装指纹标识「这套部署」，与租户无关。带 tenant_id 会让每个租户
	// 算出不同指纹，而签发方只签一个 —— 表现为部分租户提示未授权。
	"install_identity": true,
	// SSO 配置与登录态是平台级的：身份源接的是这套部署，不是某个租户。
	// oidc_states 存库而不是内存 —— 多副本下发起登录和处理回调
	// 不一定是同一个副本，存内存会表现为"有时候登录成功有时候 state 不匹配"
	"oidc_config": true,
	"oidc_states": true,

	// ── 系统 ──
	// leases 协调的是**进程**不是数据：「谁在跑检测引擎」对所有租户是同一个答案。
	// 给它加 tenant_id 就变成每个租户各选一个持有者，等于失去这张表的意义。
	"leases":            true,
	"schema_migrations": true,

	// ⚠️ 下面这些看起来像平台级，实际不是，别加进来：
	//
	//	brand_settings  带 tenant_id：0=平台默认品牌，非 0=租户白标
	//	audit_logs      带 tenant_id：平台操作用 0，租户操作用租户 ID
	//	silences        带 tenant_id：静默只在本租户生效
	//	notifiers       带 tenant_id：渠道里有各租户自己的 webhook 与密钥
}

// IsPlatformTable 供测试与 CI 检查使用。
func IsPlatformTable(name string) bool { return platformTables[name] }

// PlatformTableNames 返回白名单快照（测试用，顺序不保证）。
func PlatformTableNames() []string {
	out := make([]string, 0, len(platformTables))
	for k := range platformTables {
		out = append(out, k)
	}
	return out
}

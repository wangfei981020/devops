package store

// platformTables 平台级表白名单 —— 这些表**不带 tenant_id**，跨租户共享。
//
// # 增加一条的门槛
//
// 往这里加表等于宣布「这份数据所有租户共享」。加错一条就是跨租户泄露，
// 且不会有任何报错 —— 数据就那么静静地被别的租户看见了。
// **新增必须经过评审，并说明为什么这份数据不属于任何租户。**
//
// # 判断标准
//
// 问一句：「租户 A 的管理员看到这行数据，合理吗？」
//   - 用户表：合理（用户是平台级实体，通过 user_tenants 归属租户）
//   - 应用表：不合理 → 必须带 tenant_id
//
// 拿不准就**不要**加。带 tenant_id 的代价只是多一个字段，漏带的代价是数据泄露。
var platformTables = map[string]bool{
	// ── 租户与身份 ──
	"tenants":       true, // 租户本身
	"users":         true, // 用户是平台级实体，归属关系在 user_tenants
	"user_tenants":  true, // 用户 × 租户 × 角色
	"auth_sessions": true, // 会话属于用户
	"idp_configs":   true, // 上游身份源配置（整套系统一份）
	// 口令与应急口令属于「人」，不属于租户：同一个人在多个租户里不该有多套口令。
	// 2026-08-07 补登记 —— 漏登记时表现为「口令永远不对」，白名单确实拦住了。
	"local_credentials": true,
	"break_glass_codes": true,
	// 二次验证密钥与登录中间态同理：绑在人/一次流程上，不属于任何租户。
	// （step_up_tickets 反而是租户级的 —— 票据是"在某租户的某应用上提权"）
	"mfa_secrets":     true,
	"idp_auth_states": true,
	// OIDC 签名密钥是整套系统一套（下游用同一个 JWKS 验签），不属于任何租户
	"oidc_signing_keys": true,

	// ── 授权（产品许可，不是访问授权）──
	"licenses": true, // 许可是给整套系统的，不是给某个租户的

	// ── 系统 ──
	"schema_migrations": true,

	// ⚠️ 下面这些**看起来像**平台级，实际不是，别加进来：
	//
	//	apps               带 tenant_id：应用属于租户
	//	app_groups         带 tenant_id：分组是租户自己定义的
	//	access_policies    带 tenant_id：授权规则绝对是租户级的 ——
	//	                   这张表要是错放进白名单，等于一个租户的授权规则
	//	                   作用到了所有租户，是本系统最严重的一种事故
	//	user_groups        带 tenant_id：用户组由租户维护
	//	departments        带 tenant_id
	//	audit_logs         带 tenant_id：平台操作用 0，租户操作用租户 ID
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

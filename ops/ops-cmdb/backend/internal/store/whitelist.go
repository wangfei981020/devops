package store

// platformTables 平台级表白名单 —— 这些表**不带 tenant_id**，跨租户共享。
//
// # 增加一条的门槛
//
// 往这里加表等于宣布「这份数据所有租户共享」。加错一条就是跨租户泄露，
// 且不会有任何报错 —— 数据就那么静静地被别的租户看见了。
//
// 所以：**新增必须经过评审，并在 PR 里说明为什么这份数据不属于任何租户。**
// CI 会检查这个 map 的变更是否伴随对应的测试。
//
// # 判断标准
//
// 问一句：「租户 A 的管理员看到这行数据，合理吗？」
//   - 用户表：合理（用户是平台级实体，通过 user_tenants 归属租户）
//   - 主机表：不合理 → 必须带 tenant_id
//
// 拿不准就**不要**加进来。带 tenant_id 的代价只是多一个字段，
// 而漏带的代价是数据泄露。
var platformTables = map[string]bool{
	// ── 租户与身份 ──
	"tenants":            true, // 租户本身
	"users":              true, // 用户是平台级实体，归属关系在 user_tenants
	"user_tenants":       true, // 用户 × 租户 × 角色
	"auth_sessions":      true, // 会话属于用户
	"idp_configs":        true, // 身份提供商配置（整套系统一份）
	"idp_group_mappings": true, // 组 → 租户+角色 映射规则

	// ── 授权 ──
	"licenses": true, // 授权是给整套系统的，不是给某个租户的
	// 安装指纹的输入之一。它标识的是「这套部署」，与租户无关 ——
	// 带 tenant_id 会让每个租户算出不同的指纹，而签发方只签一个。
	"install_identity": true,

	// ── 系统 ──
	//
	//	leases 协调的是**进程**不是数据：「谁是 leader」对所有租户是同一个答案。
	//	要是给它加 tenant_id，就变成每个租户各选一个 leader，
	//	那等于没约束住"任务只跑一次"——加了字段，却失去了这张表存在的意义。
	"leases":            true, // 多副本租约
	"schema_migrations": true, // 迁移记录
	"ci_types":          true, // 配置项类型字典，全局统一

	// ⚠️ 下面这些**看起来像**平台级，实际不是，别加进来：
	//
	//	settings            带 tenant_id：0=平台级设置，非 0=租户级设置，租户级覆盖平台级
	//	local_roles         带 tenant_id：0=内置角色模板（只读），租户可基于模板建自己的角色
	//	environments        带 tenant_id：环境定义各租户可自行配置
	//	lifecycle_statuses  带 tenant_id：生命周期状态字典各租户可配
	//	audit_logs          带 tenant_id：平台操作用 0，租户操作用租户 ID
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

// Package license 是本产品对共享授权内核 licensekit 的薄封装。
//
// 共享库眼里 feature 就是字符串 —— 它不该知道任何产品有什么功能。
// 但产品内部需要类型安全：手滑写错一个字符串常量，编译期就该报错，
// 而不是等到运行时静默返回 false（那表现为"买了却没生效"，极难排查）。
//
// 本包只做两件事：定义本产品的 Feature / Capacity / implemented 表；
// 把类型安全的调用转成共享库的字符串调用。
// 状态机、验签、指纹、宽限期全在 licensekit 里，不重复实现。
package license

import (
	"sort"

	"ops-kit/licensekit"
)

// ProductID 与 license 里 products 的 key 一致。
// 改它等于让所有已发出的 license 对本产品失效。
//
// 2026-08 随目录搬迁从 "ops-data-plane" 改为 "ops-cmdb"，与 LICENSING.md §13.1 一致。
// **现在改是代价最小的时刻**：还没有任何 license 发出去。
// 若等到有客户之后再改，每一张已发出的 token 都会因为查不到自己的 key
// 而落到 not_licensed —— 客户买了却用不了，且提示是"授权不含本产品"，最难排查。
const ProductID = "ops-cmdb"

// Feature 是可被单独授权的功能键。
//
// CE（无 license / license 无效）拥有的能力**不经过 Has()** —— 它们永远可用：
// 单集群 K8s 只读、主机、域名、证书、基础 RBAC/审计、全链路拓扑、MCP 基础工具集。
//
// ⚠️ 全链路拓扑刻意放进 CE：它是最强的获客钩子，
// 客户得先看见这个能力才会考虑买 EE。留一手反而卖不动。
type Feature string

const (
	// FeatureMultiCloud 多云 connector（阿里云 / AWS / 腾讯云…）。CE 只有单一云。
	FeatureMultiCloud Feature = "multi_cloud"
	// FeatureVersionUpgrade K8s 版本与升级风险管理：到期预警、排期、预案。
	FeatureVersionUpgrade Feature = "version_upgrade"
	// FeatureCost 成本归因、闲置识别、容量水位。
	FeatureCost Feature = "cost"
	// FeatureExposure 暴露面与合规：公网暴露、高危端口、基线检查、等保报表。
	FeatureExposure Feature = "exposure"
	// FeatureMCPFull MCP 完整工具集与 AI 集成。CE 只给基础只读工具尝鲜。
	FeatureMCPFull Feature = "mcp_full"
	// FeatureSSO 标准 OIDC / SAML 单点登录与目录同步。
	FeatureSSO Feature = "sso"
	// FeatureMultiTenant 多个隔离单元。
	//
	// ⚠️ 数据层从第一天就按租户隔离，但产品上默认单租户。
	// 这个 feature 控制的是「能不能创建第二个租户」，不是「隔离生不生效」。
	FeatureMultiTenant Feature = "multi_tenant"
	// FeatureBranding 白标：产品名、logo、主色。
	FeatureBranding Feature = "branding"
	// FeatureAuditRollback 审计回滚：把配置变更退回上一版本。
	FeatureAuditRollback Feature = "audit_rollback"
)

// 容量项的键名。与 license 里 capacity 的 key 一致。
//
// CapNodes 是**主计价维度**（见定价方案）。其余是防滥用上限，
// 不作为主要计价依据 —— 两个主维度会让客户算不清账。
const (
	CapNodes         = "nodes"
	CapClusters      = "clusters"
	CapCloudAccounts = "cloud_accounts"
	CapSeats         = "seats"
	CapTenants       = "tenants"
	CapRetentionDays = "retention_days"
)

// allFeatures 完整目录 —— 用于校验 license 里的 feature 字符串是否合法。
// 出现在这里不代表已经实现，见 implemented。
var allFeatures = map[Feature]bool{
	FeatureMultiCloud:     true,
	FeatureVersionUpgrade: true,
	FeatureCost:           true,
	FeatureExposure:       true,
	FeatureMCPFull:        true,
	FeatureSSO:            true,
	FeatureMultiTenant:    true,
	FeatureBranding:       true,
	FeatureAuditRollback:  true,
}

// implemented 已经有代码在跑的功能。
//
// 一个 feature 签进 license 但不在这里，Has() 返回 false ——
// 防「卖了但没做」：宁可客户来问为什么没生效，也不能让界面出现点不动的功能。
// 反过来，功能上线时忘了加进这里就是「买了却不给用」，
// 所以**上线检查清单里必须有这一条**。
//
// 当前处于重构期，绝大多数能力还在旧系统里，这里保持诚实的空 ——
// 重构到哪一步，就点亮哪一个。
var implemented = map[string]bool{
	// 下面三个已有接口在跑，并已在 guard.go 的 featureRoutes 里挂上门控。
	// 点亮与挂门控必须**同时**发生：
	//   只点亮不挂门控 = 白送（接口不问 Has()，谁都能用）
	//   只挂门控不点亮 = 全关（Has() 恒 false，已购客户也被挡在外面）
	// TestFeatureRoutesAreImplemented 会在两者不一致时失败。
	string(FeatureCost):           true,
	string(FeatureVersionUpgrade): true,
	string(FeatureAuditRollback):  true,

	// FeatureMCPFull 的门控**不在 featureRoutes 里**，是工具级的
	// （见下方说明与 handlers/mcp.go）。所以它不参与
	// TestFeatureRoutesAreImplemented 的配对检查，
	// 点亮的依据是：mcp.go 里确实在调 Has(FeatureMCPFull)。
	string(FeatureMCPFull): true,
	// SSO 接入（OIDC）已实现：迁移 106-108 + handlers/idp*.go，
	// 门控挂在改配置的写接口上（登录链路刻意不挡，见 guard.go）
	string(FeatureSSO): true,

	// FeatureExposure 暴露面与合规。
	//
	// 🔴 它是第一个**纯只读**的付费功能，门控挂在 guard.go 的 featureReadPrefixes。
	//	原来这里注释着"没有独立写接口，等阶段 7"—— 而那期间侧栏已经按
	//	features_missing 打了 EE 标记，界面上标着 EE、接口却 200 返回全量
	//	（生产实测，OPSCMDB-072）。**标了却不拦比不标更糟：它让人以为分档生效了。**
	string(FeatureExposure): true,

	// FeatureBranding：前端已完成，后端保存未接 —— 等阶段 2 接完再点亮
	// FeatureMultiTenant：阶段 2（还没有 tenants 的写接口）
	// FeatureMCPFull 已点亮（见上）。它的门控是**工具级**而非路由级：
	//   单挂一条 /api/mcp 会把 CE 的基础工具集也一起关掉，
	//   而 CE 的 MCP 本来就该能用。
	// 判定在 handlers/mcp.go，按 mcpTool.EE 逐个来：没买时 EE 工具
	// **不出现在 tools/list 里**（列出来再拒绝的话，AI 会把它当故障反复重试），
	// 直接按名字硬调才回一句说清"是授权没包含，不是故障"。
	// FeatureMultiCloud：待重构
}

// plans 档次 → 功能。**这张表在二进制里，不在 license 里。**
//
// 正因如此，加新功能只要改这里随版本发布，老客户升级即得，一个字节都不用重签。
// 若把功能清单直接签进 license，每加一个功能就要给所有已购客户重签一遍。
//
// ⚠️ 分档是**商业决策**，这里是初稿，定价定下来前不要当成结论。
// 分档逻辑：
//
//	standard      进公司的门槛（SSO 与审计是采购的硬性要求，没有就进不了招标）
//	professional  用得深（成本归因、升级治理、暴露面——运维真正每天在看的）
//	enterprise    规模化与集成（多云、多租户、白标、完整 MCP）
//
// ⚠️ 出现在这里 ≠ 已实现。实现与否由 implemented 单独把关（§2 三道防线）。
var plans = map[string][]string{
	"standard": {
		string(FeatureSSO),
		string(FeatureAuditRollback),
	},
	"professional": {
		string(FeatureSSO),
		string(FeatureAuditRollback),
		string(FeatureCost),
		string(FeatureVersionUpgrade),
		string(FeatureExposure),
	},
	"enterprise": {
		string(FeatureSSO),
		string(FeatureAuditRollback),
		string(FeatureCost),
		string(FeatureVersionUpgrade),
		string(FeatureExposure),
		string(FeatureMultiCloud),
		string(FeatureMultiTenant),
		string(FeatureBranding),
		string(FeatureMCPFull),
	},
}

// ceLimits 未激活时的默认上限。0 表示不限。
//
// 给得不小气：CE 的目的是获客，卡得太死只会让人第一天就放弃。
// 真正的付费驱动是功能（多云、升级治理、成本归因、完整 MCP），
// 不是把人卡在很少的节点数上。
var ceLimits = map[string]int64{
	CapNodes:         100,
	CapClusters:      3,
	CapCloudAccounts: 2,
	CapSeats:         20,
	CapTenants:       1,
	CapRetentionDays: 30,
}

// IsKnownFeature 校验 feature 字符串是否在目录里（签发工具与激活时用）。
func IsKnownFeature(f Feature) bool { return allFeatures[f] }

// AllFeatures 完整功能目录，**按名字排序**。
//
// 排序不是为了好看：状态接口把它逐个 Has() 后返回给前端，
// 顺序随 map 遍历变化的话，同一份授权每次刷新返回的数组顺序都不同 ——
// 界面上的功能列表会跳来跳去，看起来像状态在变。
func AllFeatures() []Feature {
	out := make([]Feature, 0, len(allFeatures))
	for f := range allFeatures {
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// 从共享库再导出，让调用方不必同时 import 两个包。
type (
	Status   = licensekit.Status
	Payload  = licensekit.Payload
	Licensee = licensekit.Licensee
	Exceeded = licensekit.Exceeded
)

// ⚠️ 共享库新增状态时，这里必须同步补一行。
//
// 漏掉的后果不报错：业务代码里 `switch status` 少一个 case，
// 那个状态就掉进未处理分支——而它恰恰是最少见、最晚才出现的状态
// （lapsed 要欠费满 30 天才会出现），等撞上时已经在客户环境里了。
//
// 到期三段（LICENSING.md §5）：
//
//	到期日 ──14天── expired ──30天── lapsed
//	  grace 功能照常   只读·续上即恢复   只读·需重新采购
//
// lapsed **不是更严的锁**：和 expired 一样只读、数据一条不删。
// 差别只在话术，以及产品可据此停掉昂贵的后台作业（采集、拨测），
// 因为欠费一个月的实例大概率没人在看了。
const (
	StatusActive       = licensekit.StatusActive
	StatusGrace        = licensekit.StatusGrace
	StatusExpired      = licensekit.StatusExpired
	StatusLapsed       = licensekit.StatusLapsed
	StatusFingerprint  = licensekit.StatusFingerprint
	StatusNotActivated = licensekit.StatusNotActivated
	StatusNotLicensed  = licensekit.StatusNotLicensed

	// GraceDays 从 30 改成了 14：宽限期越长越会被当成到期日本身
	// （给 30 天，续费谈判就从第 31 天才开始）。
	// 解决"采购慢"的是提前提醒（RemindBeforeDays），不是把闸门往后挪。
	GraceDays            = licensekit.GraceDays
	LapsedDays           = licensekit.LapsedDays
	RemindBeforeDays     = licensekit.RemindBeforeDays
	FingerprintGraceDays = licensekit.FingerprintGraceDays
)

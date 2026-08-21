// Package license 是 OpsAlert 对共享授权内核 licensekit 的薄封装。
//
// 共享库眼里 feature 就是字符串 —— 它不该知道任何产品有什么功能。
// 但产品内部需要类型安全：手滑写错一个常量，编译期就该报错，
// 而不是运行时静默返回 false（那表现为「买了却没生效」，极难排查）。
//
// 本包只定义本产品的 Feature / Plans / Implemented，并把类型安全的调用
// 转成共享库的字符串调用。状态机、验签、指纹、宽限期全在 licensekit 里。
package license

import (
	"sort"

	"ops-kit/licensekit"
)

// ProductID 与 license 里 products 的 key 一致。
//
// ⚠️ 改它等于让所有已发出的 license 对本产品失效 —— 客户买了却用不了，
// 而提示是「授权不含本产品」，最难排查。现在还没发出任何 license，是改的最后时机。
const ProductID = "ops-alert"

// Feature 是可被单独授权的功能键。
//
// CE（无 license / license 无效）拥有的能力**不经过 Has()**，永远可用：
//
//	日志关键词检测、单数据源、事件与判定链、基础通知渠道、
//	场景模板、基础 RBAC 与审计、MCP 只读基础工具。
//
// ⚠️ 场景模板与判定链刻意留在 CE：它们是这个产品最强的获客钩子。
// 试用者要先体验到「建规则不用写 LogQL」和「能看清为什么告警」，才会考虑买 EE。
// 留一手反而卖不动。
type Feature string

const (
	// FeatureMultiDatasource 多数据源。CE 只允许一个。
	// 单数据源足够试用，但真实环境必然是多集群多环境。
	FeatureMultiDatasource Feature = "multi_datasource"

	// FeatureAdvancedDetect 高级检测类型：日志量突变、字段阈值/分位数。
	// CE 只有关键词与缺失两类。
	FeatureAdvancedDetect Feature = "advanced_detect"

	// FeatureNoise 降噪治理：关联合并、抑制规则、噪音榜与调参建议。
	// 这是规模化之后才会痛的能力 —— 十条规则不需要治理，六十条就需要。
	//
	// ⚠️ **临时静默不在这里**，它属于 CE。
	// 实测门控时发现自己把 /silences 也拦了：那等于 CE 用户收得到告警、
	// 却压不住刷屏，最后只能关掉整个通知渠道。EE 卖的是"少收到不该收的"，
	// 不是"能不能让一条告警闭嘴"。
	FeatureNoise Feature = "noise"

	// FeatureBacktest 回放实验室：拿历史数据验证改动，噪音预算。
	FeatureBacktest Feature = "backtest"

	// FeatureMCPFull MCP 完整工具集与 AI 集成。CE 只给只读基础工具尝鲜。
	FeatureMCPFull Feature = "mcp_full"

	// FeatureSSO 标准 OIDC / SAML 单点登录。
	FeatureSSO Feature = "sso"

	// FeatureMultiTenant 多租户隔离。
	FeatureMultiTenant Feature = "multi_tenant"

	// FeatureBranding 白标：品牌色、logo、产品名。
	FeatureBranding Feature = "branding"
)

// Capacity 是可被授权限制的容量键。
type Capacity string

const (
	CapRules       Capacity = "rules"
	CapDatasources Capacity = "datasources"
	CapUsers       Capacity = "users"
)

// plans 档次 → 功能清单。**这张表在二进制里，不在 license 里** ——
// 这正是「加新功能不用重签」的原因：新功能加进这张表随版本发布，老客户升级即得。
//
// 对应 license 里的 `features: ["plan:enterprise"]`。
var plans = map[string][]string{
	// 标准版：解决「规则多了之后」的问题
	"standard": {
		string(FeatureMultiDatasource),
		string(FeatureAdvancedDetect),
		string(FeatureBacktest),
	},
	// 企业版：再加上治理与集成
	"enterprise": {
		string(FeatureMultiDatasource),
		string(FeatureAdvancedDetect),
		string(FeatureBacktest),
		string(FeatureNoise),
		string(FeatureMCPFull),
		string(FeatureSSO),
		string(FeatureMultiTenant),
		string(FeatureBranding),
	},
}

// implemented 是**已经有代码在跑**的 feature。
//
// ⚠️ 签进 license 但不在这里 → Has() 返回 false（防「卖了但没做」）。
// ⚠️ 反过来，功能上线时忘了加进这里 → 「买了却不给用」，而且不报错。
//
//	上线检查清单里必须有这一条（LICENSING.md §2）。
var implemented = map[string]bool{
	string(FeatureMultiDatasource): true,
	string(FeatureAdvancedDetect):  true, // log_spike / log_field_threshold 已实现
	string(FeatureNoise):           true, // 静默、抑制、关联、噪音榜
	string(FeatureBacktest):        true, // 回放实验室
	string(FeatureMCPFull):         true, // 10 个 MCP 工具
	// ⚠️ 下面这些**还没有代码**，故意不写 true：
	//    sso / multi_tenant / branding
	// 写 true 会让客户买了之后看到一个点不动的开关。
}

// ceLimits 未激活（社区版）时的容量上限。0 = 不限。
//
// # 为什么给得这么宽
//
// 这套系统会交付给关联子公司自建部署。容量卡死会直接挡住正常使用
// （多集群就要多数据源、接了 SSO 就会有很多账号），
// 而**付费驱动是功能不是数量** —— 具体说是 MCP / AI 接入。
//
// ⚠️ 早先这里是 rules:20 / datasources:1 / users:5，
//
//	注释写着"单数据源是 CE 与 EE 的主要分界"。那条判断已经**不成立**了，
//	别照着旧注释推断意图。
//
// ⚠️ users 必须跟着放开：接了 OIDC 之后每个登录的人都会 JIT 建号，
//
//	5 个上限意味着第 6 个人登录就失败 ——「接 SSO」和「最多 5 个用户」
//	不能共存，而失败现场看起来像是 SSO 配错了。
var ceLimits = map[string]int64{
	string(CapRules):       100,
	string(CapDatasources): 100,
	string(CapUsers):       100,
}

// Options 组装给 licensekit 的产品声明。
func Options() licensekit.Options {
	return licensekit.Options{
		Product:     ProductID,
		Implemented: implemented,
		CELimits:    ceLimits,
		// ⚠️ 告警平台的 CE **必须可写**：建不了规则等于一条告警都收不到，
		// 试用者第一分钟就走了。这与 CMDB（只读台账也有价值）是相反的取向。
		CEWritable: true,
		Plans:      plans,
	}
}

// AllFeatures 排序后的全部 feature，给授权页展示用。
func AllFeatures() []Feature {
	out := []Feature{
		FeatureMultiDatasource, FeatureAdvancedDetect, FeatureNoise, FeatureBacktest,
		FeatureMCPFull, FeatureSSO, FeatureMultiTenant, FeatureBranding,
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Implemented 该 feature 是否已经有代码在跑。授权页要能区分
// 「没买」和「买了但这版还没做」—— 两者的下一步完全不同。
func Implemented(f Feature) bool { return implemented[string(f)] }

// CELimits 未激活时的容量上限。给界面用 —— 让"当前上限是多少"这件事
// 有一个可查的来源，而不是让人去翻代码或猜。
func CELimits() map[string]int64 {
	out := make(map[string]int64, len(ceLimits))
	for k, v := range ceLimits {
		out[k] = v
	}
	return out
}

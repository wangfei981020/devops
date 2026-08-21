// Package license 是本产品对共享授权内核 ops-kit/licensekit 的薄封装。
//
// 验签、指纹、有效期与宽限期、七态状态机全在 licensekit 里，本包不重复实现。
// 这里只做三件事：定义本产品的 Feature / 容量项 / implemented 与 plans 表；
// 把类型安全的调用转成共享库的字符串调用；把状态渲染成 /system/info 要的形状。
//
// # 为什么不再自己实现
//
// 本包原先自带一整套（自己 Parse、自己 Evaluate、自己算指纹），与内核分叉在三处：
//
//	宽限期     30 天 vs 内核 14 天
//	指纹算法   sha256("oap-install|"+@@server_uuid) vs 内核 HMAC(install_uuid|@@server_id)
//	指纹宽限   从签发日起算 vs 内核从「首次发现不匹配」起算
//
// 指纹算法不同这条最要命：同一套环境里两个产品会算出两个不同的指纹，
// 而 LICENSING §4 要求整份 license 只有一个 install_id ——
// 也就是说签发方按 CMDB 报上来的指纹签一张，本产品必然对不上。
//
// 还有一条安全上的：原实现的验签公钥读 OAP_LICENSE_PUBKEY 环境变量，
// 改一个环境变量就能换成自己的公钥，然后用自己的私钥签任意 license。
// 内核用编译进二进制的常量，运维改不出也伪造不出。
package license

import (
	"sort"

	"ops-kit/licensekit"
)

// ProductID 与 license 里 products 的 key 一致。
// 改它等于让所有已发出的 license 对本产品失效。
//
// ⚠️ **刻意与目录名不一致**（目录是 ops-sso，标识是 ops-access-plane）。
//
// §13.1 的先例是「标识跟目录走」（ops-cmdb 就是那么改的），这里是个例外，
// 两个理由：
//
//  1. 产品定位是**访问控制平面**，不是"第二个 IdP"。叫 ops-sso 会把
//     对外能力说小，而这个字符串会出现在客户的 license 和合同附件里。
//  2. enterprise/ops-access-plane 是本目录的另一份拷贝（准备停用）。
//     标识保持一致，将来两棵树合并时不需要给任何人重签。
//
// 现在没有任何 license 发出去，改与不改都零成本 —— 一旦有客户，
// 改它意味着每一张已发出的 token 都查不到自己的 key，落到 not_licensed。
const ProductID = "ops-access-plane"

// Feature 本产品的功能枚举。
//
// ⚠️ 新增一项必须同时决定它进不进 implemented —— 卖了没做的功能，
// 客户付完钱当天就会发现，那是最伤信任的一种事。
type Feature string

const (
	FeatureGatewayConnect Feature = "gateway_connect" // 网关代管（零改造接入）
	FeatureFormFill       Feature = "form_fill"       // 表单注入
	FeaturePathPolicy     Feature = "path_policy"     // 接口级策略
	FeatureStepUpMFA      Feature = "step_up_mfa"     // 二次验证与提权票据
	FeatureBreakGlass     Feature = "break_glass"     // 应急通道
	FeatureTamperAudit    Feature = "tamper_audit"    // 防篡改审计与取证包
	FeatureBranding       Feature = "branding"        // 白标
	FeatureMultiTenant    Feature = "multi_tenant"    // 多租户托管
	FeatureSCIM           Feature = "scim"            // SCIM 出站供应
)

// allFeatures 完整目录。出现在这里不代表已实现，见 implemented。
var allFeatures = []Feature{
	FeatureGatewayConnect, FeatureFormFill, FeaturePathPolicy, FeatureStepUpMFA,
	FeatureBreakGlass, FeatureTamperAudit, FeatureBranding, FeatureMultiTenant, FeatureSCIM,
}

// implemented 这个二进制里**真正实现了**的功能。
//
// license 给了但没实现的，/system/info 报 not_implemented 而不是 not_granted ——
// 把没做的功能说成"未购买"，客户付完钱当天就会发现。
var implemented = map[string]bool{
	string(FeatureGatewayConnect): true,
	string(FeatureFormFill):       true,
	string(FeaturePathPolicy):     true,
	string(FeatureStepUpMFA):      true,
	string(FeatureBreakGlass):     true,
	string(FeatureTamperAudit):    true,
	// FeatureBranding：未做
	// FeatureMultiTenant：表结构就位，租户切换与代入未做
	// FeatureSCIM：未做
}

// plans 档次 → 功能。**这张表在二进制里，不在 license 里。**
//
// 正因如此，加新功能只要改这里随版本发布，老客户升级即得，一个字节都不用重签。
// 若把功能清单直接签进 license，每加一个功能就要给所有已购客户重签一遍。
//
// ⚠️ 分档是商业决策，这里是初稿，定价定下来前不要当成结论。
var plans = map[string][]string{
	"standard": {
		string(FeatureGatewayConnect),
		string(FeaturePathPolicy),
	},
	"professional": {
		string(FeatureGatewayConnect),
		string(FeaturePathPolicy),
		string(FeatureFormFill),
		string(FeatureStepUpMFA),
		string(FeatureBreakGlass),
	},
	"enterprise": {
		string(FeatureGatewayConnect),
		string(FeaturePathPolicy),
		string(FeatureFormFill),
		string(FeatureStepUpMFA),
		string(FeatureBreakGlass),
		string(FeatureTamperAudit),
		string(FeatureBranding),
		string(FeatureMultiTenant),
		string(FeatureSCIM),
	},
}

// 容量项的键名。与 license 里 capacity 的 key 一致。
const (
	CapSeats        = "seats"
	CapApplications = "applications" // 接入的应用（网关代管的站点）数
	CapTenants      = "tenants"
)

// ceLimits 未激活（社区版）时的容量上限。0 表示不限。
//
// 给得不小气：CE 的目的是获客，卡得太死只会让人第一天就放弃。
var ceLimits = map[string]int64{
	CapSeats:        20,
	CapApplications: 3,
	CapTenants:      1,
}

// Implemented 该功能在当前二进制里是否已实现。
func Implemented(f Feature) bool { return implemented[string(f)] }

// AllFeatures 完整功能目录，**按名字排序**。
//
// 排序不是为了好看：/system/info 把它逐个判定后返回给前端，
// 顺序随 map 遍历变化的话，同一份授权每次刷新返回的数组顺序都不同 ——
// 界面上的功能列表会跳来跳去，看起来像状态在变。
func AllFeatures() []Feature {
	out := append([]Feature(nil), allFeatures...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// 从共享库再导出，让调用方不必同时 import 两个包。
type (
	Status       = licensekit.Status
	Payload      = licensekit.Payload
	ProductGrant = licensekit.ProductGrant
)

// GraceDays 到期后的宽限天数。
//
// ⚠️ 语义与本产品的旧实现**不同**，切换内核时一并对齐了：
//
//	旧：到期即转只读，"宽限 30 天"指的是只读但不停服
//	新：到期后 14 天内功能完全不受影响（只挂横幅），超出才转只读
//
// 采用后者是因为 LICENSING §5 就是这么定的，而且"过期第二天就写不了"
// 会把一次续费流程延误直接变成客户的生产事故。
const GraceDays = licensekit.GraceDays

const (
	StatusActive       = licensekit.StatusActive
	StatusGrace        = licensekit.StatusGrace
	StatusExpired      = licensekit.StatusExpired
	StatusLapsed       = licensekit.StatusLapsed
	StatusFingerprint  = licensekit.StatusFingerprint
	StatusNotActivated = licensekit.StatusNotActivated
	StatusNotLicensed  = licensekit.StatusNotLicensed
)

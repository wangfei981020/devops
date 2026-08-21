package licensekit

import "time"

// Licensee 授权主体：买这套系统的法人实体。
// 激活时登记，签进 license，客户不可自行修改。
//
// 与「租户」是两个概念：授权主体只有一个（谁买的），
// 租户可以有很多个（这套系统里分了几个隔离单元）。
type Licensee struct {
	Org       string `json:"org"`        // 组织名称：页脚、关于页、导出报表署名
	TaxID     string `json:"tax_id"`     // 统一社会信用代码：唯一标识，防转授
	Contact   string `json:"contact"`    // 授权联系人
	Email     string `json:"email"`      // 到期提醒
	ScopeName string `json:"scope_name"` // 授权范围：生产 / 测试 / 试用
}

// ProductGrant 一个产品的授权内容。
//
// Features 与 Capacity 都用通用类型（字符串 / map），因为**共享库不该知道
// 每个产品有什么功能**。产品侧把它们映射回自己的枚举与结构。
// 这样加一个新产品不需要改本包。
type ProductGrant struct {
	Features []string         `json:"features"`
	Capacity map[string]int64 `json:"capacity"` // 0 或缺省表示不限
}

// Has 判断某个 feature 字符串是否在授权列表里。
// 产品侧不要直接调它 —— 走 Manager.Has()，那里还会检查状态与 implemented 表。
func (g ProductGrant) Has(feature string) bool {
	for _, f := range g.Features {
		if f == feature {
			return true
		}
	}
	return false
}

// Limit 取某项容量上限，不存在返回 0（不限）。
func (g ProductGrant) Limit(item string) int64 { return g.Capacity[item] }

// Payload 是被签名的内容。**字段一旦发出就不能改语义。**
type Payload struct {
	LicenseID string   `json:"license_id"`
	Licensee  Licensee `json:"licensee"`

	// Products 多产品授权。key 是产品标识，如 "ops-data-plane"。
	//
	// 客户只买了一个产品时，这里就只有一个条目；
	// 另一个产品装上去查不到自己的 key，显示"未授权" —— 这是正确行为。
	Products map[string]ProductGrant `json:"products"`

	// InstallID 安装指纹，绑定本次安装。
	//
	// ⚠️ 整份 license 一个，不是每产品一个 —— 同一套环境里装多个产品，
	// 指纹应当一致（同一个数据库实例）。客户若把产品装在不同库上，
	// 需要签发两份 license，签发工具必须支持同一 licensee 签多份不同指纹。
	InstallID string `json:"install_id"`

	IssuedAt time.Time `json:"issued_at"`

	// ExpiresAt 整份 license 一个到期日。
	//
	// 刻意不做"产品 A 到期而 B 没到期" —— 那会让状态机复杂一倍，
	// 而实际采购里多产品通常同一个合同周期。真有需求就签两份。
	//
	// ⚠️ Perpetual 为 true 时本字段被忽略，**不要**用零值表达永久（见下）。
	ExpiresAt time.Time `json:"expires_at"`

	// Perpetual 永久授权，为 true 时忽略 ExpiresAt，永不过期。
	//
	// # 为什么必须是独立字段，不能用 ExpiresAt 的零值
	//
	// 零值同时是"没填"和"永久"两个意思，而这两件事后果完全相反——
	// 一个该拒绝，一个该永远放行。CONVENTIONS §3.3 禁止哨兵值就是这个道理。
	//
	// 这里还有过一个实际的坑：本字段加进来之前，evaluate 用
	// `now.Before(p.ExpiresAt)` 判定，零值时求值为 false、宽限判断也 false，
	// 于是**签一张不填到期日的 license，客户装上去显示「已过期」**，
	// 而签发方以为自己签了永久。加显式字段之后这条路被堵死：
	// 想要永久必须明确写 true，写不写到期日都不影响结论。
	//
	// ⚠️ 永久 license 是唯一我们自己也收不回的东西（离线不可吊销），
	// 所以 LICENSING.md §3 规定**永久必须绑定 InstallID**，
	// 且不允许与"可移植"同时成立。这条约束在签发侧强制，共享库不拦——
	// 因为已经签出去的 token 再拦也没意义，只会把老客户锁在外面。
	Perpetual bool `json:"perpetual"`

	// EntitledUntil 维护期：能拿到**哪一批**新功能。
	//
	// 与 ExpiresAt 分工：
	//
	//	ExpiresAt     功能还能不能用       到了转只读
	//	EntitledUntil 能拿到哪一批新功能   到了之后已有功能照常，新功能不再展开
	//
	// 永久 license 配一个有限的 EntitledUntil 是最常见的卖法：
	// 客户买断当前功能集，后续新功能要续维护费。
	//
	// 零值 = 不限制（见 applyEntitlement 的注释，兼容没有此字段的老 token）。
	EntitledUntil time.Time `json:"entitled_until"`
}

// Expired 判定到期与否。**永久授权永远返回 false。**
//
// 抽成方法而不是散在 evaluate 里，是因为"永久要短路"这件事
// 在到期判定、宽限期判定两处都要用；写两遍就会有一处忘了改。
func (p *Payload) Expired(now time.Time) bool {
	if p == nil {
		return true
	}
	if p.Perpetual {
		return false
	}
	return !now.Before(p.ExpiresAt)
}

// GraceDeadline 宽限期截止时刻，以及该截止是否有意义。
//
// 永久授权没有"过了期还宽限几天"这回事，返回 ok=false，调用方据此短路。
func (p *Payload) GraceDeadline(days int) (time.Time, bool) {
	if p == nil || p.Perpetual {
		return time.Time{}, false
	}
	return p.ExpiresAt.AddDate(0, 0, days), true
}

// Grant 取某产品的授权。产品不在 license 里时返回零值与 false。
func (p *Payload) Grant(product string) (ProductGrant, bool) {
	if p == nil || p.Products == nil {
		return ProductGrant{}, false
	}
	g, ok := p.Products[product]
	return g, ok
}

// Status 当前授权状态。
type Status string

const (
	StatusActive       Status = "active"        // 正常
	StatusGrace        Status = "grace"         // 过期但在宽限期内，功能不受影响
	StatusExpired      Status = "expired"       // 超出宽限期，转只读
	StatusFingerprint  Status = "finger_mismat" // 指纹不匹配（换库/迁移），进入宽限
	StatusNotActivated Status = "not_activated" // 未激活
	StatusNotLicensed  Status = "not_licensed"  // license 有效，但不含本产品
	StatusLapsed       Status = "lapsed"        // 过期太久，续期已不足以恢复，需重新采购
)

// GraceDays 过期后的宽限天数：这期间功能完全不受影响，只在顶部挂横幅。
//
// 14 天对齐 GitLab（自管版到期后 14 天宽限，第 15 天 00:00 UTC 转只读）。
//
// 早先取过 30 天，理由是"客户走内部采购流程要时间"。改回 14 的原因：
// **宽限期越长，它就越会被当成到期日本身**——给 30 天，续费谈判就从第 31 天才开始。
// 真正解决"采购流程慢"的是提前提醒（见 RemindBeforeDays），不是把闸门往后挪。
const GraceDays = 14

// LapsedDays 从**到期日**起算，超过这么多天就转 StatusLapsed。
//
// 同样对齐 GitLab：过期超过 30 天，续期不足以恢复，需重新采购。
//
// ⚠️ 注意它是从到期日算的，不是从宽限期结束算的，所以三段是：
//
//	0 ~ 14 天    grace    功能照常，挂横幅
//	14 ~ 30 天   expired  只读，**续上就恢复**
//	30 天以上     lapsed   只读，文案改为"需重新采购"
//
// ⚠️ **lapsed 不是更严的锁**——它和 expired 一样是只读，数据一条不删。
// 差别只在两处：给用户的话术不同；产品可以据此停掉昂贵的后台作业
// （比如采集、拨测），因为一个欠费一个月的实例大概率没人在看了。
// 把它做成"更严的锁"是错的：客户续费回来发现数据没了，那是砸招牌。
const LapsedDays = 30

// RemindBeforeDays 到期前多少天开始在界面上提醒。
//
// 这才是"采购流程慢"的正解：提前一个月开始提示，客户有充足时间走流程，
// 而不是等过期之后靠宽限期兜。
const RemindBeforeDays = 30

// FingerprintGraceDays 指纹不匹配后的宽限天数。
//
// 触发场景是客户把库恢复到了新实例、或主从切换到了新机器 ——
// 那通常发生在故障处理期间。此时把系统降级，等于在故障上再加一层故障。
// 14 天足够走完重新激活流程。
//
// ⚠️ 这条必须配 runbook：客户半夜恢复完数据库发现系统降级，
// 得能自己查到"为什么"和"怎么办"。
const FingerprintGraceDays = 14

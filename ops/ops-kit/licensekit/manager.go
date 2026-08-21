package licensekit

import (
	"sync/atomic"
	"time"
)

// Manager 持有当前生效的授权，供全进程查询。**每个产品实例化一个**，
// 构造时传入自己的产品标识，之后所有查询都自动落在该产品的授权上。
//
// 用原子指针而不是加锁：Has() 在每个受控接口上都会被调用，热路径不能有锁竞争。
// 激活时整体换一个新的 state 指针。
//
// ⚠️ 多副本下的收敛：本进程激活后其他副本仍是旧状态。
// 单副本时可以先不处理；**扩容前必须补上跨副本广播**，
// 否则会出现"激活了但一半请求仍报未授权"这种极难排查的现象。
type Manager struct {
	product      string
	state        atomic.Pointer[state]
	now          func() time.Time // 便于测试注入
	ceLimits     map[string]int64
	supported    map[string]bool // 该产品已实现的 feature
	ceWritable   bool
	plans        map[string][]string  // 档次 → 功能清单
	since        map[string]time.Time // feature → 引入日期，用于维护期裁剪
	onWatchError func(error)
}

type state struct {
	payload  *Payload
	grant    ProductGrant
	hasGrant bool
	validSig bool
	// features 是 grant.Features 展开并按维护期裁剪后的结果。
	// 在 Load 时算一次而不是每次 Has() 现算：Has() 挂在每个受控接口上，
	// 是热路径；而展开结果只随 Load 变 —— 它不依赖"现在几点"。
	features map[string]bool

	// fingerprint 与 mismatchSince 是 evaluate 的两个外部输入，随 Load 存下来。
	//
	// ⚠️ 这里存的是**判定的输入**，不是判定的结果。早先存的是算好的 status，
	// 那是错的：状态是时间的函数 —— 到期、宽限期结束、超期转 lapsed
	// 都会在没有任何一次 Load 发生的情况下自行到来。
	// 而重新 Load 只有两条路（进程启动、Watch 发现 revision 变了），
	// revision 取的是 license 行的 updated_at，它只有激活时才变。
	// 于是一个长期运行的进程会把 active 一直保持到重启为止：
	// 界面上 DaysUntilExpiry 是实时算的，显示"已过期 20 天"，
	// 而门控仍然放行写操作 —— 授权页看起来完全正常，正因如此没人发现。
	fingerprint   string
	mismatchSince time.Time

	loadedAt time.Time
}

// Options 产品接入时提供自己的那部分信息。
type Options struct {
	// Product 产品标识，与 license 里 products 的 key 一致，如 "ops-data-plane"。
	Product string

	// Implemented 该产品**已经有代码在跑**的 feature。
	//
	// 一个 feature 签进 license 但不在这里，Has() 返回 false ——
	// 防的是「卖了但没做」：宁可客户来问为什么没生效，
	// 也不能让界面出现点不动的功能。
	//
	// 反过来，功能上线时忘了加进这里，就是「买了却不给用」，
	// 所以上线检查清单里必须有这一条。
	Implemented map[string]bool

	// CELimits 未激活（社区版）时的容量上限。0 表示不限。
	//
	// 给得不小气：CE 的目的是获客，卡得太死只会让人第一天就放弃。
	// 真正的付费驱动是功能，不是把人卡在很小的数量上。
	CELimits map[string]int64

	// CEWritable 决定未激活（社区版）时能不能写。**默认 false**。
	//
	// 这是**产品级定位**，不是共享库能统一决定的事：
	//
	//	只读 CE 有价值的产品   资产台账、成本报表 —— 光看就有用
	//	只读 CE 无意义的产品   告警平台 —— 建不了规则等于一条告警都收不到，
	//	                      试用者第一分钟就走了
	//
	// 保持默认 false 意味着现有产品行为一字不变；需要可写 CE 的产品显式打开，
	// 于是"这个产品的社区版是什么形态"在它自己的 Options 里一眼可见，
	// 不用翻共享库去猜。
	//
	// ⚠️ 打开它只影响 ReadOnly()。功能仍由 Has() 逐项判定、容量仍受 CELimits 限制 ——
	// 可写不等于免费版全给。
	CEWritable bool

	// Plans 档次 → 功能清单，对应 license 里的 `plan:<档次>` 声明。
	//
	// 这张表在**二进制里**，不在 license 里 —— 这正是"加新功能不用重签"的原因：
	// 新功能加进这张表随版本发布，老客户升级即得。
	//
	// 留空则 `plan:` 声明展开为空集（不是全给），见 resolveFeatures 的注释。
	Plans map[string][]string

	// FeatureSince feature → 引入日期，配合 Payload.EntitledUntil 裁剪新功能。
	//
	// 查不到的 feature 视为"一直就有"，一律放行。
	// 只有卖永久 / 长周期 license 时才需要维护这张表；
	// 全是年付订阅的产品可以留空。
	FeatureSince map[string]time.Time

	// OnWatchError Watch 轮询出错时的回调（通常接到产品的日志里）。
	//
	// 留空则**静默吞掉**——而"授权同步坏了但没人知道"正是最难查的那类问题：
	// 表现是某个副本的状态永远停在旧值。接 Watch 的产品都该填上它。
	OnWatchError func(error)

	// Now 注入时间源。留空用 time.Now。
	//
	// 存在的唯一理由是测试 —— 宽限期、指纹宽限这些逻辑必须能验，
	// 而它们依赖"现在几点"。通过 Options 传比导出一个 SetNow 函数干净：
	// 后者会永久留在公共 API 里，且没法阻止有人在生产代码里调它。
	Now func() time.Time
}

// NewManager 创建一个未激活的管理器。
func NewManager(opt Options) *Manager {
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	m := &Manager{
		product:      opt.Product,
		now:          now,
		ceLimits:     opt.CELimits,
		supported:    opt.Implemented,
		ceWritable:   opt.CEWritable,
		plans:        opt.Plans,
		since:        opt.FeatureSince,
		onWatchError: opt.OnWatchError,
	}
	// 空 state：payload 为 nil，statusOf 据此判出 not_activated。
	// 不预存一个 status 字段 —— 状态一律现算，见 state 的注释。
	m.state.Store(&state{})
	return m
}

// Load 装载一份**已验签通过**的授权。验签在 verify.go 里做，这里只管状态判定。
//
// ⚠️ 用这个入口时，指纹不匹配的宽限期按「到期日 + 14 天」算，
// 于是一张一年期的 license 被复制到别的环境，**整整一年都是可用的**
// （状态是 finger_mismat，但功能照常）——长周期 license 上等于没绑定。
// 新产品一律用 LoadAt，把「第一次发现不匹配的时刻」传进来。
func (m *Manager) Load(p *Payload, currentFingerprint string) {
	m.LoadAt(p, currentFingerprint, time.Time{})
}

// LoadAt 装载授权，并告知**本机第一次发现指纹不匹配的时刻**。
//
// # 为什么这个时刻必须由产品提供
//
// 宽限期的本意是「客户刚恢复完数据库，别在故障上再加一层故障」，
// 所以它该从**发现不匹配那一刻**起算。而 licensekit 是无状态的：
// 进程重启后它不知道这次不匹配是刚发生的还是三个月前就有了。
// 若在这里用 now() 现取，重启一次宽限期就重置一次，等于永久宽限。
//
// 所以产品要在自己的库里存一列（跟着 license token 一起存最自然）：
//
//	读：Load 之前取出来传进来
//	写：发现 Status()==StatusFingerprint 且库里还没有值时，记下当前时间
//
// # 传零值会怎样
//
// 退回旧规则（从到期日起算），也就是上面 Load 说的那个宽松行为。
// 这是为了不误伤还没改造的产品——**但它不安全**，
// 接入检查清单里必须确认这一列已经加上（LICENSING.md §11）。
func (m *Manager) LoadAt(p *Payload, currentFingerprint string, mismatchSince time.Time) {
	st := &state{
		payload:       p,
		validSig:      true,
		fingerprint:   currentFingerprint,
		mismatchSince: mismatchSince,
		loadedAt:      m.now(),
	}
	st.grant, st.hasGrant = p.Grant(m.product)
	if st.hasGrant {
		// 只缓存与时间无关的那一半：features 的展开与维护期裁剪只随 Load 变。
		// 状态本身不在这里算，见 state 的注释。
		st.features = applyEntitlement(
			resolveFeatures(st.grant.Features, m.plans), m.since, p.EntitledUntil)
	}
	m.state.Store(st)
}

// Clear 回到未激活状态（用于解绑或验签失败）。
func (m *Manager) Clear() {
	m.state.Store(&state{})
}

// mismatchGraceDeadline 指纹不匹配的宽限截止时刻。
//
// 三条分支，优先级从严到宽：
//
//	永久授权          没有宽限。它靠绑定指纹限制影响面（LICENSING.md §3），
//	                 给宽限等于把绑定取消了；且零值到期日算出来是公元 1 年，
//	                 拿它做算术只会得到由零值决定的荒谬结论。
//	知道首次发现时刻   从那一刻起算 14 天 —— 这才是"给客户时间去重新激活"的本意
//	不知道（零值）     退回旧规则，从到期日起算。宽松，仅为兼容未改造的产品。
func mismatchGraceDeadline(p *Payload, mismatchSince time.Time) (time.Time, bool) {
	if p.Perpetual {
		return time.Time{}, false
	}
	if !mismatchSince.IsZero() {
		return mismatchSince.AddDate(0, 0, FingerprintGraceDays), true
	}
	return p.GraceDeadline(FingerprintGraceDays)
}

// evaluate 判定当前状态。
//
// 顺序有讲究：
//  1. 指纹优先于过期 —— 客户恢复数据库后可能既换了指纹又临近到期，
//     先提示指纹更贴近真实原因，客户才知道该做什么。
//  2. 「不含本产品」是独立状态，不能和「未激活」混为一谈 ——
//     客户买了 A 装了 B，界面得说清楚是"没买这个产品"，
//     而不是让他去反复检查激活码对不对。
func (m *Manager) evaluate(p *Payload, hasGrant bool, fingerprint string, mismatchSince time.Time) Status {
	if p == nil {
		return StatusNotActivated
	}
	now := m.now()
	// ⚠️ 这里**没有** `fingerprint != ""` 这个条件，是刻意的。
	//
	// 早先有。后果是：产品算不出指纹时（数据库读不到 install_uuid / server_id）
	// 传一个空串进来，整个指纹校验就被**静默跳过**，状态照常 active。
	// 而两个产品恰好就是这么用的 —— 出错时 `return ""`，日志还写着
	// "授权将按指纹不匹配处理"，与实际行为完全相反，排障的人会被带偏。
	//
	// 可利用性也在：把 install_uuid 那行记录删掉，指纹绑定就永久失效，
	// 一张绑着别人指纹的 license 从此在任何环境都能跑。
	//
	// 现在空指纹按**不匹配**处理，也就是进 14 天宽限期而不是直接只读：
	// 算不出指纹通常意味着数据库出了问题，那种时刻把系统降级
	// 等于在故障上再加一层故障。宽限期内功能照常，但状态是 finger_mismat，
	// 界面有提示、日志有记录 —— 关键是它不再是**静默**的。
	if p.InstallID != "" && p.InstallID != fingerprint {
		if deadline, ok := mismatchGraceDeadline(p, mismatchSince); ok && now.Before(deadline) {
			return StatusFingerprint
		}
		return StatusExpired
	}
	if !hasGrant {
		return StatusNotLicensed
	}
	if !p.Expired(now) {
		return StatusActive
	}
	if deadline, ok := p.GraceDeadline(GraceDays); ok && now.Before(deadline) {
		return StatusGrace
	}
	// 过期太久：续期已不足以恢复，需重新采购（对齐 GitLab 的 30 天）。
	// 注意 LapsedDays 从**到期日**起算，不是从宽限期结束起算。
	if deadline, ok := p.GraceDeadline(LapsedDays); ok && !now.Before(deadline) {
		return StatusLapsed
	}
	return StatusExpired
}

// statusOf 现算某个快照在**此刻**的状态。
//
// 每次查询都重新求值，而不是取 Load 时算好的结果 —— 状态是时间的函数，
// 详见 state 的注释。代价是一次 time.Now 加几次比较，
// 相对于它挂载的那些接口可以忽略；而缓存它换来的是"到期了却不降级"。
func (m *Manager) statusOf(st *state) Status {
	if st == nil {
		return StatusNotActivated
	}
	return m.evaluate(st.payload, st.hasGrant, st.fingerprint, st.mismatchSince)
}

// DaysUntilExpiry 距到期还有几天。负数表示已过期几天。
//
// ok=false 的两种情况：永久授权、未装载授权 —— 两者都没有"还剩几天"可言。
//
// 界面据此渲染提醒横幅。**提前提醒才是"客户采购流程慢"的正解**，
// 靠拉长宽限期解决的话，只会让宽限期本身被当成新的到期日。
func (m *Manager) DaysUntilExpiry() (int, bool) {
	st := m.state.Load()
	if st == nil || st.payload == nil || st.payload.Perpetual {
		return 0, false
	}
	// 按自然日取整而不是 24 小时块：用户看到的"还剩 1 天"应当和日历一致，
	// 否则会出现"昨天还显示剩 1 天，今天就过期了"这种看起来像 bug 的正常现象。
	exp := st.payload.ExpiresAt.Truncate(24 * time.Hour)
	today := m.now().Truncate(24 * time.Hour)
	return int(exp.Sub(today).Hours() / 24), true
}

// ShouldRemind 是否该在界面上提示到期。
//
// 已过期也返回 true —— 过期后的横幅比过期前更该显示。
func (m *Manager) ShouldRemind() bool {
	days, ok := m.DaysUntilExpiry()
	return ok && days <= RemindBeforeDays
}

// functional 当前状态下功能是否照常可用（宽限期算可用 —— 过期不停服）。
func functional(s Status) bool {
	return s == StatusActive || s == StatusGrace || s == StatusFingerprint
}

// Has 判定某个功能是否可用。**这是整个门控的唯一入口。**
//
// 逐项判定，任一不满足即 false：
//   - 授权无效 / 超出宽限 / 不含本产品
//   - feature 不在 license 的 Features 列表里
//   - feature 尚未实现（宁可"买了没生效"，不可"界面上有个点不动的按钮"）
func (m *Manager) Has(feature string) bool {
	st := m.state.Load()
	if st == nil || st.payload == nil || !st.validSig || !st.hasGrant {
		return false
	}
	if !functional(m.statusOf(st)) {
		return false
	}
	if m.supported != nil && !m.supported[feature] {
		return false
	}
	// 用 Load 时展开好的集合，而不是再读一遍 grant.Features ——
	// 后者拿到的是未展开的原始声明，`plan:enterprise` 会被当成一个
	// 名叫 "plan:enterprise" 的功能，于是所有按档次授权的 license 全部失效。
	return st.features[feature]
}

// Granted 该功能是否在 license 里**授予**了，不看实现与否、也不看当前状态。
//
// 与 Has() 的分工：Has 回答"能不能用"，Granted 回答"买没买"。
// 产品的系统信息页要同时知道这两件事，才能把
//
//	没做（not_implemented）／没买（not_granted）／授权失效（license_inactive）
//
// 三种情况分开说。压成一个布尔的话，界面只能显示"不可用"，
// 而这三种的下一步完全不同：等版本、找销售、去续费。
// 尤其不能把"没做"说成"没买"—— 客户付完钱当天就会发现。
//
// ⚠️ 不要用它做门控。门控只有 Has() 一个入口。
func (m *Manager) Granted(feature string) bool {
	st := m.state.Load()
	if st == nil || !st.hasGrant {
		return false
	}
	return st.features[feature]
}

// ReadOnly 是否已降级为只读。
//
// 只读的确切含义（会被写进合同附件，不要随意改）：
//   - 不能发起写操作、不能新增对象
//   - 已有数据可查看、可导出、**可回滚**（回滚是止损手段，不能锁）
//   - 数据一条不删
func (m *Manager) ReadOnly() bool {
	st := m.state.Load()
	if st == nil {
		return true
	}
	s := m.statusOf(st)
	// 社区版是否可写由产品自己声明，见 Options.CEWritable。
	// 注意只放开"未激活"这一种：过期、超宽限、装了没买的产品仍然只读。
	if s == StatusNotActivated && m.ceWritable {
		return false
	}
	return !functional(s)
}

// Status 当前状态，供系统信息页与顶部横幅使用。
func (m *Manager) Status() Status {
	return m.statusOf(m.state.Load())
}

// Payload 当前授权内容。未激活时返回 nil。
func (m *Manager) Payload() *Payload {
	if st := m.state.Load(); st != nil {
		return st.payload
	}
	return nil
}

// Limit 取某项容量上限。未激活或不含本产品时返回 CE 默认值。0 表示不限。
//
// ⚠️ 这些值**同时是业务参数**：分页上限、并发闸门、保留期裁剪都读同一份，
// 所以把校验 patch 掉的人会得到一个"上限为 0"的系统，功能会自己坏掉，
// 而不是"免费解锁"。
func (m *Manager) Limit(item string) int64 {
	st := m.state.Load()
	if st == nil || !st.hasGrant || !functional(m.statusOf(st)) {
		return m.ceLimits[item]
	}
	if v, ok := st.grant.Capacity[item]; ok {
		return v
	}
	return m.ceLimits[item]
}

// Exceeded 描述一项超限。**只用于提示，调用方不得据此阻断操作。**
type Exceeded struct {
	Item    string `json:"item"`
	Current int64  `json:"current"`
	Limit   int64  `json:"limit"`
	// Unlimited 当前用量是**无限**，不是 Current 那个具体数字。
	//
	// 🔴 为什么不能只靠 Current 表示：
	//
	//	"无限"参与比较时得用一个足够大的数（哨兵），而那个数会被**原样渲染**到
	//	界面和 API 上 —— 生产实测拿到的是 `{"current": 999999, "limit": 3650}`，
	//	而同一个响应的 usage 里写着 `retention_unlimited: true`（OPSCMDB-073）。
	//	同一个概念在同一份响应里出现两种编码，读的人只能判断"其中一处是 bug"。
	//
	// ⚠️ 判定本身是对的（永不清理确实超过任何有限保留期上限），
	//
	//	错的是**把哨兵值当成事实值发出去**。带上这一位，调用方就能显示
	//	「永不清理」而不是一个看着像 bug 的魔数。
	Unlimited bool `json:"unlimited,omitempty"`
}

// CheckCapacity 核对用量，返回所有超限项。
//
// 返回非空**不代表要拒绝操作** —— 它只该被渲染成提示，并计入续购报价。
// 客户临时扩容结果系统罢工，是会丢客户的设计。
func (m *Manager) CheckCapacity(usage map[string]int64) []Exceeded {
	return m.CheckCapacityEx(usage, nil)
}

// CheckCapacityEx 同 CheckCapacity，另外声明**哪些项的用量是无限的**。
//
// unlimited[item]=true 时，usage[item] 里放的是比较用的哨兵值，
// 结果里会标上 Unlimited 让调用方按"无限"渲染，而不是把哨兵数字显示出去。
func (m *Manager) CheckCapacityEx(usage map[string]int64, unlimited map[string]bool) []Exceeded {
	var out []Exceeded
	for item, cur := range usage {
		if lim := m.Limit(item); lim > 0 && cur > lim {
			out = append(out, Exceeded{Item: item, Current: cur, Limit: lim,
				Unlimited: unlimited[item]})
		}
	}
	return out
}

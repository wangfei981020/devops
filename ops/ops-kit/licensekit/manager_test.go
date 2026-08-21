package licensekit

import (
	"testing"
	"time"
)

const (
	prodA = "ops-data-plane"
	prodB = "ops-release-plane"
)

func newTestManager(now time.Time) *Manager {
	m := NewManager(Options{
		Product: prodA,
		Implemented: map[string]bool{
			"sso":      true,
			"branding": true,
			// "cost" 故意不实现 —— 用来验证「卖了但没做」不会放行
		},
		CELimits: map[string]int64{"nodes": 100, "seats": 10},
	})
	m.now = func() time.Time { return now }
	return m
}

func payloadFor(products map[string]ProductGrant, expires time.Time, installID string) *Payload {
	return &Payload{
		LicenseID: "LIC-TEST",
		Licensee:  Licensee{Org: "测试组织"},
		Products:  products,
		InstallID: installID,
		IssuedAt:  expires.AddDate(-1, 0, 0),
		ExpiresAt: expires,
	}
}

// 未激活时：功能全关、只读、走 CE 容量。
func TestNotActivated(t *testing.T) {
	m := newTestManager(time.Now())
	if m.Status() != StatusNotActivated {
		t.Fatalf("status = %v, want not_activated", m.Status())
	}
	if m.Has("sso") {
		t.Error("未激活不该有 sso")
	}
	if !m.ReadOnly() {
		t.Error("未激活应为只读")
	}
	if got := m.Limit("nodes"); got != 100 {
		t.Errorf("CE nodes 上限 = %d, want 100", got)
	}
}

// ★ 核心：逐项判定，不是"有效 license = 全给"。
func TestHasIsPerFeature(t *testing.T) {
	now := time.Now()
	m := newTestManager(now)
	m.Load(payloadFor(map[string]ProductGrant{
		prodA: {Features: []string{"sso"}, Capacity: map[string]int64{"nodes": 1000}},
	}, now.AddDate(1, 0, 0), ""), "")

	if !m.Has("sso") {
		t.Error("授权了 sso 却判为无")
	}
	if m.Has("branding") {
		t.Error("没授权 branding 却判为有 —— 说明退化成了「全给」")
	}
}

// 签了但没实现的 feature 必须返回 false（宁可「买了没生效」，不可界面上有点不动的按钮）。
func TestSoldButNotImplemented(t *testing.T) {
	now := time.Now()
	m := newTestManager(now)
	m.Load(payloadFor(map[string]ProductGrant{
		prodA: {Features: []string{"cost"}},
	}, now.AddDate(1, 0, 0), ""), "")

	if m.Has("cost") {
		t.Error("cost 未实现，必须返回 false")
	}
}

// ★ 多产品：只买了 B，装 A 时应报「不含本产品」，而不是「未激活」。
func TestProductNotInLicense(t *testing.T) {
	now := time.Now()
	m := newTestManager(now)
	m.Load(payloadFor(map[string]ProductGrant{
		prodB: {Features: []string{"sso"}},
	}, now.AddDate(1, 0, 0), ""), "")

	if m.Status() != StatusNotLicensed {
		t.Errorf("status = %v, want not_licensed —— 客户要能区分「没买这个产品」和「激活码不对」", m.Status())
	}
	if m.Has("sso") {
		t.Error("本产品未授权，不该有任何 feature")
	}
	if !m.ReadOnly() {
		t.Error("未授权本产品应为只读")
	}
}

// 一份 license 覆盖两个产品，各自拿到各自的授权。
func TestMultiProduct(t *testing.T) {
	now := time.Now()
	p := payloadFor(map[string]ProductGrant{
		prodA: {Features: []string{"sso"}, Capacity: map[string]int64{"nodes": 1000}},
		prodB: {Features: []string{"approval"}, Capacity: map[string]int64{"applications": 200}},
	}, now.AddDate(1, 0, 0), "")

	a := newTestManager(now)
	a.Load(p, "")
	if !a.Has("sso") || a.Limit("nodes") != 1000 {
		t.Error("产品 A 没拿到自己的授权")
	}

	b := NewManager(Options{Product: prodB, Implemented: map[string]bool{"approval": true}})
	b.now = func() time.Time { return now }
	b.Load(p, "")
	if !b.Has("approval") {
		t.Error("产品 B 没拿到自己的授权")
	}
	if b.Has("sso") {
		t.Error("产品 B 拿到了产品 A 的 feature —— 串了")
	}
}

// ★ 过期不停服：宽限期内功能照常。
func TestGracePeriodKeepsWorking(t *testing.T) {
	now := time.Now()
	m := newTestManager(now)
	// 已过期 10 天，仍在 30 天宽限内
	m.Load(payloadFor(map[string]ProductGrant{
		prodA: {Features: []string{"sso"}},
	}, now.AddDate(0, 0, -10), ""), "")

	if m.Status() != StatusGrace {
		t.Fatalf("status = %v, want grace", m.Status())
	}
	if !m.Has("sso") {
		t.Error("宽限期内功能必须照常 —— 过期不停服")
	}
	if m.ReadOnly() {
		t.Error("宽限期内不该只读")
	}
}

// 超出宽限：转只读，功能关闭。
func TestBeyondGraceGoesReadOnly(t *testing.T) {
	now := time.Now()
	m := newTestManager(now)
	m.Load(payloadFor(map[string]ProductGrant{
		prodA: {Features: []string{"sso"}},
	}, now.AddDate(0, 0, -(GraceDays+1)), ""), "")

	if m.Status() != StatusExpired {
		t.Fatalf("status = %v, want expired", m.Status())
	}
	if m.Has("sso") || !m.ReadOnly() {
		t.Error("超出宽限应关功能并只读")
	}
}

// ★ 指纹不匹配（客户恢复数据库/主从切换）：进入宽限而不是立刻停服。
func TestFingerprintMismatchEntersGrace(t *testing.T) {
	now := time.Now()
	m := newTestManager(now)
	m.Load(payloadFor(map[string]ProductGrant{
		prodA: {Features: []string{"sso"}},
	}, now.AddDate(1, 0, 0), "fingerprint-old"), "fingerprint-new")

	if m.Status() != StatusFingerprint {
		t.Fatalf("status = %v, want finger_mismat", m.Status())
	}
	if !m.Has("sso") {
		t.Error("指纹宽限期内功能必须照常 —— 故障期间降级等于雪上加霜")
	}
}

// 指纹优先于过期判定：两者同时发生时，提示指纹更贴近真实原因。
func TestFingerprintTakesPrecedence(t *testing.T) {
	now := time.Now()
	m := newTestManager(now)
	m.Load(payloadFor(map[string]ProductGrant{
		prodA: {Features: []string{"sso"}},
	}, now.AddDate(0, 0, -5), "old"), "new")

	if m.Status() != StatusFingerprint {
		t.Errorf("status = %v, want finger_mismat（指纹应优先于过期）", m.Status())
	}
}

// ★ 容量超限只提示不阻断：CheckCapacity 返回超限项，但不影响 Has/ReadOnly。
func TestCapacityWarnsButNeverBlocks(t *testing.T) {
	now := time.Now()
	m := newTestManager(now)
	m.Load(payloadFor(map[string]ProductGrant{
		prodA: {Features: []string{"sso"}, Capacity: map[string]int64{"nodes": 100}},
	}, now.AddDate(1, 0, 0), ""), "")

	ex := m.CheckCapacity(map[string]int64{"nodes": 150})
	if len(ex) != 1 || ex[0].Item != "nodes" || ex[0].Current != 150 || ex[0].Limit != 100 {
		t.Fatalf("超限项 = %+v, want nodes 150/100", ex)
	}
	if m.ReadOnly() {
		t.Error("超限绝不能导致只读 —— 客户临时扩容不该让系统罢工")
	}
	if !m.Has("sso") {
		t.Error("超限绝不能关闭功能")
	}
}

// 上限为 0 表示不限，不该报超限。
func TestZeroLimitMeansUnlimited(t *testing.T) {
	now := time.Now()
	m := newTestManager(now)
	m.Load(payloadFor(map[string]ProductGrant{
		prodA: {Features: []string{"sso"}, Capacity: map[string]int64{"nodes": 0}},
	}, now.AddDate(1, 0, 0), ""), "")

	if ex := m.CheckCapacity(map[string]int64{"nodes": 99999}); len(ex) != 0 {
		t.Errorf("上限 0 应表示不限，却报了超限 %+v", ex)
	}
}

// license 里没写的容量项，回落到 CE 默认值。
func TestMissingCapacityFallsBackToCE(t *testing.T) {
	now := time.Now()
	m := newTestManager(now)
	m.Load(payloadFor(map[string]ProductGrant{
		prodA: {Features: []string{"sso"}, Capacity: map[string]int64{"nodes": 1000}},
	}, now.AddDate(1, 0, 0), ""), "")

	if got := m.Limit("seats"); got != 10 {
		t.Errorf("未指定的 seats = %d, want CE 默认 10", got)
	}
}

// Clear 后回到未激活。
func TestClear(t *testing.T) {
	now := time.Now()
	m := newTestManager(now)
	m.Load(payloadFor(map[string]ProductGrant{
		prodA: {Features: []string{"sso"}},
	}, now.AddDate(1, 0, 0), ""), "")
	m.Clear()

	if m.Status() != StatusNotActivated || m.Has("sso") {
		t.Error("Clear 后应回到未激活")
	}
}

// CEWritable 是产品级定位开关：默认关，现有产品行为一字不变。
func TestCEWritableDefaultsOff(t *testing.T) {
	m := NewManager(Options{Product: "p", CELimits: map[string]int64{"nodes": 100}})
	if !m.ReadOnly() {
		t.Fatal("默认应保持只读 —— 不打开开关的产品行为必须完全不变")
	}
}

// 打开后社区版可写，但功能与容量仍受限 —— 可写不等于免费版全给。
func TestCEWritableOnlyAffectsReadOnly(t *testing.T) {
	m := NewManager(Options{
		Product:     "p",
		CEWritable:  true,
		CELimits:    map[string]int64{"nodes": 100},
		Implemented: map[string]bool{"sso": true},
	})
	if m.ReadOnly() {
		t.Fatal("打开后社区版应可写")
	}
	if m.Has("sso") {
		t.Error("可写不等于给功能，EE 功能仍应为 false")
	}
	if got := m.Limit("nodes"); got != 100 {
		t.Errorf("容量仍应受 CE 上限约束, got %d", got)
	}
}

// 只放开"未激活"这一种状态：过期、超宽限、装了没买的产品仍然只读。
func TestCEWritableDoesNotUnlockOtherStates(t *testing.T) {
	now := time.Now()
	m := NewManager(Options{
		Product: prodA, CEWritable: true,
		Now: func() time.Time { return now },
	})

	// 装了没买的产品
	m.Load(payloadFor(map[string]ProductGrant{prodB: {}}, now.AddDate(1, 0, 0), ""), "")
	if !m.ReadOnly() {
		t.Error("装了没买的产品仍应只读")
	}

	// 超出宽限期的过期
	m.Load(payloadFor(map[string]ProductGrant{prodA: {}}, now.AddDate(0, 0, -GraceDays-1), ""), "")
	if !m.ReadOnly() {
		t.Error("超出宽限期仍应只读")
	}
}

// 状态必须随时间**自行**推进：装载之后一次 Load 都不再发生，
// 到期日一到就该转宽限，宽限期满就该只读，再过一段就该 lapsed。
//
// 这是本文件最重要的一条。早先状态是在 LoadAt 时算好缓存进 state 的，
// 而重新装载只有两条路——进程启动，或 Watch 发现 license 行的 updated_at
// 变了，而那个字段只有激活时才变。于是一个长期运行的进程会把 active
// 一直保持到重启为止。它之所以没被发现，是因为 DaysUntilExpiry 是实时算的：
// 界面上横幅红着、写着"已过期 20 天"，而写操作照常放行——授权页看起来完全正常。
func TestStatusAdvancesWithoutReload(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	expires := start.AddDate(0, 0, 10)
	m := newTestManager(start)
	m.LoadAt(payloadFor(map[string]ProductGrant{
		prodA: {Features: []string{"sso"}, Capacity: map[string]int64{"nodes": 5000}},
	}, expires, ""), "", time.Time{})

	if got := m.Status(); got != StatusActive {
		t.Fatalf("装载时 = %v, want active", got)
	}
	if got := m.Limit("nodes"); got != 5000 {
		t.Fatalf("生效期容量 = %d, want 5000", got)
	}

	// 之后只推进时间，不再调用任何 Load
	at := func(days int) { m.now = func() time.Time { return start.AddDate(0, 0, days) } }

	at(11) // 到期后第 1 天
	if got := m.Status(); got != StatusGrace {
		t.Errorf("到期后第 1 天 = %v, want grace", got)
	}
	if !m.Has("sso") {
		t.Error("宽限期内功能不该受影响")
	}
	if m.ReadOnly() {
		t.Error("宽限期内不该只读——过期不停服")
	}

	at(10 + GraceDays + 1) // 宽限期满
	if got := m.Status(); got != StatusExpired {
		t.Errorf("超出宽限期 = %v, want expired", got)
	}
	if !m.ReadOnly() {
		t.Error("超出宽限期必须只读——这正是缓存状态时失效的那条")
	}
	if m.Has("sso") {
		t.Error("超出宽限期功能应关闭")
	}
	if got := m.Limit("nodes"); got != 100 {
		t.Errorf("失效后容量应回落 CE = 100，实际 %d", got)
	}

	at(10 + LapsedDays + 1) // 过期满 30 天
	if got := m.Status(); got != StatusLapsed {
		t.Errorf("过期满 %d 天 = %v, want lapsed", LapsedDays, got)
	}
}

// 指纹不匹配的宽限期同样要自行结束，不能等到进程重启才发现。
//
// 这里把到期日设在一年后，所以下面观察到的状态变化只可能来自指纹宽限期。
func TestFingerprintGraceEndsWithoutReload(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m := newTestManager(start)
	p := payloadFor(map[string]ProductGrant{prodA: {Features: []string{"sso"}}},
		start.AddDate(1, 0, 0), "fp-签发时的")
	m.LoadAt(p, "fp-当前的", start) // 第三个参数：首次发现不匹配就是此刻

	if got := m.Status(); got != StatusFingerprint {
		t.Fatalf("刚发现不匹配 = %v, want finger_mismat", got)
	}
	if m.ReadOnly() {
		t.Error("指纹宽限期内不该只读——客户多半正在恢复数据库，别在故障上再加一层")
	}

	m.now = func() time.Time { return start.AddDate(0, 0, FingerprintGraceDays+1) }
	if got := m.Status(); got != StatusExpired {
		t.Errorf("指纹宽限期满 = %v, want expired", got)
	}
	if !m.ReadOnly() {
		t.Error("指纹宽限期满必须只读")
	}
}

// 算不出安装指纹时**不能**静默放行。
//
// 产品在数据库读不到 install_uuid / server_id 时，最自然的写法是返回空串。
// 早先内核的判定条件里有 `fingerprint != ""`，于是空串等于"跳过指纹校验"——
// 一张绑着别人指纹的 license 在任何环境都能跑，而且状态显示 active，
// 没有任何迹象表明这道防线没在工作。实际就有两个产品这么传，
// 其中一个的日志还写着"将按指纹不匹配处理"，与真实行为正好相反。
//
// 现在空指纹按不匹配处理：进宽限期（不是直接只读 —— 算不出指纹多半是
// 数据库出了问题，那种时刻把系统降级等于在故障上再加一层），
// 但状态是 finger_mismat，界面和日志都看得见。
func TestEmptyFingerprintDoesNotSkipBinding(t *testing.T) {
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	m := newTestManager(now)
	p := payloadFor(map[string]ProductGrant{prodA: {Features: []string{"sso"}}},
		now.AddDate(1, 0, 0), "fp-签发时绑定的")

	// 产品算不出指纹，传空串
	m.LoadAt(p, "", now)

	if got := m.Status(); got != StatusFingerprint {
		t.Fatalf("空指纹 = %v, want finger_mismat（绝不能是 active）", got)
	}
	// 宽限期内功能照常 —— 数据库故障时不该再叠一层降级
	if !m.Has("sso") {
		t.Error("指纹宽限期内功能不该受影响")
	}
	if m.ReadOnly() {
		t.Error("指纹宽限期内不该只读")
	}

	// 宽限期满则转只读，不会永远宽限下去
	m.now = func() time.Time { return now.AddDate(0, 0, FingerprintGraceDays+1) }
	if got := m.Status(); got != StatusExpired {
		t.Errorf("空指纹宽限期满 = %v, want expired", got)
	}

	// 对照：可移植 license（未绑定指纹）不受影响
	portable := payloadFor(map[string]ProductGrant{prodA: {Features: []string{"sso"}}},
		now.AddDate(1, 0, 0), "")
	m2 := newTestManager(now)
	m2.LoadAt(portable, "", time.Time{})
	if got := m2.Status(); got != StatusActive {
		t.Errorf("可移植授权不该受指纹影响 = %v, want active", got)
	}
}

package licensekit

import (
	"sort"
	"strings"
	"testing"
	"time"
)

var testPlans = map[string][]string{
	"standard":   {"sso", "audit"},
	"enterprise": {"sso", "audit", "scim", "branding"},
}

func keys(m map[string]bool) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestResolveFeatures(t *testing.T) {
	cases := []struct {
		name     string
		declared []string
		want     string
	}{
		{"档次展开", []string{"plan:standard"}, "audit,sso"},
		{"高档次", []string{"plan:enterprise"}, "audit,branding,scim,sso"},
		{"档次加购", []string{"plan:standard", "+scim"}, "audit,scim,sso"},
		{"档次排除", []string{"plan:enterprise", "-branding"}, "audit,scim,sso"},
		// 早期直接列清单的写法必须继续有效，否则已发出的 license 会突然失效
		{"裸名兼容", []string{"sso", "audit"}, "audit,sso"},
		{"裸名与档次混用", []string{"plan:standard", "formfill"}, "audit,formfill,sso"},
		// 未知档次给空集而不是全给：多给是收不回的，少给客户会来问
		{"未知档次得空集", []string{"plan:galaxy"}, ""},
		{"空声明", nil, ""},
		{"空白项被忽略", []string{"  ", "sso"}, "sso"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := keys(resolveFeatures(c.declared, testPlans)); got != c.want {
				t.Errorf("resolveFeatures(%v) = %q, want %q", c.declared, got, c.want)
			}
		})
	}
}

// 排除必须与书写位置无关。
//
// 边遍历边增删的实现里，["-branding","plan:enterprise"] 会把 branding 又加回来，
// 而"调换两个元素的顺序改变了授权范围"是没人查得出来的那类 bug。
func TestRevokeIsOrderIndependent(t *testing.T) {
	a := keys(resolveFeatures([]string{"plan:enterprise", "-branding"}, testPlans))
	b := keys(resolveFeatures([]string{"-branding", "plan:enterprise"}, testPlans))
	if a != b {
		t.Fatalf("排除受书写顺序影响：%q vs %q", a, b)
	}
	if strings.Contains(a, "branding") {
		t.Fatalf("branding 应当被排除，得到 %q", a)
	}
}

// plan: 前缀绝不能被当成一个名叫 "plan:xxx" 的功能泄漏出去。
func TestPlanPrefixIsNotAFeature(t *testing.T) {
	got := resolveFeatures([]string{"plan:standard"}, testPlans)
	if got["plan:standard"] {
		t.Fatal("plan: 声明被当成了功能名")
	}
}

func TestApplyEntitlement(t *testing.T) {
	cut := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	since := map[string]time.Time{
		"sso":   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), // 维护期前，给
		"scim":  time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), // 维护期后，不给
		"audit": cut,                                         // 正好当天，给（不是 After）
	}

	t.Run("裁掉维护期之后引入的功能", func(t *testing.T) {
		in := map[string]bool{"sso": true, "scim": true, "audit": true}
		if got := keys(applyEntitlement(in, since, cut)); got != "audit,sso" {
			t.Errorf("= %q, want %q", got, "audit,sso")
		}
	})

	// 没有这个字段的老 token 解出来就是零值，不能让它们突然什么都拿不到
	t.Run("零值不做任何裁剪", func(t *testing.T) {
		in := map[string]bool{"sso": true, "scim": true}
		if got := keys(applyEntitlement(in, since, time.Time{})); got != "scim,sso" {
			t.Errorf("= %q, want %q", got, "scim,sso")
		}
	})

	// 忘了登记引入日期 → 放行。失效方向是"多给"，由签发/上线检查清单兜底
	t.Run("未登记引入日期的一律放行", func(t *testing.T) {
		in := map[string]bool{"formfill": true}
		if got := keys(applyEntitlement(in, since, cut)); got != "formfill" {
			t.Errorf("= %q, want %q", got, "formfill")
		}
	})
}

// ── 永久授权 ────────────────────────────────────────────────

func perpetualManager(now func() time.Time) *Manager {
	return NewManager(Options{
		Product:     "p",
		Implemented: map[string]bool{"sso": true, "scim": true, "branding": true, "audit": true},
		Plans:       testPlans,
		Now:         now,
	})
}

// 回归：加 Perpetual 之前，不填 ExpiresAt 的 license 会被判成「已过期」——
// 签发方以为签了永久，客户装上去看到过期，且没有任何报错。
func TestPerpetualNeverExpires(t *testing.T) {
	now := func() time.Time { return time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC) }
	m := perpetualManager(now)
	m.Load(&Payload{
		Perpetual: true,
		Products:  map[string]ProductGrant{"p": {Features: []string{"plan:enterprise"}}},
		// ExpiresAt 刻意留零值：永久授权不该关心它
	}, "")

	if got := m.Status(); got != StatusActive {
		t.Fatalf("永久授权在 2099 年应当仍是 %s，得到 %s", StatusActive, got)
	}
	if !m.Has("scim") {
		t.Error("永久授权应当能用 enterprise 档的功能")
	}
	if m.ReadOnly() {
		t.Error("永久授权不该是只读")
	}
}

// 非永久 + 零值到期日 = 已过期。这是正确行为：
// 零值意味着"没填"，而没填到期日的 license 不该被当成永久放行。
func TestZeroExpiryWithoutPerpetualIsExpired(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC) }
	m := perpetualManager(now)
	m.Load(&Payload{
		Products: map[string]ProductGrant{"p": {Features: []string{"plan:standard"}}},
	}, "")

	// 断言意图而不是具体状态值：零值到期日等于"过期了两千年"，
	// 落在 expired 还是 lapsed 都对，关键是**它绝不能被当成永久放行**。
	if got := m.Status(); got != StatusExpired && got != StatusLapsed {
		t.Fatalf("没填到期日且非永久，应当已过期，得到 %s", got)
	}
	if !m.ReadOnly() {
		t.Error("没填到期日的 license 不该是可写的")
	}
	if m.Has("sso") {
		t.Error("没填到期日的 license 不该给功能")
	}
}

// 永久 + 指纹对不上 → 立刻降级，没有无限宽限。
//
// 永久授权本来就是靠绑定指纹限制影响面的（LICENSING.md §3），
// 若给它按 ExpiresAt 算宽限期，零值算出来是公元 1 年，
// 反而会得到"永远不在宽限期内"或"永远在宽限期内"这类由零值决定的荒谬结论。
func TestPerpetualFingerprintMismatchDegrades(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC) }
	m := perpetualManager(now)
	m.Load(&Payload{
		Perpetual: true,
		InstallID: "fingerprint-A",
		Products:  map[string]ProductGrant{"p": {Features: []string{"plan:enterprise"}}},
	}, "fingerprint-B")

	if got := m.Status(); got != StatusExpired {
		t.Fatalf("永久授权指纹不匹配应当降级为 %s，得到 %s", StatusExpired, got)
	}
	if m.Has("scim") {
		t.Error("降级后不该还能用付费功能")
	}
}

// 档次授权必须真的通到 Has()。
// 若 Has() 仍读未展开的 grant.Features，"plan:enterprise" 会被当成功能名，
// 所有按档次签的 license 会全部失效——而按清单签的老 license 照常，
// 于是问题只在新客户身上出现。
func TestHasUsesResolvedPlan(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 8, 9, 0, 0, 0, 0, time.UTC) }
	m := perpetualManager(now)
	m.Load(&Payload{
		Perpetual: true,
		Products:  map[string]ProductGrant{"p": {Features: []string{"plan:standard", "-audit"}}},
	}, "")

	if !m.Has("sso") {
		t.Error("standard 档应当含 sso")
	}
	if m.Has("audit") {
		t.Error("audit 已被 -audit 排除")
	}
	if m.Has("scim") {
		t.Error("scim 不在 standard 档里")
	}
	if m.Has("plan:standard") {
		t.Error("plan: 声明泄漏成了功能名")
	}
}

// 维护期在 Manager 层通到底：签了 enterprise 但维护期早于 scim 的引入日期 → 拿不到 scim。
func TestEntitledUntilGatesNewFeatureEndToEnd(t *testing.T) {
	now := func() time.Time { return time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC) }
	m := NewManager(Options{
		Product:     "p",
		Implemented: map[string]bool{"sso": true, "scim": true},
		Plans:       map[string][]string{"enterprise": {"sso", "scim"}},
		FeatureSince: map[string]time.Time{
			"scim": time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		},
		Now: now,
	})
	m.Load(&Payload{
		Perpetual:     true,
		EntitledUntil: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Products:      map[string]ProductGrant{"p": {Features: []string{"plan:enterprise"}}},
	}, "")

	if !m.Has("sso") {
		t.Error("维护期之前就有的功能应当照常可用")
	}
	if m.Has("scim") {
		t.Error("维护期之后引入的功能不该给")
	}
}

// ── 指纹宽限期的起算点 ──────────────────────────────────────

func fpManager(now func() time.Time) *Manager {
	return NewManager(Options{
		Product:     "p",
		Implemented: map[string]bool{"sso": true},
		Plans:       map[string][]string{"standard": {"sso"}},
		Now:         now,
	})
}

func yearLongLicense() *Payload {
	return &Payload{
		InstallID: "fingerprint-A",
		ExpiresAt: time.Date(2027, 8, 9, 0, 0, 0, 0, time.UTC),
		Products:  map[string]ProductGrant{"p": {Features: []string{"plan:standard"}}},
	}
}

// 这是修之前的问题：一年期 license 复制到别的环境，整整一年都能用。
// 保留这条断言不是为了"锁住 bug"，是为了让**旧入口的宽松行为是显式的**——
// 哪天有人想改 Load 的默认，这里会立刻变红，逼他先想清楚兼容性。
func TestLegacyLoadIsLenientOnMismatch(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) }
	m := fpManager(now)
	m.Load(yearLongLicense(), "fingerprint-B") // 不传首次发现时刻

	if got := m.Status(); got != StatusFingerprint {
		t.Fatalf("旧入口应当仍是宽松的 %s，得到 %s", StatusFingerprint, got)
	}
	if !m.Has("sso") {
		t.Error("宽限期内功能应当照常")
	}
}

// LoadAt 传入首次发现时刻后，宽限期从那一刻起算 14 天。
func TestLoadAtGraceStartsFromDetection(t *testing.T) {
	detected := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	t.Run("发现后第10天仍在宽限期", func(t *testing.T) {
		m := fpManager(func() time.Time { return detected.AddDate(0, 0, 10) })
		m.LoadAt(yearLongLicense(), "fingerprint-B", detected)
		if got := m.Status(); got != StatusFingerprint {
			t.Fatalf("= %s, want %s", got, StatusFingerprint)
		}
		if !m.Has("sso") {
			t.Error("宽限期内功能应当照常，好让客户有时间重新激活")
		}
	})

	// 关键断言：距离到期还有大半年，但复制出去 15 天就失效了
	t.Run("发现后第15天降级", func(t *testing.T) {
		m := fpManager(func() time.Time { return detected.AddDate(0, 0, 15) })
		m.LoadAt(yearLongLicense(), "fingerprint-B", detected)
		if got := m.Status(); got != StatusExpired {
			t.Fatalf("超出 %d 天宽限应当降级为 %s，得到 %s", FingerprintGraceDays, StatusExpired, got)
		}
		if m.Has("sso") {
			t.Error("降级后不该还能用付费功能")
		}
	})
}

// 指纹一致时，传不传首次发现时刻都不该有任何影响。
func TestMismatchSinceIgnoredWhenFingerprintMatches(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC) }
	m := fpManager(now)
	m.LoadAt(yearLongLicense(), "fingerprint-A", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))

	if got := m.Status(); got != StatusActive {
		t.Fatalf("指纹一致应当是 %s，得到 %s", StatusActive, got)
	}
}

// ── 到期三段：grace / expired / lapsed ──────────────────────

func expiryManager(now time.Time) *Manager {
	return NewManager(Options{
		Product:     "p",
		Implemented: map[string]bool{"sso": true},
		Plans:       map[string][]string{"standard": {"sso"}},
		Now:         func() time.Time { return now },
	})
}

var expiryDay = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

func loadExpiring(m *Manager) {
	m.LoadAt(&Payload{
		ExpiresAt: expiryDay,
		Products:  map[string]ProductGrant{"p": {Features: []string{"plan:standard"}}},
	}, "", time.Time{})
}

// 三段边界一次钉死。对齐 GitLab：14 天宽限、过期满 30 天转"需重新采购"。
func TestExpiryBands(t *testing.T) {
	cases := []struct {
		name     string
		dayAfter int
		want     Status
		usable   bool // 功能是否照常
	}{
		{"到期前一天", -1, StatusActive, true},
		{"到期当天", 0, StatusGrace, true},
		{"宽限期第13天", 13, StatusGrace, true},
		// 第 14 天整宽限期结束 —— 边界取"不含"，与 GitLab 第 15 天转只读一致
		{"宽限期满转只读", 14, StatusExpired, false},
		{"过期第29天仍是可续期的只读", 29, StatusExpired, false},
		{"过期满30天转需重新采购", 30, StatusLapsed, false},
		{"过期很久", 400, StatusLapsed, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := expiryManager(expiryDay.AddDate(0, 0, c.dayAfter))
			loadExpiring(m)
			if got := m.Status(); got != c.want {
				t.Fatalf("第 %d 天 = %s, want %s", c.dayAfter, got, c.want)
			}
			if got := m.Has("sso"); got != c.usable {
				t.Errorf("功能可用性 = %v, want %v", got, c.usable)
			}
			// lapsed 不是更严的锁：和 expired 一样只读，不多锁任何东西
			if c.want == StatusLapsed && !m.ReadOnly() {
				t.Error("lapsed 应当是只读")
			}
		})
	}
}

// 到期提醒：提前 30 天开始提示，这才是"采购流程慢"的正解。
func TestExpiryReminder(t *testing.T) {
	cases := []struct {
		dayAfter   int
		wantDays   int
		wantRemind bool
	}{
		{-90, 90, false}, // 还早，不打扰
		{-31, 31, false}, // 恰好在阈值外
		{-30, 30, true},  // 开始提醒
		{-1, 1, true},
		{0, 0, true},
		{7, -7, true}, // 已过期 7 天，横幅更该显示
	}
	for _, c := range cases {
		m := expiryManager(expiryDay.AddDate(0, 0, c.dayAfter))
		loadExpiring(m)
		days, ok := m.DaysUntilExpiry()
		if !ok {
			t.Fatalf("第 %d 天：应当能算出剩余天数", c.dayAfter)
		}
		if days != c.wantDays {
			t.Errorf("第 %d 天：剩余 = %d, want %d", c.dayAfter, days, c.wantDays)
		}
		if got := m.ShouldRemind(); got != c.wantRemind {
			t.Errorf("第 %d 天：是否提醒 = %v, want %v", c.dayAfter, got, c.wantRemind)
		}
	}
}

// 永久授权没有"还剩几天"可言，界面不该出现倒计时。
func TestPerpetualHasNoCountdown(t *testing.T) {
	m := expiryManager(expiryDay)
	m.LoadAt(&Payload{
		Perpetual: true,
		Products:  map[string]ProductGrant{"p": {Features: []string{"plan:standard"}}},
	}, "", time.Time{})

	if _, ok := m.DaysUntilExpiry(); ok {
		t.Error("永久授权不该算出剩余天数")
	}
	if m.ShouldRemind() {
		t.Error("永久授权不该提醒到期")
	}
}

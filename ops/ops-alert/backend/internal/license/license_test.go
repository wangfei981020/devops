package license

import "testing"

// LICENSING.md §2 定的三道防线，职责不能混：
//
//	Implemented   这功能有没有代码在跑   → 不在表里 Has() 必须 false
//	plan / +/-    客户买没买             → 没买 Has() 必须 false
//	entitled_until 够不够格拿新功能      → 由内核管
//
// 这个文件锁住前两道在**本产品**的表现。内核自己的状态机由 licensekit 的测试锁。

// 未激活时不能有任何付费功能 —— 否则等于白送。
func TestCommunityHasNoPaidFeature(t *testing.T) {
	m := NewManager()
	for _, f := range AllFeatures() {
		if m.Has(f) {
			t.Errorf("未激活状态下 %s 不该可用 —— 这是白送一个付费功能", f)
		}
	}
}

// 「卖了但没做」：签进 license 也不能放行，否则界面上会出现点不动的功能。
func TestUnimplementedNeverGranted(t *testing.T) {
	for _, f := range []Feature{FeatureSSO, FeatureMultiTenant, FeatureBranding} {
		if Implemented(f) {
			t.Errorf("%s 被标成已实现了 —— 但代码里还没有它，客户买了会看到一个点不动的开关", f)
		}
	}
}

// 「买了却不给用」：已经有代码在跑的功能忘了进 implemented 表，
// 表现是客户付了钱功能却是灰的，而且不报错。
func TestShippedFeaturesAreMarkedImplemented(t *testing.T) {
	// 这四项在代码里确实有实现（回放页、降噪页、多数据源、10 个 MCP 工具）
	for _, f := range []Feature{
		FeatureBacktest, FeatureNoise, FeatureMultiDatasource, FeatureMCPFull,
	} {
		if !Implemented(f) {
			t.Errorf("%s 已经有代码在跑，却没进 implemented 表 —— 客户买了也用不了，且不报错", f)
		}
	}
}

// 档次要能展开成功能，否则「签档次不签清单」就退化成一个空集：
// 客户付了钱、状态显示 active、Has() 全 false，且不报任何错。
func TestPlansExpandToFeatures(t *testing.T) {
	if len(plans["enterprise"]) == 0 {
		t.Fatal("enterprise 档次是空的 —— 签 plan:enterprise 的客户一个功能都拿不到")
	}
	if len(plans["standard"]) >= len(plans["enterprise"]) {
		t.Error("standard 不该覆盖到和 enterprise 一样多的功能，否则分档没有意义")
	}
	// standard 必须是 enterprise 的子集：否则会出现「买了高档反而少了功能」
	ent := map[string]bool{}
	for _, f := range plans["enterprise"] {
		ent[f] = true
	}
	for _, f := range plans["standard"] {
		if !ent[f] {
			t.Errorf("standard 有 %s 而 enterprise 没有 —— 升档会丢功能", f)
		}
	}
}

// CE 必须可写。告警平台建不了规则 = 一条告警都收不到，试用者第一分钟就走。
func TestCommunityIsWritable(t *testing.T) {
	if !Options().CEWritable {
		t.Error("CE 被设成只读了 —— 告警平台的社区版建不了规则就等于不能用")
	}
}

// CE 容量要够试用。卡太死只会让人第一天放弃，而付费驱动应该是功能不是数量。
func TestCommunityLimitsAreGenerous(t *testing.T) {
	if ceLimits[string(CapRules)] < 10 {
		t.Errorf("CE 规则上限 %d 太小，覆盖不了一个团队的核心链路", ceLimits[string(CapRules)])
	}
}

// 超容量只提示不拒绝（内核契约）。因为超了 20 条就不让建第 21 条，
// 而第 21 条恰好是这次事故要加的那条 —— 这种产品没人会续费。
func TestCapacityOverageDoesNotBlock(t *testing.T) {
	m := NewManager()
	ex := m.CheckCapacity(map[Capacity]int64{CapRules: 999})
	if len(ex) == 0 {
		t.Fatal("999 条规则应当被判为超限并提示")
	}
	// 关键：它返回的是"超限项清单"，不是 error —— 调用方不该据此拒绝操作
	if ex[0].Current != 999 {
		t.Errorf("超限项里的当前值不对：%d", ex[0].Current)
	}
}

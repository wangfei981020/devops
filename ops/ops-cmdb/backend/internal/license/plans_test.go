package license

import "testing"

// plans 表写错一个字符串不会报错，只是那个功能永远不开 ——
// 客户付了钱、状态是 active、按钮就是灰的。这几条断言就是防这个。

func TestPlansOnlyReferenceKnownFeatures(t *testing.T) {
	for plan, feats := range plans {
		for _, f := range feats {
			if !IsKnownFeature(Feature(f)) {
				t.Errorf("档次 %q 里的 %q 不在功能目录里 —— 打错字的话它永远不会生效，且不报错", plan, f)
			}
		}
	}
}

func TestPlansAreCumulative(t *testing.T) {
	// 高档次必须完全包含低档次。
	// 漏一个的后果很荒谬但不报错：花更多钱的客户反而少一个功能，
	// 而且只有那个客户用到那个功能时才会发现。
	order := []string{"standard", "professional", "enterprise"}
	for i := 1; i < len(order); i++ {
		lower, higher := set(plans[order[i-1]]), set(plans[order[i]])
		for f := range lower {
			if !higher[f] {
				t.Errorf("%q 缺少 %q 里的功能 %q —— 高档次必须包含低档次", order[i], order[i-1], f)
			}
		}
	}
}

func TestPlansHaveNoDuplicates(t *testing.T) {
	for plan, feats := range plans {
		seen := map[string]bool{}
		for _, f := range feats {
			if seen[f] {
				t.Errorf("档次 %q 里 %q 重复了", plan, f)
			}
			seen[f] = true
		}
	}
}

func TestEnterpriseCoversEveryFeature(t *testing.T) {
	// 最高档次应当覆盖全部功能。有功能不属于任何档次，
	// 意味着它只能靠单项加购（+feature）卖，而销售根本不会知道有这回事。
	top := set(plans["enterprise"])
	for f := range allFeatures {
		if !top[string(f)] {
			t.Errorf("功能 %q 不在 enterprise 档次里 —— 确认是有意只做单项加购，还是漏了", f)
		}
	}
}

func TestProductIDMatchesSpec(t *testing.T) {
	// 与 LICENSING.md §13.1 一致。不一致的话，按文档签出来的 license
	// 会因为查不到本产品的 key 而落到 not_licensed —— 客户买了却用不了。
	if ProductID != "ops-cmdb" {
		t.Fatalf("ProductID = %q，规范要求 ops-cmdb", ProductID)
	}
}

func set(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

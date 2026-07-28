package cdnsource

import "testing"

// 套餐名在 CF 返回里是 "Free Website"/"Pro Plan" 这类带后缀的写法，
// 直接全串匹配会全部落空、上限判定静默失效。
func TestPageRuleLimitParsesPlanNames(t *testing.T) {
	cases := map[string]int{
		"Free Website": 3, "free": 3,
		"Pro Plan": 20, "Business Plan": 50, "Enterprise Website": 125,
		"":            0, // 未知套餐返回 0 = 不做上限判定，而不是当成 0 条上限
		"Custom Tier": 0,
	}
	for plan, want := range cases {
		if got := PageRuleLimit(plan); got != want {
			t.Errorf("PageRuleLimit(%q) = %d，期望 %d", plan, got, want)
		}
	}
}

// 动作值可能是字符串/数字/对象，一律原样带出——排查时要看出实际配了什么。
func TestActionSummaryKeepsValue(t *testing.T) {
	if got := actionSummary("cache_level", []byte(`"cache_everything"`)); got != "cache_level=cache_everything" {
		t.Errorf("字符串值应去引号，实际 %q", got)
	}
	if got := actionSummary("always_use_https", nil); got != "always_use_https" {
		t.Errorf("无值动作应只留 id，实际 %q", got)
	}
	if got := actionSummary("edge_cache_ttl", []byte(`7200`)); got != "edge_cache_ttl=7200" {
		t.Errorf("数字值应保留，实际 %q", got)
	}
}

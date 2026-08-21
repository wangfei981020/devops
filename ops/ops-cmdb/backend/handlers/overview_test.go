package handlers

import "testing"

func p64(v int64) *int64 { return &v }

// ★ 统计失败必须排在最前。
//
// Count == nil 意味着这个维度我们**完全没看到**，比"看到了 3 条"更值得先处理。
// 排在最后的话它会被淹没在正常项里，而首页看起来一切正常 ——
// 那正是"没报错但结果是假的"在首页上的形态。
func TestSortAttentionUnknownFirst(t *testing.T) {
	items := []attentionItem{
		{Key: "low", Severity: "medium", Count: p64(2)},
		{Key: "high", Severity: "high", Count: p64(1)},
		{Key: "broken", Severity: "medium", Count: nil},
	}
	sortAttention(items)
	if items[0].Key != "broken" {
		t.Errorf("统计失败的应排最前，得到 %s", items[0].Key)
	}
	if items[1].Key != "high" {
		t.Errorf("高危应排在中危前，得到 %s", items[1].Key)
	}
}

func TestSortAttentionSameSeverityByCount(t *testing.T) {
	items := []attentionItem{
		{Key: "a", Severity: "high", Count: p64(1)},
		{Key: "b", Severity: "high", Count: p64(9)},
	}
	sortAttention(items)
	if items[0].Key != "b" {
		t.Errorf("同级应按数量降序，得到 %s", items[0].Key)
	}
}

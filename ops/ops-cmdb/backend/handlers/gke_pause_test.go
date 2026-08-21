package handlers

import (
	"strings"
	"testing"
)

// pausedReason 是**逗号分隔的多个枚举**，每一条都要翻译，
// 而 kind 必须取其中最严重的那一档。
//
// 守的是 OPSCMDB-031 P1-20。原来是一个 switch，第一条匹配就返回：
// 实测生产上出现过 `MAINTENANCE_WINDOW,CLUSTER_DISRUPTION_BUDGET_MINOR_UPGRADE`，
// 第二条被静默丢掉。
//
// 🔴 最危险的排列是「常态在前、真挡住在后」：
// 页面会说「常态，到窗口就会自动升级」，而实际上升级被维护排除挡死了 ——
// **与事实相反**，比不显示更糟。
func TestClassifyPauseKeepsEveryReason(t *testing.T) {
	cases := []struct {
		name     string
		reason   string
		wantKind string
		wantIn   []string
	}{
		{
			name: "空 → 什么都不说", reason: "", wantKind: "", wantIn: nil,
		},
		{
			name:     "单条常态节流",
			reason:   "MAINTENANCE_WINDOW",
			wantKind: "throttled",
			wantIn:   []string{"维护窗口"},
		},
		{
			// 🔴 P1-20 的原型：两条都要出现
			name:     "两条都要翻译，一条都不能丢",
			reason:   "MAINTENANCE_WINDOW,CLUSTER_DISRUPTION_BUDGET_MINOR_UPGRADE",
			wantKind: "throttled",
			wantIn:   []string{"维护窗口", "中断预算"},
		},
		{
			// 🔴 最危险的一条：常态排在前面，真正挡住的排在后面
			name:     "常态在前、真挡住在后 → kind 必须是 excluded",
			reason:   "MAINTENANCE_WINDOW,MAINTENANCE_EXCLUSION_NO_UPGRADES",
			wantKind: "excluded",
			wantIn:   []string{"维护窗口", "真正挡住"},
		},
		{
			name:     "没登记的枚举照原样带出来",
			reason:   "SOME_BRAND_NEW_REASON",
			wantKind: "throttled",
			wantIn:   []string{"未识别", "SOME_BRAND_NEW_REASON"},
		},
		{
			name:     "没登记的排除类型按被挡住处理",
			reason:   "MAINTENANCE_EXCLUSION_SOMETHING_NEW",
			wantKind: "excluded",
			wantIn:   []string{"真正挡住"},
		},
		{
			name:     "多余的逗号和空格不产生空条目",
			reason:   " MAINTENANCE_WINDOW , ,",
			wantKind: "throttled",
			wantIn:   []string{"维护窗口"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, note := classifyPause(c.reason)
			if kind != c.wantKind {
				t.Errorf("kind = %q，期望 %q（note=%q）", kind, c.wantKind, note)
			}
			for _, w := range c.wantIn {
				if !strings.Contains(note, w) {
					t.Errorf("note 里没有 %q：%s", w, note)
				}
			}
			if c.reason == "" && note != "" {
				t.Errorf("没有暂停原因时不该编一句话出来：%q", note)
			}
		})
	}
}

// 同一条原因出现两次不该说两遍。
func TestClassifyPauseDedupes(t *testing.T) {
	_, note := classifyPause("MAINTENANCE_WINDOW,MAINTENANCE_WINDOW")
	if strings.Count(note, "维护窗口") != 1 {
		t.Errorf("重复的原因应合并成一条：%s", note)
	}
}

package diag

import (
	"strings"
	"testing"
)

// 🔴 准入判定的核心：**日志里有报错行就不该花钱**。
// Layer 1 已经把那一行原样摆出来了，比模型的转述更可信也更便宜。
func TestShouldCallAI(t *testing.T) {
	cases := []struct {
		name    string
		res     *DiagnosisResult
		enabled bool
		want    bool
		code    string
	}{
		{
			name:    "规则判出具体根因 → 不调",
			res:     &DiagnosisResult{Matched: true, RootCause: "JVM 堆内存溢出"},
			enabled: true, want: false, code: "rule_matched",
		},
		{
			name: "泛化结论，但日志里有报错行 → 不调（看那几行更可信）",
			res: &DiagnosisResult{
				Matched: true, Generic: true, RootCause: "容器反复重启",
				Extracted: &ExtractedEvidence{Scanned: 200, Lines: []string{"connection refused"}},
			},
			enabled: true, want: false, code: "evidence_found",
		},
		{
			name: "泛化结论 + 日志里一条报错都没有 → 这才该调",
			res: &DiagnosisResult{
				Matched: false, Generic: true, RootCause: "未识别到已知故障模式",
				Extracted: &ExtractedEvidence{Scanned: 200, NoErrorFound: true},
			},
			enabled: true, want: true,
		},
		{
			name: "同上但没开 AI → 不调，且说清楚是没开而不是判过了",
			res: &DiagnosisResult{
				Generic: true, Extracted: &ExtractedEvidence{Scanned: 200, NoErrorFound: true},
			},
			enabled: false, want: false, code: "disabled",
		},
		{
			name:    "连日志都没采到 → 不调（喂进去是空的，先修采集）",
			res:     &DiagnosisResult{Generic: true, Extracted: nil},
			enabled: true, want: false, code: "no_context",
		},
	}
	for _, c := range cases {
		d := ShouldCallAI(c.res, c.enabled)
		if d.Allowed != c.want {
			t.Errorf("%s: Allowed=%v 期望 %v（理由：%s）", c.name, d.Allowed, c.want, d.Reason)
		}
		if c.code != "" && d.Code != c.code {
			t.Errorf("%s: Code=%q 期望 %q", c.name, d.Code, c.code)
		}
		// ⚠️ 无论允不允许都必须有逐层说明 —— 只在允许时记录的话，
		//	「这个 Pod 为什么没走 AI」就永远查不到
		if len(d.LayersTried) == 0 && d.Code != "no_context" {
			t.Errorf("%s: 没有逐层说明，等于没法回答「为什么走/不走 AI」", c.name)
		}
		if d.Reason == "" {
			t.Errorf("%s: Reason 为空", c.name)
		}
	}
}

// 🔴 硬上限：用满就停，并且**知道自己跳过了多少**。
// 静默截断的结果是「扫完了，这些就是全部问题」——而那是假的。
func TestAIBudgetIsHardCap(t *testing.T) {
	b := NewAIBudget(2, 0)
	if !b.Take() || !b.Take() {
		t.Fatal("前两次应当放行")
	}
	if b.Take() {
		t.Fatal("第三次必须被挡住 —— 这是硬上限不是软提示")
	}
	if b.Take() {
		t.Fatal("超限后仍在放行")
	}
	calls, _, skipped, note := b.Summary()
	if calls != 2 {
		t.Errorf("实际调用 %d 次，期望 2", calls)
	}
	if skipped != 2 {
		t.Errorf("跳过 %d 次，期望 2 —— 跳过数必须记，否则人会以为全扫完了", skipped)
	}
	if note == "" {
		t.Error("停下来的原因必须说出来")
	}
}

func TestAIBudgetCostCap(t *testing.T) {
	b := NewAIBudget(0, 0.01)
	if !b.Take() {
		t.Fatal("首次应当放行")
	}
	b.Charge(0.02, true) // 一次就超了
	if b.Take() {
		t.Fatal("成本超限后必须挡住")
	}
	_, cost, skipped, _ := b.Summary()
	if cost < 0.019 || skipped != 1 {
		t.Errorf("账单不对：cost=%v skipped=%d", cost, skipped)
	}
}

// nil 预算 = 不允许调用。⚠️ 不能反过来兜成"不限"——
// 忘了传预算就变成无上限，是最容易发生也最贵的那种错。
func TestNilBudgetDeniesByDefault(t *testing.T) {
	var b *AIBudget
	if b.Take() {
		t.Fatal("nil 预算必须拒绝，而不是当成不限")
	}
}

func TestAICost(t *testing.T) {
	// 认得的型号：按单价折算
	c, ok := AICost("claude-haiku-4-5", 1_000_000, 1_000_000)
	if !ok || c < 4.79 || c > 4.81 {
		t.Errorf("haiku 100 万 in + 100 万 out = US$%.4f（ok=%v），期望 4.80", c, ok)
	}
	// 🔴 不认得的型号必须返回 ok=false 并计 0 —— 编一个价钱比不记更坏：
	//	账单和我们记的数对不上时，人会信我们记的那个
	if c, ok := AICost("some-future-model", 1000, 1000); ok || c != 0 {
		t.Errorf("未知型号应当 (0,false)，得到 (%v,%v)", c, ok)
	}
}

// TestForcedGateOverridesRuleMatch 强制复核要能绕过"已有结论"，但绕不过硬约束。
//
// 🔴 双向：只放开该放开的两层，硬约束仍必须挡住 ——
// 否则「没开 AI 也强制」得到的是报错，「没日志也强制」得到的是模型编的。
func TestForcedGateOverridesRuleMatch(t *testing.T) {
	ruleHit := &DiagnosisResult{
		Matched: true, RootCause: "凭据不对，向依赖方认证失败",
		Extracted: &ExtractedEvidence{Scanned: 200, NoErrorFound: true},
	}
	if d := ShouldCallAI(ruleHit, true); d.Allowed || d.Code != "rule_matched" {
		t.Fatalf("默认应被 rule_matched 挡下，实际 %+v", d)
	}
	if d := ShouldCallAIForced(ruleHit, true); !d.Allowed {
		t.Fatalf("强制复核应放行，实际被挡：code=%s reason=%s", d.Code, d.Reason)
	}
	// 规则的初判必须留在层级说明里 —— 复核时人和模型都要看得到"初判是什么"
	if d := ShouldCallAIForced(ruleHit, true); len(d.LayersTried) == 0 ||
		!strings.Contains(strings.Join(d.LayersTried, "|"), "凭据不对") {
		t.Fatalf("强制后丢了规则初判：%v", d.LayersTried)
	}

	// 硬约束：没开 AI
	if d := ShouldCallAIForced(ruleHit, false); d.Allowed || d.Code != "disabled" {
		t.Fatalf("没开 AI 时强制也不该放行，实际 %+v", d)
	}
	// 硬约束：连日志都没采到
	noCtx := &DiagnosisResult{Matched: true, RootCause: "x", Extracted: nil}
	if d := ShouldCallAIForced(noCtx, true); d.Allowed || d.Code != "no_context" {
		t.Fatalf("没日志时强制也不该放行，实际 %+v", d)
	}
	// 日志里有报错行：默认挡下，强制放行
	hasErr := &DiagnosisResult{
		Generic: true, RootCause: "反复重启",
		Extracted: &ExtractedEvidence{Scanned: 200, Lines: []string{"boom"}},
	}
	if d := ShouldCallAI(hasErr, true); d.Allowed || d.Code != "evidence_found" {
		t.Fatalf("默认应被 evidence_found 挡下，实际 %+v", d)
	}
	if d := ShouldCallAIForced(hasErr, true); !d.Allowed {
		t.Fatalf("强制复核应放行，实际 %+v", d)
	}
}

// TestAIBudgetStopsOnUnknownModelPrice 价目表里没有的型号 → 成本记 0，
// 于是金额上限**永远触发不了**。
//
// 🔴 这个洞的形状：`Charge(0)` 累加不到 MaxCostUSD，一轮 sweep 能一直调下去，
// 只剩次数上限拦着。而"用了价目表还没跟上的新模型"恰恰是最可能花超的时候。
//
// ⚠️ 修法不是编一个价钱（那比不记更坏），而是让"不知道花了多少"本身成为停止条件。
func TestAIBudgetStopsOnUnknownModelPrice(t *testing.T) {
	// 金额上限设得很高，确保挡住它的不是金额而是"不可核算"
	b := NewAIBudget(10, 100)
	if !b.Take() {
		t.Fatal("首次应当放行")
	}
	b.Charge(0, false) // 认不出的型号：成本按 0 记
	if b.Take() {
		t.Fatal("型号不在价目表里、成本无法核算时，必须停止继续调用")
	}
	_, _, skipped, note := b.Summary()
	if skipped != 1 {
		t.Errorf("skipped = %d，want 1", skipped)
	}
	if !strings.Contains(note, "价目表") {
		t.Errorf("停止原因没说清楚：%q", note)
	}

	// 反向：认得的型号照常按金额判，别把这条改成"一旦 Charge 过就停"
	b2 := NewAIBudget(10, 100)
	for i := 0; i < 3; i++ {
		if !b2.Take() {
			t.Fatalf("第 %d 次被误挡：认得的型号且远未超额", i+1)
		}
		b2.Charge(0.001, true)
	}
}

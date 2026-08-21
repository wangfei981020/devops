package diag

import (
	"strings"
	"testing"
)

// 🔴 g50-dev/g50-plaza 的真实形态：报错被心跳刷出了 30 行窗口。
// 这正是「日志里其实写着，只是没人看见」——第 1 层要解决的就是它。
func TestExtractFindsErrorBuriedInNoise(t *testing.T) {
	var b strings.Builder
	b.WriteString("2026/08/19 01:40:01 [ERROR] [gamedb.go:40] game dao initialize RegisterDataBase, error=Access denied for user 'g32_dev'\n")
	// 之后 150 行心跳把它顶出末尾窗口
	for i := 0; i < 150; i++ {
		b.WriteString("2026/08/19 01:4" + string(rune('0'+i%10)) + ":0" + string(rune('0'+i%10)) +
			" [   INFO] [wrapper.go:265] [116] redis health check success, time now=2026-08-19 01:41:0" +
			string(rune('0'+i%10)) + "\n")
	}
	ex := ExtractErrors(b.String(), 8)

	if ex.NoErrorFound {
		t.Fatal("日志里明明有报错，却报告「一条都没有」")
	}
	if len(ex.Lines) == 0 || !strings.Contains(ex.Lines[0], "Access denied") {
		t.Fatalf("没捞出被刷屏顶掉的那条报错: %+v", ex.Lines)
	}
	// ⚠️ 心跳绝不能混进报错列表 —— 一旦混进来，人还得自己在里面找，这层就白做了
	for _, l := range ex.Lines {
		if strings.Contains(l, "health check success") {
			t.Errorf("把正常心跳当成报错捞进来了: %s", l)
		}
	}
}

// 🔴 「一条报错都没有」是**允许走 AI 的唯一前提**，必须判准。
func TestNoErrorFoundIsTheAIGate(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("[GIN] 2026/08/19 - 02:14:01 | 200 | 32.208µs | 10.42.10.1 | GET \"/healthcheck\"\n")
		b.WriteString("2026/08/19 02:14:01.001 [INFO] [client.go:275] redis health check success\n")
	}
	ex := ExtractErrors(b.String(), 8)
	if !ex.NoErrorFound {
		t.Fatalf("全是正常输出，却没判成「一条报错都没有」: %+v", ex.Lines)
	}
	for _, want := range []string{"一条报错都没有", "被外部强制终止", "query_loki"} {
		if !strings.Contains(ex.Note, want) {
			t.Errorf("说明里缺 %q: %s", want, ex.Note)
		}
	}
}

// 同一条报错每秒刷一遍时必须折叠，否则 8 条名额全被它占满，
// 真正的第一现场（更早的那条）反而挤不进来。
func TestRepeatedErrorsAreFolded(t *testing.T) {
	var b strings.Builder
	b.WriteString("2026/08/19 01:00:00 FATAL config parse failed at line 42\n")
	for i := 0; i < 60; i++ {
		b.WriteString("2026/08/19 01:0" + string(rune('0'+i%10)) + ":00 [ERROR] connection refused to db:3306\n")
	}
	ex := ExtractErrors(b.String(), 8)
	if len(ex.Lines) > 3 {
		t.Errorf("重复行没折叠，捞出了 %d 条: %+v", len(ex.Lines), ex.Lines)
	}
	if !strings.Contains(ex.Lines[0], "config parse failed") {
		t.Errorf("第一条应该是最早那条（最接近根因）: %s", ex.Lines[0])
	}
	if !strings.Contains(strings.Join(ex.Lines, " "), "重复") {
		t.Error("折叠了却没说折叠了多少次")
	}
}

// ANSI 颜色码要剥掉再折叠，否则同一行会因颜色不同被当成不同的行。
func TestANSIStrippedBeforeDedup(t *testing.T) {
	l := "\x1b[1;31m[  ERROR]\x1b[0m OrmInit failed, error=Access denied\n"
	ex := ExtractErrors(l+l+l, 8)
	if len(ex.Lines) != 1 {
		t.Fatalf("同一行带颜色码没被折叠成一条: %+v", ex.Lines)
	}
	if strings.Contains(ex.Lines[0], "\x1b[") {
		t.Errorf("颜色码没剥干净: %q", ex.Lines[0])
	}
}

// 规则给出泛化结论时，才补证据；已经判出具体根因的不该被日志淹掉。
func TestExtractedOnlyForGeneric(t *testing.T) {
	tail := "2026/08/19 FATAL something broke\n"
	crash := &DiagnosisContext{
		Containers: []ContainerCtx{{Name: "app", State: "waiting", StateReason: "CrashLoopBackOff", RestartCount: 9, LastExitCode: i32(1)}},
		LogTails:   map[string]string{"app": tail},
	}
	res := RuleProvider{}.Diagnose(crash)
	// FATAL 会被 fatal-line 信号命中 → 具体结论 → 不该带 Extracted
	if res.Generic {
		t.Fatalf("FATAL 应该被具体信号命中: %s", res.RootCause)
	}
	if res.Extracted != nil {
		t.Error("已判出具体根因，却仍附了证据提炼，会把结论淹掉")
	}
}

package handlers

import (
	"strings"
	"testing"
)

// 真实的 vite 构建失败日志片段（含 ANSI 码与栈帧）。
// 根因是 App.vue 少了个逗号，而它所在的行小写开头——旧实现一条都抓不到。
var viteFailLog = []string{
	"> g66-baccarat-pc-c@0.0.1 build /workspace/source",
	"> vite build",
	"vite v5.0.10 building for production...",
	"transforming...",
	"\x1b[31merror during build:\x1b[0m",
	"[vite:vue] [vue/compiler-sfc] Unexpected token, expected \",\" (38:2)",
	"/workspace/source/src/App.vue",
	"36 |  const a = 1",
	"    at createCompilerError (/workspace/node_modules/@vue/compiler-core/dist/compiler-core.cjs.js:1234:17)",
	"    at emitError (/workspace/node_modules/@vue/compiler-core/dist/compiler-core.cjs.js:5678:9)",
	" ELIFECYCLE  Command failed with exit code 1.",
	"ERR_PNPM_RECURSIVE_RUN_FIRST_FAIL @game-framework/g66-baccarat-pc-c@0.0.1 build: `vite build`",
	"Exit status 1",
	"ERROR: script returned exit code 1",
	"Finished: FAILURE",
}

func TestExtractErrorLinesCatchesLowercaseRootCause(t *testing.T) {
	got := extractErrorLines(viteFailLog)
	all := ""
	for _, g := range got {
		all += g["text"].(string) + " "
		if ctx, ok := g["context"].([]string); ok {
			all += strings.Join(ctx, " ") + " "
		}
	}
	// 根因必须出现——这正是旧实现漏掉的
	for _, must := range []string{"error during build", "Unexpected token", "App.vue"} {
		if !strings.Contains(all, must) {
			t.Errorf("抽取结果里必须含根因 %q，实际抽到:\n%s", must, all)
		}
	}
	// 但不能因为放宽模式就退化成把整段日志搬回来
	if len(got) > 10 {
		t.Errorf("抽取条数应在 10 条以内（放宽模式不能退化成全量），实际 %d 条", len(got))
	}
}

// ANSI 颜色码必须清掉，否则抽出来的行难读、也会干扰匹配。
func TestExtractErrorLinesStripsANSI(t *testing.T) {
	for _, g := range extractErrorLines(viteFailLog) {
		if strings.Contains(g["text"].(string), "\x1b[") {
			t.Errorf("抽取结果仍残留 ANSI 转义码: %q", g["text"])
		}
	}
}

// 上下文不该被栈帧占满——栈帧不含根因，挤掉的正是文件名和行号。
//
// 从 index 5（"Unexpected token" 那行）起：它后面依次是文件路径、代码片段、两行栈帧、
// 然后是下一个报错行。期望收进前两行、跳过栈帧、遇到下一个报错停下。
func TestCollectContextSkipsStackFrames(t *testing.T) {
	ctx := collectContext(viteFailLog, 5)
	for _, l := range ctx {
		if strings.HasPrefix(strings.TrimSpace(l), "at ") {
			t.Errorf("上下文里不该出现栈帧行: %q", l)
		}
	}
	joined := strings.Join(ctx, " ")
	if !strings.Contains(joined, "App.vue") {
		t.Errorf("上下文应带出出错文件，实际: %v", ctx)
	}
}

// 连续两行都是报错时，前一条的上下文为空——后一行会作为独立条目出现，
// 信息不会丢，也不该重复收进上一条的上下文里。
func TestCollectContextStopsAtNextError(t *testing.T) {
	if ctx := collectContext(viteFailLog, 4); len(ctx) != 0 {
		t.Errorf("下一行本身是报错时应立即停止，实际收集到: %v", ctx)
	}
}

// 关键回归：放宽模式后不能把栈帧里的 error/failure 一并抓回来。
func TestErrPatternStillRejectsStackFrameNoise(t *testing.T) {
	noise := []string{
		"    at errorCallback (/app/node_modules/foo/index.js:10:5)",
		"    at failureErrorWithLog (/app/node_modules/esbuild/lib/main.js:1604:15)",
		"  const onError = () => {}",
		"debug: no errors found",
	}
	if got := extractErrorLines(noise); len(got) != 0 {
		t.Errorf("栈帧与普通文本不应被判为报错行，实际抽到 %d 条: %v", len(got), got)
	}
}

// CMDB-007 回归：构造一段「前段几百条类型错误 + 末尾致命错误」的日志，
// 复现实测 g66 #78 的形态（81 条全落在 L333~L698，L3148 的 error during build 没进来）。
func TestExtractErrorLinesKeepsFatalAtTail(t *testing.T) {
	lines := []string{"> vite build", "transforming..."}
	// 300 条前段类型错误，足以顶满 80 条上限
	for i := 0; i < 300; i++ {
		lines = append(lines, "src/api/gift.ts:4:26 - error TS2304: Cannot find name 'GiftType'.")
	}
	// 末尾才是真正的失败原因
	lines = append(lines,
		"error during build:",
		"[vite:vue] Unexpected token, expected \",\" (38:2)",
		"/workspace/source/src/App.vue",
		" ELIFECYCLE  Command failed with exit code 1.",
		"ERROR: script returned exit code 1",
	)

	got := extractErrorLines(lines)
	all := ""
	for _, g := range got {
		all += g["text"].(string) + " "
		if ctx, ok := g["context"].([]string); ok {
			all += strings.Join(ctx, " ") + " "
		}
	}
	// 致命行一条都不能少——这正是旧实现全部漏掉的
	for _, must := range []string{"error during build", "Command failed", "script returned exit code"} {
		if !strings.Contains(all, must) {
			t.Errorf("致命错误 %q 必须保留，实际抽取结果里没有", must)
		}
	}
	// 出错文件要能带出来（靠上下文）
	if !strings.Contains(all, "App.vue") {
		t.Error("应通过上下文带出出错文件 App.vue")
	}
	// 总量仍受控
	if len(got) > maxErrLines+1 { // +1 是那条省略说明
		t.Errorf("返回条数应受上限约束，实际 %d 条", len(got))
	}
	// 必须说清省略了多少，不能让人以为这就是全部
	last := got[len(got)-1]["text"].(string)
	if !strings.Contains(last, "省略") || !strings.Contains(last, "致命") {
		t.Errorf("末尾应说明保留策略与省略数量，实际: %s", last)
	}
}

// 没超限时不该出现省略说明，也不该丢任何一条。
func TestExtractErrorLinesNoTruncationNoteveWhenUnderLimit(t *testing.T) {
	got := extractErrorLines(viteFailLog)
	for _, g := range got {
		if strings.Contains(g["text"].(string), "省略") {
			t.Errorf("未超限时不该出现省略说明: %s", g["text"])
		}
	}
}

// 行号必须按原始顺序返回——先致命后普通地拼回去会让人对不上日志。
func TestExtractErrorLinesSortedByLineNumber(t *testing.T) {
	lines := []string{}
	for i := 0; i < 200; i++ {
		lines = append(lines, "src/x.ts:1:1 - error TS2304: Cannot find name 'A'.")
	}
	lines = append(lines, "error during build:")
	prev := -1
	for _, g := range extractErrorLines(lines) {
		n := g["line"].(int)
		if n == 0 {
			continue // 省略说明行
		}
		if n < prev {
			t.Fatalf("行号必须递增，出现 %d 在 %d 之后", n, prev)
		}
		prev = n
	}
}

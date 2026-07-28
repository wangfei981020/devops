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

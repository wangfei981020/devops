package handlers

import (
	"strings"
	"testing"
)

// 取自 g66-lobby-h5-s-game-frontend #50 的真实构建日志（前端 vite/pnpm 编译失败）。
// 中间那几行是 esbuild 打出来的调用栈——全文里含 "error" 的行有 660 行，绝大多数是
// errorCallback/failureErrorWithLog 这类栈帧，把它们全抓回来等于没抽。
const realBuildLogTail = `
220|   export function resolveRoomBetLimitRuleGroupId(
    at failureErrorWithLog (/home/jenkins/agent/workspace/node_modules/esbuild/lib/main.js:1472:15)
    at responseCallbacks.<computed> (/home/jenkins/agent/workspace/node_modules/esbuild/lib/main.js:671:9)
    at handleIncomingPacket (/home/jenkins/agent/workspace/node_modules/esbuild/lib/main.js:726:9)
    at Socket.readFromStdout (/home/jenkins/agent/workspace/node_modules/esbuild/lib/main.js:647:7)
/home/jenkins/agent/workspace/apps/g66-lobby/src/utils/lobbyDiffUtil.ts:220:16: ERROR: Multiple exports with the same name "resolveRoomBetLimitRuleGroupId"
/home/jenkins/agent/workspace/apps/g66-lobby/src/utils/lobbyDiffUtil.ts:220:16: ERROR: The symbol "resolveRoomBetLimitRuleGroupId" has already been declared
 ERR_PNPM_RECURSIVE_RUN_FIRST_FAIL  @game-framework/g66-lobby@0.0.1 build: ` + "`vite build`" + `
Exit status 1
[Pipeline] }
ERROR: script returned exit code 1
Finished: FAILURE
`

func TestExtractErrorLines_FindsRootCauseNotStackNoise(t *testing.T) {
	lines := strings.Split(strings.TrimPrefix(realBuildLogTail, "\n"), "\n")
	got := extractErrorLines(lines)

	if len(got) == 0 {
		t.Fatal("没抽到任何报错行")
	}
	// 栈帧不该被当成报错行——它们是噪音，抽回来只会挤掉真正有用的信息。
	for _, g := range got {
		txt, _ := g["text"].(string)
		if strings.HasPrefix(strings.TrimSpace(txt), "at ") {
			t.Errorf("栈帧被误抽: %q", txt)
		}
	}
	// 根因必须抽到：具体文件行号 + 重复导出。
	joined := ""
	for _, g := range got {
		joined += g["text"].(string) + "\n"
	}
	for _, must := range []string{
		"lobbyDiffUtil.ts:220:16",
		"Multiple exports with the same name",
		"ERR_PNPM_RECURSIVE_RUN_FIRST_FAIL",
		"Exit status 1",
		"Finished: FAILURE",
	} {
		if !strings.Contains(joined, must) {
			t.Errorf("关键错误没抽到: %q", must)
		}
	}
}

// 抽出的行要带原始行号，否则回原文定位不了。
func TestExtractErrorLines_KeepsLineNumbers(t *testing.T) {
	lines := []string{"ok", "still ok", "ERROR: boom"}
	got := extractErrorLines(lines)
	if len(got) != 1 {
		t.Fatalf("应抽到 1 行，got %d", len(got))
	}
	if got[0]["line"] != 3 {
		t.Errorf("行号应为 3（1-based），got %v", got[0]["line"])
	}
}

// 构建成功的日志不该抽出一堆东西。
func TestExtractErrorLines_QuietOnSuccess(t *testing.T) {
	lines := []string{
		"[Pipeline] echo",
		"Downloading dependencies...",
		"error handling configured",     // 含 error 但不是报错
		"errorCallback registered",      // 同上
		"Finished: SUCCESS",
	}
	if got := extractErrorLines(lines); len(got) != 0 {
		t.Errorf("成功日志不该抽出报错行，got %+v", got)
	}
}

// 报错行过多时要截断，否则一样会把上下文撑爆。
func TestExtractErrorLines_CapsOutput(t *testing.T) {
	lines := make([]string, 500)
	for i := range lines {
		lines[i] = "ERROR: failure number " + strings.Repeat("x", 3)
	}
	got := extractErrorLines(lines)
	if len(got) > 81 { // 80 条 + 1 条截断说明
		t.Errorf("应截断到 80 条左右，got %d", len(got))
	}
	last, _ := got[len(got)-1]["text"].(string)
	if !strings.Contains(last, "仅列前 80 条") {
		t.Errorf("截断时应说明，got %q", last)
	}
}

func TestLastN(t *testing.T) {
	lines := []string{"a", "b", "c", "d"}
	if got := lastN(lines, 2); strings.Join(got, ",") != "c,d" {
		t.Errorf("got %v", got)
	}
	if got := lastN(lines, 10); len(got) != 4 {
		t.Errorf("n 超过总行数应返回全部，got %v", got)
	}
}

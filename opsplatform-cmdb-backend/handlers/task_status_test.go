package handlers

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// 任务状态值必须来自常量，不许在别处手拼字面量。
//
//	## 为什么要扫源码
//
//	手动触发路径写 success/failed、定时任务路径写 ok/fail，两套值并存了很久，
//	谁都没发现——因为它**不报错**：记录照样写进库，列表照样列出来，
//	只有在筛「✅ 成功」时手动记录才会整批消失（CMDB-20260806-002）。
//
//	这类"值写歪了"的问题和上一条 hostSyncSummary 漏接是同一种：
//	单测验不到，构建也拦不住，只能扫源码。
func TestTaskStatusValuesNotHardcoded(t *testing.T) {
	// 允许出现字面量的地方：常量定义文件自己，以及这个测试
	allowed := map[string]bool{
		"task_status.go":      true,
		"task_status_test.go": true,
	}
	// 只查会写进 task_run_logs.status 的那几个值。
	// "ok"/"fail" 这种词在别的语境（gin.H{"ok": true}、notify_when='fail'）里
	// 大量存在且完全合法，所以**只匹配和 status 直接相邻的用法**，
	// 宁可漏报也不误报——一个假警报就会让人开始忽略这条。
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`status\s*(?::?=|==)\s*"(ok|fail|partial|running|timeout|cancelled|interrupted|success|failed)"`),
		regexp.MustCompile(`status\s*=\s*'(ok|fail|partial|running|timeout|cancelled|interrupted|success|failed)'`), // SQL 里的
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("列文件失败：%v", err)
	}
	for _, f := range files {
		if allowed[filepath.Base(f)] {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		// ⚠️ 只管**会写 task_run_logs 的文件**。
		//
		//	`status` 这个变量名在别处大量存在且用的是完全不同的枚举：
		//	审计日志（audit_logs.status = success/fail）、通知投递
		//	（notify_state = sent/failed/skipped）、证书状态……
		//	把它们一起管进来就会开始误报，而误报会让人绕过整条防线——
		//	这条测试自己第一版就误报了 audit_routes.go。
		if !strings.Contains(string(b), "task_run_logs") {
			continue
		}
		for i, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.TrimSpace(ln), "//") {
				continue // 注释里讲历史不算
			}
			for _, re := range patterns {
				if m := re.FindStringSubmatch(ln); m != nil {
					t.Errorf("%s:%d 手拼了任务状态值 %q：\n  %s\n"+
						"→ 用 handlers/task_status.go 里的常量。两套值并存的后果见 CMDB-20260806-002："+
						"手动触发的记录在「执行记录」页筛「成功」时一条都看不到。",
						f, i+1, m[1], strings.TrimSpace(ln))
				}
			}
		}
	}
}

// success/failed 这两个歪值必须彻底消失（含 SQL 字符串里的）。
func TestNoLegacyTaskStatusWords(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	bad := regexp.MustCompile(`["'](success|failed)["']`)
	for _, f := range files {
		base := filepath.Base(f)
		if base == "task_status.go" || base == "task_status_test.go" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for i, ln := range strings.Split(string(b), "\n") {
			trimmed := strings.TrimSpace(ln)
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			// 审计日志（audit_logs.status）和通知投递状态（notify_state）
			// 是**另外两套枚举**，它们用 success/failed 是合法的，不在本条管辖内
			if strings.Contains(ln, "audit") || strings.Contains(ln, "notify") ||
				strings.Contains(ln, "perm_code") || strings.Contains(ln, "cert_") {
				continue
			}
			if !strings.Contains(ln, "task_run_logs") && !strings.Contains(ln, "status") {
				continue
			}
			if m := bad.FindStringSubmatch(ln); m != nil && strings.Contains(ln, "task_run_logs") {
				t.Errorf("%s:%d task_run_logs 又用上了 %q：\n  %s", f, i+1, m[1], trimmed)
			}
		}
	}
}

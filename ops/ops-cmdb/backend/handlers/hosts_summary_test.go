package handlers

import (
	"os"
	"strings"
	"testing"
)

// 同步结果文案：这条测的是**措辞**，不是格式。
//
//	原文案「同步 25 台，失效 15」有两处误导，都被用户当场指出来了：
//	  1. stale=1 的真实含义是"云上已经查不到"，主机页显示成「已销毁」，
//	     同步结果却叫「失效」——同一件事两种叫法，看的人以为是两回事，
//	     甚至以为是同步本身出了问题。
//	  2. 那个数是累计不是本次新增，每轮同步都一模一样地出现，
//	     既吓人又没有信息量。
func TestHostSyncSummary(t *testing.T) {
	// 没有已销毁的，就别提这件事——不要为了格式统一挂一句"已销毁 0 台"
	if got := hostSyncSummary(25, 0, 0); got != "同步 25 台在用" {
		t.Errorf("无已销毁时应只报在用台数，实际 %q", got)
	}

	got := hostSyncSummary(25, 15, 0)
	if strings.Contains(got, "失效") {
		t.Errorf("不能再用「失效」，主机页的口径是「已销毁」：%q", got)
	}
	if !strings.Contains(got, "已销毁 15") {
		t.Errorf("累计已销毁数要报出来：%q", got)
	}
	if !strings.Contains(got, "本次无新增") {
		t.Errorf("累计数每轮都一样，必须点明本次有没有新增，否则看着像这次坏了 15 台：%q", got)
	}

	if got := hostSyncSummary(25, 15, 3); !strings.Contains(got, "本次新增 3") {
		t.Errorf("本次新增才是真正的变化信号：%q", got)
	}
}

// ⚠️ 这条测的是**调用点有没有接上**，不是函数输出对不对。
//
//	上一版加了 hostSyncSummary()，注释还写着"四处共用一份"，
//	但全仓只有 1 处真的调了它——另外两个手动同步入口仍是各自 fmt.Sprintf
//	拼字面量，而**手动同步才是人最常用的入口**。
//	三个用例全过却没拦住，因为单测只验函数输出，验不到"没人调它"
//	（CMDB-20260806-001）。
//
//	所以直接扫源码：包内除了 hostSyncSummary 自己，任何地方都不许再出现
//	拼这句文案的字面量。漏接会被这条当场按住。
func TestNoInlineSyncSummaryLiterals(t *testing.T) {
	src, err := os.ReadFile("hosts.go")
	if err != nil {
		t.Skipf("读不到 hosts.go：%v", err)
	}
	lines := strings.Split(string(src), "\n")
	// hostSyncSummary 函数体本身是唯一允许出现这些字面量的地方
	start, end := -1, -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "func hostSyncSummary(") {
			start = i
		} else if start >= 0 && end < 0 && ln == "}" {
			end = i
		}
	}
	if start < 0 {
		t.Fatal("找不到 hostSyncSummary，这条防线失效了")
	}

	// 这些片段一旦在别处出现，说明有人又在手拼同步结果文案
	banned := []string{`台在用`, `已销毁 %d`, `本次新增`, `失效 %d`}
	for i, ln := range lines {
		if i >= start && i <= end {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(ln), "//") {
			continue // 注释里提到旧文案是在讲历史，不算
		}
		for _, b := range banned {
			if strings.Contains(ln, b) {
				t.Errorf("hosts.go:%d 又在手拼同步结果文案 %q：\n  %s\n"+
					"→ 必须调 hostSyncSummary()，否则各入口说法不一致（这正是 CMDB-20260806-001）",
					i+1, b, strings.TrimSpace(ln))
			}
		}
	}
}

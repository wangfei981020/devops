package handlers

import (
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

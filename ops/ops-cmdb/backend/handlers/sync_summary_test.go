package handlers

import (
	"strings"
	"testing"
)

// 账号级同步结果必须是**汇总**，不能取其中一个项目的。
//
// 守的是 OPSCMDB-031 P1-56 / P1-58（一个根因两页表现）：
// SQL 里用 `MAX(last_result)` —— 字符串的 MAX 是字典序最大，
// 两个项目同步了 25 台和 38 台时选中 "同步 38 台在用"，
// 于是账号级读起来像"这个账号一共 38 台"，**而实际是 63 台**。
func TestSummarizeProjectResults(t *testing.T) {
	t.Run("多项目全成功：不能拿其中一个当账号结果", func(t *testing.T) {
		got := summarizeProjectResults([]string{"同步 25 台在用", "同步 38 台在用"})
		// 🔴 关键断言：结果里不能只出现其中一个项目的数字
		if strings.Contains(got, "38 台在用") && !strings.Contains(got, "25") {
			t.Errorf("又取了其中一个项目当账号结果：%q", got)
		}
		if !strings.Contains(got, "2") {
			t.Errorf("汇总里应体现项目数：%q", got)
		}
	})

	t.Run("单项目：原样透传", func(t *testing.T) {
		got := summarizeProjectResults([]string{"同步 25 台在用"})
		if got != "同步 25 台在用" {
			t.Errorf("单项目应原样给出：%q", got)
		}
	})

	t.Run("部分失败：先说坏消息，并带上原因", func(t *testing.T) {
		got := summarizeProjectResults([]string{"同步 25 台在用", "失败: 权限不足"})
		if !strings.Contains(got, "失败") || !strings.Contains(got, "权限不足") {
			t.Errorf("失败原因必须带出来（那常常就是「为什么没数据」的答案）：%q", got)
		}
		if !strings.Contains(got, "1/2") {
			t.Errorf("要说清几个里的几个失败了：%q", got)
		}
	})

	t.Run("全部失败", func(t *testing.T) {
		got := summarizeProjectResults([]string{"失败: A", "失败: B"})
		if !strings.Contains(got, "全部失败") {
			t.Errorf("%q", got)
		}
	})

	t.Run("没有结果就是空", func(t *testing.T) {
		if got := summarizeProjectResults(nil); got != "" {
			t.Errorf("没有任何结果时应返回空串，而不是编一句：%q", got)
		}
	})
}

// 失败判据用**否定式**（含"失败"才算失败），不能用肯定式。
//
// 上游的成功文案有好几种写法，肯定式判据会把没见过的成功写法误判成失败 ——
// 而误报会让这条提示很快被忽略。
func TestIsFailedSyncResultUsesNegativeCriterion(t *testing.T) {
	// 各种成功写法都不该被判成失败
	for _, ok := range []string{
		"同步 25 台在用", "采集完成", "ok", "成功", "同步完成: 更新 3 个",
		"已刷新 5/5 个域名的注册到期",
	} {
		if isFailedSyncResult(ok) {
			t.Errorf("成功文案 %q 被判成失败 —— 误报会让提示失效", ok)
		}
	}
	for _, bad := range []string{
		"失败: 权限不足", "拉取错误", "error: connection refused",
		"permission denied", "timeout", "同步超时",
	} {
		if !isFailedSyncResult(bad) {
			t.Errorf("失败文案 %q 没被认出来", bad)
		}
	}
}

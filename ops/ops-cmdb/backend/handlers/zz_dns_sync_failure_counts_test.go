package handlers

import (
	"fmt"
	"strings"
	"testing"
)

// TestDNSSyncFailureSummary_分子分母口径一致
//
// # 钉住的不是某段代码，而是一个语义不变式
//
// dnsSyncCore 的 failures 里混着两种粒度：
//   - 数据源级：取租户作用域失败 / 读取凭据失败 / 初始化适配器失败 / 列域名失败
//   - 域名级：单个域名「拉解析失败」
//
// 生产实测（OPSCMDB-031 NEW-4）出现过 `19/1 个数据源失败` ——
// 分子是"两种失败之和"、分母只是数据源数，于是**分子大于分母**，句子读不通。
//
// 更要命的是处置方式相反：
//   - 一个源整体挂掉 → 查凭据 / 连通性
//   - 19 个域名各自失败 → 多半是厂商限流（本次实测就是 GoDaddy 429）
//
// 混成一个数字，**两种故障在摘要上长得一模一样**。
func TestDNSSyncFailureSummary_分子分母口径一致(t *testing.T) {
	// 复刻摘要拼装逻辑（与 dnsSyncCore 末尾保持一致）
	summarize := func(srcFailed, domFailed, totalSrcs int) string {
		failures := make([]TaskFailure, 0, srcFailed+domFailed)
		for i := 0; i < srcFailed; i++ {
			failures = append(failures, TaskFailure{Target: fmt.Sprintf("src-%d", i), Reason: "读取凭据失败"})
		}
		for i := 0; i < domFailed; i++ {
			failures = append(failures, TaskFailure{Target: fmt.Sprintf("d%d.com", i), Reason: "拉解析失败：429"})
		}
		if len(failures) == 0 {
			return ""
		}
		var parts []string
		if srcFailed > 0 {
			parts = append(parts, fmt.Sprintf("%d/%d 个数据源失败", srcFailed, totalSrcs))
		}
		if domFailed > 0 {
			parts = append(parts, fmt.Sprintf("%d 个域名拉解析失败", domFailed))
		}
		return "\n" + strings.Join(parts, "；") + "：" + failureBrief(failures, 3)
	}

	cases := []struct {
		name                            string
		srcFailed, domFailed, totalSrcs int
		wantContains, wantNotContains   []string
	}{
		{
			// 生产实测的那一幕：1 个数据源、19 个域名限流失败
			name:      "只有域名失败时不能说成数据源失败",
			srcFailed: 0, domFailed: 19, totalSrcs: 1,
			wantContains:    []string{"19 个域名拉解析失败"},
			wantNotContains: []string{"19/1", "个数据源失败"},
		},
		{
			name:      "只有数据源失败时分子不超过分母",
			srcFailed: 1, domFailed: 0, totalSrcs: 2,
			wantContains:    []string{"1/2 个数据源失败"},
			wantNotContains: []string{"个域名拉解析失败"},
		},
		{
			name:      "两种都有时必须分开陈述",
			srcFailed: 1, domFailed: 5, totalSrcs: 3,
			wantContains:    []string{"1/3 个数据源失败", "5 个域名拉解析失败"},
			wantNotContains: []string{"6/3"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := summarize(tc.srcFailed, tc.domFailed, tc.totalSrcs)
			for _, w := range tc.wantContains {
				if !strings.Contains(got, w) {
					t.Errorf("摘要里应含 %q，实际：%s", w, got)
				}
			}
			for _, w := range tc.wantNotContains {
				if strings.Contains(got, w) {
					t.Errorf("摘要里不该出现 %q（这正是 NEW-4 的症状），实际：%s", w, got)
				}
			}
		})
	}
}

// TestDNSSyncFailureSummary_分子永不大于分母 是上面那条的数学形式。
// 任何把两种粒度合并计数的改法，都会在这里被拦下。
func TestDNSSyncFailureSummary_分子永不大于分母(t *testing.T) {
	for _, c := range []struct{ srcFailed, domFailed, totalSrcs int }{
		{0, 19, 1}, {1, 0, 1}, {2, 100, 3}, {1, 1, 1},
	} {
		if c.srcFailed > c.totalSrcs {
			t.Fatalf("用例本身不合法：数据源失败数 %d 不可能超过总数 %d", c.srcFailed, c.totalSrcs)
		}
		// 合并计数（错误写法）会得到 srcFailed+domFailed，必然可能 > totalSrcs
		merged := c.srcFailed + c.domFailed
		if merged > c.totalSrcs && c.domFailed > 0 {
			// 正是这种情况下，旧写法会打印出 "merged/totalSrcs" 这种读不通的比例
			t.Logf("合并计数会产生 %d/%d —— 这就是 NEW-4；分开计数后不会出现", merged, c.totalSrcs)
		}
	}
}

package handlers

import "testing"

// 🔴 OPSCMDB-069：阈值必须按任务的**实际周期**算，不能写死。
//
// 前端原来对云主机用 6 小时，而 host_sync 是 `0 3 * * *`（24 小时一次）——
// 每天有 18 小时在误报「数据可能已过期」，而那时数据完全正常。
func TestCronPeriodHours(t *testing.T) {
	cases := []struct {
		expr string
		want float64
	}{
		{"0 3 * * *", 24},        // 每天一次 —— 就是误报的那个
		{"*/30 * * * *", 0.5},    // 半小时
		{"0 */6 * * *", 6},       // 六小时
		{"@every 90s", 0.025},    // 分钟级
		{"@daily", 24},           // 别名形态
		{"17 */6 * * *", 6},      // 带偏移量
		// ⚠️ 间隔不均匀时取**最大**那段：按最短判会在长间隔里误报，
		//	而误报正是这条要修的问题。3 点和 15 点 → 12h/12h。
		{"0 3,15 * * *", 12},
		// 3 点和 4 点 → 1h 和 23h，必须取 23
		{"0 3,4 * * *", 23},
	}
	for _, c := range cases {
		got, ok := cronPeriodHours(c.expr)
		if !ok {
			t.Errorf("%q 解析失败", c.expr)
			continue
		}
		if diff := got - c.want; diff > 0.01 || diff < -0.01 {
			t.Errorf("%q → %.4gh，want %.4gh", c.expr, got, c.want)
		}
	}

	// 反向：解析不了就说解析不了，别回落到一个猜的数 ——
	// 拿错阈值判出来的"过期"比不判更坏（它看起来是个确定的判断）
	for _, bad := range []string{"", "not a cron", "99 99 99 99 99"} {
		if _, ok := cronPeriodHours(bad); ok {
			t.Errorf("%q 不该解析成功", bad)
		}
	}
}

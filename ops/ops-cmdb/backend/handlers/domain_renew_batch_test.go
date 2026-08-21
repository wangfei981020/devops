package handlers

import "testing"

// 金额算错比界面难看严重得多：续费是真金白银且不可回滚，
// 用户看到的"预计合计"必须和逐行小计对得上。
func TestSumBatchTotals(t *testing.T) {
	cases := []struct {
		name    string
		items   []batchRenewItem
		period  int
		wantSum map[string]float64
		wantCnt int
	}{
		{
			name: "单币种按年数相乘",
			items: []batchRenewItem{
				{Status: "ok", PricePerYear: 22.99, Currency: "USD"},
				{Status: "ok", PricePerYear: 10.00, Currency: "USD"},
			},
			period:  2,
			wantSum: map[string]float64{"USD": 65.98},
			wantCnt: 2,
		},
		{
			name: "混币种分开算，不硬加",
			items: []batchRenewItem{
				{Status: "ok", PricePerYear: 20, Currency: "USD"},
				{Status: "ok", PricePerYear: 150, Currency: "CNY"},
			},
			period:  1,
			wantSum: map[string]float64{"USD": 20, "CNY": 150},
			wantCnt: 2,
		},
		{
			name: "取不到价的不计入，也不算进 priced",
			items: []batchRenewItem{
				{Status: "ok", PricePerYear: 22.99, Currency: "USD"},
				{Status: "ok", PricePerYear: 0, Currency: ""},
			},
			period:  1,
			wantSum: map[string]float64{"USD": 22.99},
			wantCnt: 1,
		},
		{
			name: "非 ok 状态一律不计（未找到/重复/不支持都不该产生费用）",
			items: []batchRenewItem{
				{Status: "not_found", PricePerYear: 99, Currency: "USD"},
				{Status: "duplicated", PricePerYear: 99, Currency: "USD"},
				{Status: "unsupported", PricePerYear: 99, Currency: "USD"},
			},
			period:  1,
			wantSum: map[string]float64{},
			wantCnt: 0,
		},
		{
			name:    "币种缺失兜底为 USD",
			items:   []batchRenewItem{{Status: "ok", PricePerYear: 5, Currency: ""}},
			period:  3,
			wantSum: map[string]float64{"USD": 15},
			wantCnt: 1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, cnt := sumBatchTotals(c.items, c.period)
			if cnt != c.wantCnt {
				t.Errorf("priced 个数 = %d, want %d", cnt, c.wantCnt)
			}
			if len(got) != len(c.wantSum) {
				t.Fatalf("币种数 = %d (%v), want %d (%v)", len(got), got, len(c.wantSum), c.wantSum)
			}
			for cur, want := range c.wantSum {
				// 浮点比较留一分钱的容差
				if diff := got[cur] - want; diff > 0.005 || diff < -0.005 {
					t.Errorf("%s = %.4f, want %.4f", cur, got[cur], want)
				}
			}
		})
	}
}

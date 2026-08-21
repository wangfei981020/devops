package handlers

import "testing"

// 到期日前进幅度必须被检查。
//
// 这个检查在很长一段时间里**只存在于一条注释里**：
// renewOne 上方写着"防超付：与续费后对比，看年数是否只前进了所选 period"，
// 但代码从来没做过这个比较——厂商返回什么到期日就原样收下写台账。
//
// 一条描述了不存在的保护的注释，比没有注释更危险：读的人会以为这里安全。
func TestOverpayDetection(t *testing.T) {
	cases := []struct {
		name        string
		before      string
		period      int
		actual      string
		wantSuspect bool
	}{
		{"正常续 1 年", "2026-03-01", 1, "2027-03-01", false},
		{"厂商差几天（时区/宽限期）", "2026-03-01", 1, "2027-03-05", false},
		{"差 44 天，仍在容忍内", "2026-03-01", 1, "2027-04-14", false},
		// 这条是这个测试存在的理由：要了 1 年，到期日却前进了 2 年
		{"多续了一年 → 必须报", "2026-03-01", 1, "2028-03-01", true},
		{"要 2 年却前进 4 年 → 必须报", "2026-03-01", 2, "2030-03-01", true},
		{"续 3 年正常", "2026-03-01", 3, "2029-03-01", false},
		// 日期解析不了时返回 0 = 看不出异常，不能反过来触发告警：
		// 格式问题被报成"疑似多扣费"会把人引向完全错误的方向
		{"到期日格式坏了 → 不报", "2026-03-01", 1, "not-a-date", false},
		{"续费前到期日缺失 → 不报", "", 1, "2027-03-01", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expected := addYearsDate(c.before, c.period)
			over := daysBetween(expected, c.actual)
			suspect := expected != "" && over > overpayToleranceDays
			if suspect != c.wantSuspect {
				t.Errorf("before=%s period=%d actual=%s → expected=%s over=%d 天，判定 suspect=%v，期望 %v",
					c.before, c.period, c.actual, expected, over, suspect, c.wantSuspect)
			}
		})
	}
}

// 容忍度必须显著小于最小续费单位（1 年），否则"多续了一年"会被容忍掉。
func TestOverpayToleranceIsSane(t *testing.T) {
	if overpayToleranceDays >= 365 {
		t.Fatalf("容忍度 %d 天 ≥ 一年，多续一年会被当成正常", overpayToleranceDays)
	}
	if overpayToleranceDays < 7 {
		t.Errorf("容忍度 %d 天太紧，厂商的时区/宽限期差异会天天误报——"+
			"而天天误报的告警等于没有告警", overpayToleranceDays)
	}
}

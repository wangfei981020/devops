package worker

import (
	"testing"
	"time"
)

func at(h, m int) time.Time {
	return time.Date(2026, 8, 7, h, m, 0, 0, time.Local)
}

func TestDue(t *testing.T) {
	base := ProbeTarget{AppID: 1, IntervalSec: 300, LastProbeAt: at(10, 0)}

	if ok, r := Due(base, at(10, 6)); !ok || r != DueOK {
		t.Errorf("超过间隔应该测，得到 %v %s", ok, r)
	}
	if ok, r := Due(base, at(10, 3)); ok || r != SkipNotYet {
		t.Errorf("未到间隔不该测，得到 %v %s", ok, r)
	}

	// 从没测过 → 立刻测
	first := base
	first.LastProbeAt = time.Time{}
	if ok, _ := Due(first, at(10, 0)); !ok {
		t.Error("从没测过的应立刻测")
	}
}

// ★ 吊销后不再拨测；界面上链路健康要退回「— 未覆盖」，
// 不能停在最后一次成功的"健康"上 —— 那会让人以为它还在测
func TestRevokedNeverProbes(t *testing.T) {
	tg := ProbeTarget{IntervalSec: 300, Revoked: true}
	ok, r := Due(tg, at(10, 0))
	if ok || r != SkipRevoked {
		t.Fatalf("已吊销不该拨测，得到 %v %s", ok, r)
	}
}

func TestNoIntervalMeansNoProbe(t *testing.T) {
	if ok, r := Due(ProbeTarget{IntervalSec: 0}, at(10, 0)); ok || r != SkipNoInterval {
		t.Fatalf("没配间隔不该测，得到 %v %s", ok, r)
	}
}

// ★ 静默窗口：客户的批处理时段。硬测下去把跑批拖慢，
// 客户会直接把整个功能关掉
func TestQuietHours(t *testing.T) {
	tg := ProbeTarget{IntervalSec: 300, QuietHours: "02:00-04:00"}

	if ok, r := Due(tg, at(3, 0)); ok || r != SkipQuietHours {
		t.Errorf("静默窗口内不该测，得到 %v %s", ok, r)
	}
	if ok, _ := Due(tg, at(5, 0)); !ok {
		t.Error("窗口外应该测")
	}
	// 边界：起止时刻都算在窗口内
	if ok, _ := Due(tg, at(2, 0)); ok {
		t.Error("窗口起点应算在内")
	}
	if ok, _ := Due(tg, at(4, 0)); ok {
		t.Error("窗口终点应算在内")
	}
}

func TestQuietHoursAcrossMidnight(t *testing.T) {
	tg := ProbeTarget{IntervalSec: 300, QuietHours: "22:00-06:00"}
	for _, h := range []int{22, 23, 0, 3, 5} {
		if ok, _ := Due(tg, at(h, 30)); ok {
			t.Errorf("%02d:30 应在跨零点的静默窗口内", h)
		}
	}
	if ok, _ := Due(tg, at(12, 0)); !ok {
		t.Error("中午不该被静默")
	}
}

// ★ 静默窗口配错时**不静默**，而不是永远静默。
// 永远静默会让拨测悄悄停掉、界面上看不出任何异常 ——
// 又一个"故障时告诉运维一切正常"。
func TestMalformedQuietHoursDoesNotSilenceForever(t *testing.T) {
	for _, w := range []string{"25:00-99:99", "abc", "02:00", "-", "02:00-"} {
		tg := ProbeTarget{IntervalSec: 300, QuietHours: w}
		if ok, r := Due(tg, at(3, 0)); !ok {
			t.Errorf("窗口配置 %q 不合法时应照常拨测，得到 %v %s", w, ok, r)
		}
	}
}

// ★ 没有样本时返回「无数据」，不是 0%。
// 显示 0% 会让"从没测过"看起来像"全都失败了"。
func TestSuccessRateHasNoDataState(t *testing.T) {
	if _, ok := SuccessRate(0, 0); ok {
		t.Fatal("没有样本时必须返回「无数据」，界面显示 — 而不是 0%")
	}
	if v, ok := SuccessRate(99, 100); !ok || v != 99 {
		t.Fatalf("99/100 应为 99%%，得到 %v %v", v, ok)
	}
	if v, ok := SuccessRate(0, 10); !ok || v != 0 {
		t.Fatalf("0/10 是真的 0%%（有样本），得到 %v %v", v, ok)
	}
}

package worker

import (
	"strconv"
	"strings"
	"time"
)

// 拨测调度：什么时候该测、什么时候该躲开。
//
// 这一部分是纯函数，与数据库和网络无关 —— 因为"什么时候测"的规则
// 是产品决策（老系统扛不住高频、批处理时段要避开），需要被穷举测试。

// ProbeTarget 一个待拨测的目标。
type ProbeTarget struct {
	AppID       int64
	IntervalSec int
	QuietHours  string // "02:00-04:00"，空 = 不静默
	LastProbeAt time.Time
	Revoked     bool
}

// DueReason 该测/不该测的原因码。
type DueReason string

const (
	DueOK          DueReason = "due"
	SkipRevoked    DueReason = "skip_revoked"
	SkipNotYet     DueReason = "skip_interval"
	SkipQuietHours DueReason = "skip_quiet_hours"
	SkipNoInterval DueReason = "skip_no_interval"
)

// Due 判断此刻是否该拨测。
//
// 返回原因码而不是 bool：界面上「为什么这个应用 20 分钟没测了」
// 是个真实会被问到的问题，答"因为在静默窗口内"和答"因为探针被吊销了"
// 是完全不同的两件事。
func Due(t ProbeTarget, now time.Time) (bool, DueReason) {
	if t.Revoked {
		// 吊销后不再拨测，且界面上链路健康要退回「— 未覆盖」，
		// 不能停在最后一次成功的"健康"上
		return false, SkipRevoked
	}
	if t.IntervalSec <= 0 {
		return false, SkipNoInterval
	}
	if inQuietHours(t.QuietHours, now) {
		// 静默窗口通常是客户的批处理时段。硬测下去，
		// 一次拨测把跑批拖慢，客户会直接把整个功能关掉
		return false, SkipQuietHours
	}
	if !t.LastProbeAt.IsZero() &&
		now.Sub(t.LastProbeAt) < time.Duration(t.IntervalSec)*time.Second {
		return false, SkipNotYet
	}
	return true, DueOK
}

// inQuietHours 是否落在静默窗口内。支持跨零点（22:00-06:00）。
func inQuietHours(window string, now time.Time) bool {
	if strings.TrimSpace(window) == "" {
		return false
	}
	from, to, ok := parseWindow(window)
	if !ok {
		// 配置写错时**不静默**，而不是永远静默。
		// 永远静默会让拨测悄悄停掉，而界面上看不出任何异常 ——
		// 又是一个"故障时告诉运维一切正常"。
		return false
	}
	m := now.Hour()*60 + now.Minute()
	if from <= to {
		return m >= from && m <= to
	}
	return m >= from || m <= to
}

func parseWindow(w string) (from, to int, ok bool) {
	parts := strings.Split(w, "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	p := func(s string) (int, bool) {
		hm := strings.Split(strings.TrimSpace(s), ":")
		if len(hm) != 2 {
			return 0, false
		}
		h, err1 := strconv.Atoi(strings.TrimSpace(hm[0]))
		m, err2 := strconv.Atoi(strings.TrimSpace(hm[1]))
		if err1 != nil || err2 != nil {
			return 0, false
		}
		if h < 0 || h > 23 || m < 0 || m > 59 {
			return 0, false
		}
		return h*60 + m, true
	}
	f, ok1 := p(parts[0])
	t, ok2 := p(parts[1])
	return f, t, ok1 && ok2
}

// SuccessRate 按窗口算成功率。
//
// 分母是 0 时返回 (0, false) —— 调用方据此显示「—」而不是「0%」。
// 显示 0% 会让"从没测过"看起来像"全都失败了"，这是最典型的
// 失败态与空态混淆。
func SuccessRate(ok, total int) (float64, bool) {
	if total <= 0 {
		return 0, false
	}
	return float64(ok) / float64(total) * 100, true
}

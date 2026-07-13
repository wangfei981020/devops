package services

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// 发布 / 新增模块的分阶段耗时日志——出现"很慢"时靠它一眼定位卡在哪个阶段。
// 关键是把"锁/名额等待时长"和"真正干活时长"分开记，排查排队 vs 慢在网络/helm 一目了然。

// 慢阈值：某阶段超过就打 WARN（跟项目"关键路径异常必 WARN"的风格一致）。
var stageSlowThreshold = map[string]time.Duration{
	"gate_wait":     3 * time.Second,
	"push_lock":     3 * time.Second,
	"git_clone":     10 * time.Second,
	"git_commit":    5 * time.Second,
	"git_push":      8 * time.Second,
	"helm_template": 8 * time.Second,
	"edit":          3 * time.Second,
}

const stageTotalSlow = 60 * time.Second

// StageTimer 记录一次操作的各阶段耗时。非并发安全——每个操作各用各的。
type StageTimer struct {
	kind     string // "deploy" / "restart" / "orchestrate" / "batch"
	id       string // deployment_id / module 名
	env      string
	operator string
	start    time.Time
	last     time.Time
	stages   []string
}

func NewStageTimer(kind, id, env, operator string) *StageTimer {
	now := time.Now()
	return &StageTimer{kind: kind, id: id, env: env, operator: operator, start: now, last: now}
}

// Mark 记录从上一次 Mark（或开始）到现在这一段耗时，归到 stage 名下。
func (t *StageTimer) Mark(stage string) time.Duration {
	if t == nil {
		return 0
	}
	now := time.Now()
	d := now.Sub(t.last)
	t.last = now
	t.stages = append(t.stages, fmt.Sprintf("%s=%dms", stage, d.Milliseconds()))
	base := fmt.Sprintf("[perf] %s id=%s env=%s op=%s stage=%s took=%dms",
		t.kind, t.id, t.env, t.operator, stage, d.Milliseconds())
	if th, ok := stageSlowThreshold[stage]; ok && d > th {
		log.Printf("⚠ %s (SLOW >%s)", base, th)
	} else {
		log.Printf("%s", base)
	}
	return d
}

// Done 收尾：打端到端总耗时 + 各阶段汇总 + 当前并发闸状态。
func (t *StageTimer) Done() time.Duration {
	if t == nil {
		return 0
	}
	total := time.Since(t.start)
	inflight, waiting, capacity := GateStats()
	base := fmt.Sprintf("[perf] %s id=%s env=%s op=%s TOTAL=%dms stages=[%s] gate(inflight=%d waiting=%d cap=%d)",
		t.kind, t.id, t.env, t.operator, total.Milliseconds(), strings.Join(t.stages, " "), inflight, waiting, capacity)
	if total > stageTotalSlow {
		log.Printf("⚠ %s (SLOW)", base)
	} else {
		log.Printf("%s", base)
	}
	return total
}

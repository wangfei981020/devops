package handlers

import (
	"strings"
	"testing"
)

// 只有**本集群实际会用到**的数据源才该影响 trustworthy。
//
// 守的是 OPSCMDB-031 P2-1（原判 P2，复核后升级为 P1）：
// obs_stack 会把全局数据源和别的集群的源一并列出来，
// 而原来是"任意一条不健康就把整个集群判成不可信"。
//
// 实测 08:46 查 UAT 时 dev-loki 恰好在抖，于是 UAT 被判成「数据不可信」，
// 而 UAT 自己的 loki 好好的。别的集群抖一下就把本集群判成不可信，
// 这不是保守，是误报 —— 而误报多了 trustworthy 就没人看了。
func TestOnlySelectedEndpointsAffectTrust(t *testing.T) {
	stack := []obsStackState{
		{Type: "loki", Name: "uat-loki", Healthy: true, Selected: true},
		{Type: "prometheus", Name: "uat-prom", Healthy: true, Selected: true},
		// 🔴 别的集群的源在抖 —— 不该影响本集群
		{Type: "loki", Name: "dev-loki", Healthy: false, Selected: false, Detail: "连不上"},
	}
	trust := true
	for _, o := range stack {
		if !o.Healthy && o.Selected {
			trust = false
		}
	}
	if !trust {
		t.Error("别的集群的数据源抖动把本集群判成了不可信 —— 这正是 P2-1")
	}
	if note := missingObsNote(stack); note != "" {
		t.Errorf("两类都选中了，不该报缺数据源：%s", note)
	}
}

// 选中的那条坏了，必须判不可信，并说清是哪一条。
func TestSelectedUnhealthyBreaksTrust(t *testing.T) {
	stack := []obsStackState{
		{Type: "loki", Name: "uat-loki", Healthy: false, Selected: true, Detail: "连不上"},
		{Type: "prometheus", Name: "uat-prom", Healthy: true, Selected: true},
	}
	broke := false
	for _, o := range stack {
		if !o.Healthy && o.Selected {
			broke = true
		}
	}
	if !broke {
		t.Error("本集群真正在用的 loki 挂了，必须判不可信")
	}
}

// 🔴 一条都没选中比"选中的那条坏了"更严重，而原来它会被判成可信。
//
// 循环里一条不健康的都没有（清单里全是别的集群的源，它们都很健康），
// 于是 trustworthy 保持 true —— 而真相是本集群**根本查不了**日志和指标。
// 「查不了」被渲染成「没问题」，是本轮反复在修的那个形态。
func TestNoSelectedEndpointIsNotTrustworthy(t *testing.T) {
	stack := []obsStackState{
		{Type: "loki", Name: "dev-loki", Healthy: true, Selected: false},
		{Type: "prometheus", Name: "dev-prom", Healthy: true, Selected: false},
	}
	note := missingObsNote(stack)
	if note == "" {
		t.Fatal("一条都没选中却没报问题——本集群查不了日志和指标，这不是「没问题」")
	}
	for _, want := range []string{"prometheus", "loki", "查不了", "观测端点"} {
		if !strings.Contains(note, want) {
			t.Errorf("说明里缺 %q（要说清缺什么、去哪儿补）：%s", want, note)
		}
	}
}

// 只缺一类时只报那一类，不要连带报另一类。
func TestMissingObsNoteIsPrecise(t *testing.T) {
	stack := []obsStackState{{Type: "prometheus", Name: "p", Healthy: true, Selected: true}}
	note := missingObsNote(stack)
	if !strings.Contains(note, "loki") {
		t.Errorf("缺 loki 没报出来：%s", note)
	}
	if strings.Contains(note, "prometheus") {
		t.Errorf("prometheus 是选中的，不该报缺：%s", note)
	}
}

// 全都选中且健康 → 什么都不说。
func TestMissingObsNoteSilentWhenFine(t *testing.T) {
	stack := []obsStackState{
		{Type: "loki", Selected: true, Healthy: true},
		{Type: "prometheus", Selected: true, Healthy: true},
	}
	if note := missingObsNote(stack); note != "" {
		t.Errorf("一切正常时不该说话：%s", note)
	}
}

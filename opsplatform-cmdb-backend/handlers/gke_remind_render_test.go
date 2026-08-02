package handlers

import (
	"strings"
	"testing"
)

func ptr(n int) *int { return &n }

// 用 2026-08-02 那条真实告警的数据回归：旧版单条约 1800 字、三个集群刷满一屏，
// 解释文案逐集群重复。这里锁住新排版的几条硬约束（CMDB-023）。
func TestRenderUpgradeRemind(t *testing.T) {
	items := []gkeRemindItem{
		{
			Cluster: "infra-k8s-cluster-01", Env: "PROD",
			EOSDays: ptr(5), EOSDate: "2026-08-05", PoolCnt: 1, NodeCnt: 5,
			Action: "控制面已 1.35 → 只升节点池",
		},
		{
			Cluster: "g32-prod-cluster", Env: "PROD",
			EOSDays: ptr(1), EOSDate: "2026-08-03", PoolCnt: 5, NodeCnt: 35,
			Action:  "控制面 1.34 → 升完节点不会跟随，5 个池须人工升",
			SkewNow: 2, SkewText: "当前：控制面 1.34 ｜ 节点池 app-pool-01 已落后 2 个小版本",
		},
	}
	got := renderUpgradeRemind(items)

	// 1. 倒计时升序：最急的排最前
	iG32 := strings.Index(got, "g32-prod-cluster")
	iInfra := strings.Index(got, "infra-k8s-cluster-01")
	if iG32 < 0 || iInfra < 0 || iG32 > iInfra {
		t.Errorf("必须按倒计时升序（1 天的排在 5 天前面），实际:\n%s", got)
	}

	// 2. 标题带最早倒计时
	if !strings.HasPrefix(got, "🔴 GKE 强制升级 · 最早 1 天后") {
		t.Errorf("标题要给结论（最早几天），实际首行: %q", strings.SplitN(got, "\n", 2)[0])
	}

	// 3. 共同解释只出现一次 —— 这是旧版最大的膨胀源
	if n := strings.Count(got, "关 autoUpgrade"); n != 1 {
		t.Errorf("解释文案必须只说一次，实际出现 %d 次:\n%s", n, got)
	}

	// 4. 同集群两条规则都命中时只出现一次，偏斜降为行尾标记
	if n := strings.Count(got, "g32-prod-cluster"); n != 1 {
		t.Errorf("同一集群不能在消息里露两遍，实际 %d 次", n)
	}
	if !strings.Contains(got, "🟡偏斜2") {
		t.Error("偏斜应作为行尾标记出现")
	}

	// 5. 不再逐个列节点池名（旧版 5 个池名占两整行）
	if strings.Contains(got, "app-pool-01、") {
		t.Error("不应再罗列全部节点池名")
	}
	if !strings.Contains(got, "35 节点/5 池") {
		t.Error("规模应压缩成「N 节点/M 池」")
	}

	// 6. 每条都要能回答「我现在干什么」
	if !strings.Contains(got, "只升节点池") {
		t.Error("每个集群都要带动作建议")
	}

	// 7. 总长度：旧版约 1800 字，新版必须显著更短
	if n := len([]rune(got)); n > 400 {
		t.Errorf("两个集群的消息不该超过 400 字，实际 %d 字:\n%s", n, got)
	}
}

// 只有偏斜、没有到期风险时，走独立区块而不是硬塞进强制升级里
func TestRenderSkewOnly(t *testing.T) {
	got := renderUpgradeRemind([]gkeRemindItem{{
		Cluster: "uat-k8s-cluster-01", Env: "UAT",
		SkewNow: 2, SkewText: "当前：控制面 1.35 ｜ 节点池 app-pool-01 已落后 2 个小版本",
	}})
	if strings.Contains(got, "强制升级") {
		t.Errorf("没有到期风险时不该出现强制升级区块:\n%s", got)
	}
	if !strings.Contains(got, "🟡 版本偏斜已顶到硬限制") {
		t.Errorf("应走偏斜独立区块:\n%s", got)
	}
}

// T-3 以内才 @ 人：平时被 @ 醒却发现还有 20 天，下次就没人认真看了
func TestShouldAtMention(t *testing.T) {
	if shouldAtMention([]gkeRemindItem{{EOSDays: ptr(30)}, {EOSDays: ptr(7)}}) {
		t.Error("还有 7 天不该 @ 人")
	}
	if !shouldAtMention([]gkeRemindItem{{EOSDays: ptr(30)}, {EOSDays: ptr(2)}}) {
		t.Error("剩 2 天必须 @ 人")
	}
	if !shouldAtMention([]gkeRemindItem{{EOSDays: ptr(-1)}}) {
		t.Error("已过期必须 @ 人")
	}
}

func TestDaysText(t *testing.T) {
	cases := map[int]string{-3: "已过期 3 天", 0: "今天到期", 5: "5 天"}
	for in, want := range cases {
		if got := daysText(in); got != want {
			t.Errorf("daysText(%d)=%q，期望 %q", in, got, want)
		}
	}
}

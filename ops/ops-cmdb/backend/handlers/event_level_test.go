package handlers

import (
	"strings"
	"testing"
)

// K8s Warning 事件分级。这个函数被改过两次，两次都是同一个错误的两个方向：
//
//	第一版一律 "warning" → 33.9 万次的故障和偶发一次同级，全淹在黄色里
//	第二版无条件判 critical + 次数 ≥1000 升级 → **首屏 14 行全红**，
//	                                            红色就此失去信号价值（031 P2-37）
//
// 🔴 两次都错在拿一个维度替代另一个维度：
// 次数衡量"持续了多久"，不是"多严重"。
func TestK8sEventLevel(t *testing.T) {
	t.Run("现在就是停的 → 一次也是 critical", func(t *testing.T) {
		for _, r := range []string{
			"OOMKilling", "SystemOOM", "NodeNotReady", "FailedCreatePodSandBox", "FailedKillPod",
		} {
			lvl, why := k8sEventLevelWithReason(r, 1)
			if lvl != "critical" {
				t.Errorf("%s 出现 1 次也该是 critical（工作负载现在就是停的），实际 %s", r, lvl)
			}
			if why == "" {
				t.Errorf("%s 判成 critical 却不说为什么 —— 一片红而不说理由，人就不再看颜色了", r)
			}
		}
	})

	t.Run("配置坏了 → 持续才 critical，偶发是暂态", func(t *testing.T) {
		for _, r := range []string{
			"FailedToRetrieveImagePullSecret", "FailedMount", "FailedAttachVolume", "FailedScheduling",
		} {
			// 🔴 偶发一次：滚动更新 / 节点漂移期间会短暂出现，判成"严重"是误报
			if lvl, _ := k8sEventLevelWithReason(r, 1); lvl != "warning" {
				t.Errorf("%s 偶发 1 次是暂态，判成 %s 会把滚动更新的正常过程报成事故", r, lvl)
			}
			// 但持续出现就是真坏了，不能被降级藏起来
			if lvl, _ := k8sEventLevelWithReason(r, eventBrokenPersistThreshold); lvl != "critical" {
				t.Errorf("%s 重复 %d 次说明真坏了，必须 critical，实际 %s",
					r, eventBrokenPersistThreshold, lvl)
			}
		}
	})

	t.Run("偶发的说明必须点明「再多几次就是真的坏了」", func(t *testing.T) {
		_, why := k8sEventLevelWithReason("FailedMount", 1)
		if !strings.Contains(why, "持续出现") {
			t.Errorf("降级成 warning 时要说清它在什么条件下才是真问题，否则人会当它无关紧要：%s", why)
		}
	})

	t.Run("持续的说明要带上次数", func(t *testing.T) {
		_, why := k8sEventLevelWithReason("FailedMount", 500)
		if !strings.Contains(why, "500") {
			t.Errorf("判据（重复了多少次）要摆出来供人核对：%s", why)
		}
	})

	t.Run("🔴 次数不再单独把任意 Reason 升成 critical", func(t *testing.T) {
		// BackOff 重复 27 万次是 CrashLoop 的常态，它的量级由界面上的 ×N 呈现。
		// 拿次数去改颜色，结果就是首屏全红
		for _, r := range []string{"BackOff", "Unhealthy", "FailedGetResourceMetric", "TriggerFailed"} {
			if lvl, _ := k8sEventLevelWithReason(r, 999999); lvl == "critical" {
				t.Errorf("%s 无论重复多少次都不该只因次数就判 critical —— 次数是「持续了多久」不是「多严重」", r)
			}
		}
	})

	t.Run("没登记的 Reason 保持 warning，不编级别", func(t *testing.T) {
		lvl, why := k8sEventLevelWithReason("SomeBrandNewReason", 5)
		if lvl != "warning" {
			t.Errorf("不认识的 Reason 应保持 warning，实际 %s", lvl)
		}
		if why != "" {
			t.Errorf("不认识就别编理由：%s", why)
		}
	})
}

// 两张表不能有重叠：同一个 Reason 落进两档，判定就取决于代码里谁先查，
// 而那是个看不出来的顺序依赖。
func TestEventReasonTablesDoNotOverlap(t *testing.T) {
	for r := range eventDownReasons {
		if _, dup := eventBrokenReasons[r]; dup {
			t.Errorf("%s 同时在 down 和 broken 两张表里", r)
		}
	}
}

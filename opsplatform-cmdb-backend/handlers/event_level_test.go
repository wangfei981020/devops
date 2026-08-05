package handlers

import "testing"

// 原来所有 K8s Warning 一律写死 "warning"，于是
// FailedToRetrieveImagePullSecret 发生 33.9 万次和某个 Pod 偶发一次同级，
// 全淹在同一片黄色里——而前者意味着那批 Pod 根本起不来。
func TestK8sEventLevel(t *testing.T) {
	// 确定性故障：不会自愈，一次也是 critical
	for _, r := range []string{
		"FailedToRetrieveImagePullSecret", "FailedMount", "FailedCreatePodSandBox",
		"NodeNotReady", "OOMKilling", "FailedScheduling",
	} {
		if got := k8sEventLevel(r, 1); got != "critical" {
			t.Errorf("%s 出现 1 次也该是 critical（配置/环境坏了，重试不会好），实际 %s", r, got)
		}
	}

	// 暂态类：偶发是噪音
	for _, r := range []string{"BackOff", "Unhealthy", "FailedGetResourceMetric"} {
		if got := k8sEventLevel(r, 5); got != "warning" {
			t.Errorf("%s 偶发 5 次是噪音，不该是 %s——把噪音升级成 critical，真故障就淹了", r, got)
		}
	}

	// 但重复到量级就不是噪音了：一直没恢复的问题不该和偶发同级
	if got := k8sEventLevel("BackOff", 1000); got != "critical" {
		t.Errorf("BackOff 重复 1000 次说明一直在崩，应升级为 critical，实际 %s", got)
	}
	if got := k8sEventLevel("BackOff", 999); got != "warning" {
		t.Errorf("阈值边界：999 次仍是 warning，实际 %s", got)
	}
}

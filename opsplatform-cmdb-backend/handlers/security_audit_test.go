package handlers

import (
	"strings"
	"testing"
)

func risksJoined(f *secFinding) string {
	if f == nil {
		return ""
	}
	return strings.Join(f.Risks, " | ")
}

// 业务命名空间里的特权容器是最高危的一类——容器边界形同虚设。
func TestJudgeSecurityPrivilegedInBusinessNSIsCritical(t *testing.T) {
	f := judgeSecurity(secRow{ns: "g50-uat", pod: "app-1", privileged: "app"})
	if f == nil || f.Severity != "critical" {
		t.Fatalf("业务侧特权容器应为 critical，实际 %+v", f)
	}
	if f.Platform {
		t.Error("g50-uat 不该被判为平台组件")
	}
}

// 同样是特权容器，平台组件（CNI/CSI 等）降为 info——全报出来会淹掉真问题。
// 但仍然要列出并说明原因，「知道谁有特权」本身有价值。
func TestJudgeSecurityPlatformPrivilegedDowngraded(t *testing.T) {
	f := judgeSecurity(secRow{ns: "kube-system", pod: "calico-node-x", privileged: "calico"})
	if f == nil || f.Severity != "info" {
		t.Fatalf("平台组件特权应降为 info，实际 %+v", f)
	}
	if !strings.Contains(f.Note, "所必需") {
		t.Errorf("平台组件应附说明避免被误读为配置错误，实际 note=%q", f.Note)
	}
}

// 业务 Pod 挂危险路径 = critical：它没有任何理由需要集群证书或容器运行时 socket。
func TestJudgeSecurityDangerousHostPathInBusinessNSIsCritical(t *testing.T) {
	for _, p := range []string{
		"/var/run/docker.sock",
		"/var/lib/kubelet/pods", // 前缀匹配：子路径同样危险
		"/etc/kubernetes/admin.conf",
	} {
		f := judgeSecurity(secRow{ns: "g50-uat", pod: "x", paths: p})
		if f == nil || f.Severity != "critical" {
			t.Errorf("业务侧危险路径 %s 应为 critical，实际 %+v", p, f)
		}
		if !strings.Contains(risksJoined(f), "：") {
			t.Errorf("危险路径应附上危害说明，实际: %s", risksJoined(f))
		}
	}
}

// 控制面组件挂 /etc/kubernetes 是本职工作。判 critical 只会训练人忽略告警——
// 每个集群都有这几条且永远不会处理。降为 high：仍然可见，但不占用 critical。
// 这条是真实数据暴露的：本地集群跑出来的 4 条 critical 全是 etcd/apiserver。
func TestJudgeSecurityControlPlaneHostPathIsHighNotCritical(t *testing.T) {
	f := judgeSecurity(secRow{ns: "kube-system", pod: "etcd-node1", paths: "/etc/kubernetes/pki"})
	if f == nil || f.Severity != "high" {
		t.Fatalf("控制面挂集群证书目录应为 high 而非 critical，实际 %+v", f)
	}
	if !strings.Contains(risksJoined(f), "集群管理员权限") {
		t.Errorf("降级不等于不说明危害，实际: %s", risksJoined(f))
	}
}

// 普通 hostPath（如挂日志目录）不该拔到 critical，否则又是一片噪音。
func TestJudgeSecurityOrdinaryHostPathIsMedium(t *testing.T) {
	f := judgeSecurity(secRow{ns: "g50-uat", pod: "x", paths: "/data/logs"})
	if f == nil || f.Severity != "medium" {
		t.Fatalf("普通 hostPath 应为 medium，实际 %+v", f)
	}
}

// 敏感 capability 要给出具体危害，而不是只报个名字。
func TestJudgeSecuritySensitiveCapExplained(t *testing.T) {
	f := judgeSecurity(secRow{ns: "g50-uat", pod: "x", caps: "SYS_ADMIN"})
	if f == nil || f.Severity != "high" {
		t.Fatalf("业务侧 SYS_ADMIN 应为 high，实际 %+v", f)
	}
	if !strings.Contains(risksJoined(f), "privileged") {
		t.Errorf("SYS_ADMIN 应说明它近乎等同 privileged，实际: %s", risksJoined(f))
	}
	// 不认识的 capability 也要报，只是降一级——不能因为不在表里就当它安全
	if f2 := judgeSecurity(secRow{ns: "g50-uat", pod: "x", caps: "SOME_NEW_CAP"}); f2 == nil || f2.Severity != "medium" {
		t.Errorf("未知 capability 也应报出（medium），实际 %+v", f2)
	}
}

// 单独的 runAsRoot 极其普遍，只能是 medium；它的价值在于与其它风险叠加时抬高整体判定。
func TestJudgeSecurityRunAsRootAloneIsMedium(t *testing.T) {
	f := judgeSecurity(secRow{ns: "g50-uat", pod: "x", runAsRoot: true})
	if f == nil || f.Severity != "medium" {
		t.Fatalf("单独 runAsRoot 应为 medium，实际 %+v", f)
	}
	// 叠加特权后必须升到 critical，且两条风险都要列出
	f2 := judgeSecurity(secRow{ns: "g50-uat", pod: "x", runAsRoot: true, privileged: "c"})
	if f2.Severity != "critical" || len(f2.Risks) != 2 {
		t.Errorf("风险叠加时应取最高级并保留全部条目，实际 sev=%s risks=%v", f2.Severity, f2.Risks)
	}
}

// 什么风险属性都没有的行不该产出条目（采集侧已过滤，这里是第二道保险）。
func TestJudgeSecurityCleanPodReturnsNil(t *testing.T) {
	if f := judgeSecurity(secRow{ns: "g50-uat", pod: "clean"}); f != nil {
		t.Errorf("无任何风险属性时不应产出条目，实际 %+v", f)
	}
}

func TestPlatformNamespaceCoversCommonPrefixes(t *testing.T) {
	for _, ns := range []string{"kube-system", "istio-system", "gke-managed-system", "kubesphere-monitoring-system"} {
		if !platformNamespace(ns) {
			t.Errorf("%s 应识别为平台命名空间", ns)
		}
	}
	for _, ns := range []string{"g50-uat", "default", "g66-test"} {
		if platformNamespace(ns) {
			t.Errorf("%s 是业务命名空间，不该按平台组件降级", ns)
		}
	}
}

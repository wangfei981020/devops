package k8ssource

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func i64p(i int64) *int64 { return &i }

func TestExtractPodSecurityReadsAllFields(t *testing.T) {
	tru := true
	p := &corev1.Pod{Spec: corev1.PodSpec{
		HostNetwork: true, HostPID: true,
		Containers: []corev1.Container{{
			Name: "app",
			SecurityContext: &corev1.SecurityContext{
				Privileged:               &tru,
				AllowPrivilegeEscalation: &tru,
				Capabilities:             &corev1.Capabilities{Add: []corev1.Capability{"SYS_ADMIN", "NET_ADMIN"}},
			},
		}},
		Volumes: []corev1.Volume{{VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: "/var/run/docker.sock"}}}},
	}}
	s := extractPodSecurity(p)
	if !s.HostNetwork || !s.HostPID || s.HostIPC {
		t.Errorf("host* 字段读取错误: %+v", s)
	}
	if len(s.Privileged) != 1 || s.Privileged[0] != "app" {
		t.Errorf("特权容器名应为 [app]，实际 %v", s.Privileged)
	}
	if !s.PrivEsc {
		t.Error("allowPrivilegeEscalation 未读到")
	}
	// capabilities 排序后输出，否则每轮采集顺序不同、diff 全是噪音
	if len(s.AddedCaps) != 2 || s.AddedCaps[0] != "NET_ADMIN" {
		t.Errorf("capabilities 应去重排序，实际 %v", s.AddedCaps)
	}
	if len(s.HostPaths) != 1 || s.HostPaths[0] != "/var/run/docker.sock" {
		t.Errorf("hostPath 未读到，实际 %v", s.HostPaths)
	}
}

// 初始化容器的特权同样要采：它先跑，而且常被忽略。
func TestExtractPodSecurityCoversInitContainers(t *testing.T) {
	tru := true
	p := &corev1.Pod{Spec: corev1.PodSpec{
		InitContainers: []corev1.Container{{Name: "init-sysctl",
			SecurityContext: &corev1.SecurityContext{Privileged: &tru}}},
		Containers: []corev1.Container{{Name: "app"}},
	}}
	s := extractPodSecurity(p)
	if len(s.Privileged) != 1 || s.Privileged[0] != "init-sysctl" {
		t.Errorf("初始化容器的特权应被采到，实际 %v", s.Privileged)
	}
}

// 容器级 securityContext 覆盖 Pod 级——只看 Pod 级会漏判。
func TestExtractPodSecurityContainerOverridesPod(t *testing.T) {
	tru := true
	p := &corev1.Pod{Spec: corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &tru},
		Containers: []corev1.Container{
			{Name: "ok"},
			{Name: "bad", SecurityContext: &corev1.SecurityContext{RunAsUser: i64p(0)}},
		},
	}}
	if s := extractPodSecurity(p); !s.RunAsRoot {
		t.Error("Pod 级声明了 runAsNonRoot，但某容器显式 runAsUser=0，应判为以 root 运行")
	}
}

// 什么都没声明时按 K8s 实际行为算作可能以 root 运行——
// 「没声明」不等于「安全」，镜像默认用户通常就是 root。
func TestExtractPodSecurityUnsetDefaultsToRoot(t *testing.T) {
	p := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}}
	if s := extractPodSecurity(p); !s.RunAsRoot {
		t.Error("未声明 runAsNonRoot 时应保守判为以 root 运行")
	}
}

// 明确声明 runAsNonRoot 的 Pod 不该被报出来，否则等于惩罚做对了的人。
func TestExtractPodSecurityRespectsNonRoot(t *testing.T) {
	tru := true
	p := &corev1.Pod{Spec: corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &tru},
		Containers:      []corev1.Container{{Name: "app"}},
	}}
	if s := extractPodSecurity(p); s.RunAsRoot {
		t.Error("显式声明 runAsNonRoot=true 的 Pod 不该被判为 root")
	}
}

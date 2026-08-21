package k8ssource

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func boolp(b bool) *bool { return &b }

// 一个把所有引用形态都用上的 Pod：漏掉任何一类，对应的根因就永远查不出来。
func kitchenSinkPod() *corev1.Pod {
	return &corev1.Pod{
		Spec: corev1.PodSpec{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "harbor-id"}},
			InitContainers: []corev1.Container{{
				Name: "init",
				Env: []corev1.EnvVar{{
					Name: "DB", ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "db-cred"}, Key: "password",
						}},
				}},
			}},
			Containers: []corev1.Container{{
				Name: "app",
				Env: []corev1.EnvVar{{
					Name: "LEVEL", ValueFrom: &corev1.EnvVarSource{
						ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "app-conf"}, Key: "log_level",
						}},
				}},
				EnvFrom: []corev1.EnvFromSource{
					{ConfigMapRef: &corev1.ConfigMapEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "common-env"}}},
					{SecretRef: &corev1.SecretEnvSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "opt-sec"}, Optional: boolp(true)}},
				},
			}},
			Volumes: []corev1.Volume{
				{Name: "c", VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "nginx-conf"}}}},
				{Name: "s", VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: "tls-cert"}}},
				{Name: "p", VolumeSource: corev1.VolumeSource{
					Projected: &corev1.ProjectedVolumeSource{Sources: []corev1.VolumeProjection{
						{Secret: &corev1.SecretProjection{
							LocalObjectReference: corev1.LocalObjectReference{Name: "sa-token"}}},
					}}}},
			},
		},
	}
}

func has(refs []configRef, kind, name, source string) bool {
	for _, r := range refs {
		if r.Kind == kind && r.Name == name && r.Source == source {
			return true
		}
	}
	return false
}

func TestPodConfigRefsCoversEveryReferenceForm(t *testing.T) {
	refs := podConfigRefs(kitchenSinkPod())
	want := []struct{ kind, name, source string }{
		{kindSec, "harbor-id", "imagePullSecret"}, // DEV-002 这一类
		{kindSec, "db-cred", "env"},               // 初始化容器——它先跑，往往才是真卡点
		{kindCM, "app-conf", "env"},
		{kindCM, "common-env", "envFrom"},
		{kindCM, "nginx-conf", "volume"},
		{kindSec, "tls-cert", "volume"},
		{kindSec, "sa-token", "volume"}, // 投射卷不展开就整片漏掉
	}
	for _, w := range want {
		if !has(refs, w.kind, w.name, w.source) {
			t.Errorf("漏掉引用: %s %s (来源 %s)", w.kind, w.name, w.source)
		}
	}
}

// optional 必须如实带出：把可选引用当必需报，就是又一批误报。
func TestPodConfigRefsCarriesOptional(t *testing.T) {
	for _, r := range podConfigRefs(kitchenSinkPod()) {
		if r.Name == "opt-sec" && !r.Optional {
			t.Error("opt-sec 声明了 optional=true，必须被记为可选")
		}
		if r.Name == "harbor-id" && r.Optional {
			t.Error("imagePullSecret 无 optional 语义，不该被判为可选")
		}
	}
}

// 键名要一并记下，否则「引用的键不存在」这类问题无从判定。
func TestPodConfigRefsKeepsKey(t *testing.T) {
	var got string
	for _, r := range podConfigRefs(kitchenSinkPod()) {
		if r.Name == "app-conf" {
			got = r.Key
		}
	}
	if got != "log_level" {
		t.Errorf("应记录引用的键名 log_level，实际 %q", got)
	}
}

// 多容器引同一个 envFrom 时要去重，否则影响面统计虚高。
func TestPodConfigRefsDedupes(t *testing.T) {
	same := corev1.Container{EnvFrom: []corev1.EnvFromSource{
		{ConfigMapRef: &corev1.ConfigMapEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: "shared"}}}}}
	p := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{same, same}}}
	if got := podConfigRefs(p); len(got) != 1 {
		t.Errorf("同一容器名下的重复引用应去重为 1 条，实际 %d 条: %v", len(got), got)
	}
}

// 空名引用（字段存在但 name 为空）不该落库，否则会生成查不到对应物的假条目。
func TestPodConfigRefsSkipsEmptyNames(t *testing.T) {
	p := &corev1.Pod{Spec: corev1.PodSpec{
		ImagePullSecrets: []corev1.LocalObjectReference{{Name: ""}},
		Volumes: []corev1.Volume{{VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{SecretName: ""}}}},
	}}
	if got := podConfigRefs(p); len(got) != 0 {
		t.Errorf("空名引用应被跳过，实际抽到 %v", got)
	}
}

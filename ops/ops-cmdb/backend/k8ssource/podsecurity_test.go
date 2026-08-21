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

// 什么都没声明 → **未知**，不是 root（OPSCMDB-032）。
//
// ⚠️ 这条测试原来断言的是「应保守判为以 root 运行」，那正是被修掉的错误判据：
// 实际 uid 由镜像 USER 决定，CMDB 采不到镜像内容，所以这里判不了。
// 把判不了说成 root，UAT 上造成 55% 的误报，让非 root 整改无法验收。
func TestExtractPodSecurityUnsetIsUnknownNotRoot(t *testing.T) {
	p := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}}
	s := extractPodSecurity(p)
	if s.RunAsRoot {
		t.Error("两级都没声明时不能判成 root——实际 uid 取决于镜像 USER")
	}
	if !s.RootUnknown {
		t.Error("两级都没声明时应标为未知，让人去查镜像，而不是当作已知")
	}
}

// 模式 3（OPSCMDB-032 里最误导的一种）：设了 runAsUser 但没写 runAsNonRoot。
// 对象已经是非 root 了，只是少了个布尔标志，原实现照样报 root。
// UAT 的 rocketmq-broker-master / nameserver / dashboard 都是这个形态。
func TestExtractPodSecurityRunAsUserWithoutNonRootFlag(t *testing.T) {
	p := &corev1.Pod{Spec: corev1.PodSpec{
		SecurityContext: &corev1.PodSecurityContext{RunAsUser: i64p(3000)},
		Containers:      []corev1.Container{{Name: "broker"}},
	}}
	s := extractPodSecurity(p)
	if s.RunAsRoot {
		t.Error("pod 级 runAsUser=3000 已经是非 root，不该因为没写 runAsNonRoot 就报 root")
	}
	if s.RootUnknown {
		t.Error("runAsUser 有值就是确定的，不该标未知")
	}
}

// 模式 1/2：root initContainer 不该算到主容器头上，但也不能不报。
// UAT 86 个前端都是这个形态：主容器 uid 101，init 修 nginx 缓存目录权限用 uid 0。
func TestExtractPodSecurityRootInitSeparateFromMain(t *testing.T) {
	tru := true
	p := &corev1.Pod{Spec: corev1.PodSpec{
		InitContainers: []corev1.Container{
			{Name: "fix-perm", SecurityContext: &corev1.SecurityContext{RunAsUser: i64p(0)}},
		},
		Containers: []corev1.Container{
			{Name: "app", SecurityContext: &corev1.SecurityContext{
				RunAsUser: i64p(101), RunAsNonRoot: &tru,
			}},
		},
	}}
	s := extractPodSecurity(p)
	if s.RunAsRoot {
		t.Error("主容器是 uid 101，不该报「主容器以 root 运行」")
	}
	if !s.RootInit {
		t.Error("init 容器是 uid 0，必须报出来——restricted PSA 过不了")
	}
}

// runAsNonRoot=false 不等于「以 root 跑」：它只是不强制校验，
// 镜像里可能本来就是非 root 用户。落到未知，不能判成 root。
func TestExtractPodSecurityNonRootFalseIsUnknown(t *testing.T) {
	fal := false
	p := &corev1.Pod{Spec: corev1.PodSpec{
		Containers: []corev1.Container{
			{Name: "app", SecurityContext: &corev1.SecurityContext{RunAsNonRoot: &fal}},
		},
	}}
	s := extractPodSecurity(p)
	if s.RunAsRoot {
		t.Error("runAsNonRoot=false 只是不强制校验，不是「确定以 root 跑」")
	}
	if !s.RootUnknown {
		t.Error("这种情况应落到未知，让人去查镜像 USER")
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

// TestRealWorldObjectsFromOPSCMDB059 拿 OPSCMDB-059 里**逐个实测过运行期 uid**
// 的真实对象反测判据。
//
// 🔴 这五个对象是那份档案的全部实证样本，且答案是已知的（get_manifest 实测）：
// 判据只要在其中任何一个上出错，那份清单就会真假混杂 ——
// 而真假混杂是最难处理的形态，运维只能逐条回查，功能等于没有净收益。
func TestRealWorldObjectsFromOPSCMDB059(t *testing.T) {
	i64 := func(v int64) *int64 { return &v }
	b := func(v bool) *bool { return &v }
	// 容器级只有这些"规范但与 uid 无关"的字段 —— 正是漏判的诱因：
	// 看着有 securityContext，里面却没有 runAsUser
	noUID := &corev1.SecurityContext{
		AllowPrivilegeEscalation: b(false),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}

	cases := []struct {
		name        string
		pod         *corev1.Pod
		wantRoot    bool
		wantUnknown bool
	}{
		{
			// pod 级写了 uid，容器级不写 —— helm chart 的常规写法
			name: "fleet-controller 实测 uid 1000",
			pod: &corev1.Pod{Spec: corev1.PodSpec{
				SecurityContext: &corev1.PodSecurityContext{
					RunAsUser: i64(1000), RunAsGroup: i64(1000), RunAsNonRoot: b(true)},
				Containers: []corev1.Container{
					{Name: "fleetcontroller", SecurityContext: noUID},
					{Name: "gitjob", SecurityContext: noUID},
					{Name: "agentmanagement", SecurityContext: noUID},
				},
			}},
		},
		{
			// 只有 runAsNonRoot:true，uid 来自镜像 USER。
			// kubelet 启动前会校验镜像 USER，是 0 就拒绝启动 —— 所以这个标志比 uid 更硬
			name: "argo-rollouts 只声明 runAsNonRoot，实测 uid 999",
			pod: &corev1.Pod{Spec: corev1.PodSpec{
				SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: b(true)},
				Containers:      []corev1.Container{{Name: "argo-rollouts", SecurityContext: noUID}},
			}},
		},
		{
			// 🔴 判据盲区的典型：sidecar **连容器级 securityContext 都没有**，
			// 完全靠 pod 级继承。同一个 Pod 里两个容器都曾被报成 root，实测两个都是 10001
			name: "loki-backend-0：loki 有容器级但无 uid，loki-sc-rules 完全没有",
			pod: &corev1.Pod{Spec: corev1.PodSpec{
				SecurityContext: &corev1.PodSecurityContext{
					RunAsUser: i64(10001), RunAsGroup: i64(10001), RunAsNonRoot: b(true)},
				Containers: []corev1.Container{
					{Name: "loki", SecurityContext: noUID},
					{Name: "loki-sc-rules"}, // 没有 securityContext
				},
			}},
		},
		{
			// 整改验收：14:35 补齐 pod 级后重建，判定必须跟着变 ——
			// 否则"改完了"都验收不了（清单一条不少）
			name: "kafka-ui 整改后：pod 级补了 runAsUser 100",
			pod: &corev1.Pod{Spec: corev1.PodSpec{
				SecurityContext: &corev1.PodSecurityContext{
					RunAsUser: i64(100), RunAsGroup: i64(101), RunAsNonRoot: b(true)},
				Containers: []corev1.Container{{Name: "kafka-ui", SecurityContext: noUID}},
			}},
		},
		{
			// 反向：真的 root 必须仍然报出来，不能为了消误报把真问题一起消掉
			name:     "g32-auto-labeler 实测 uid 0",
			wantRoot: true,
			pod: &corev1.Pod{Spec: corev1.PodSpec{
				SecurityContext: &corev1.PodSecurityContext{RunAsUser: i64(0)},
				Containers:      []corev1.Container{{Name: "labeler"}},
			}},
		},
		{
			// 两级都没声明：归「运行用户未知」，**不能**判成 root
			name:        "filebeat-kibana-pattern-sync：两级都没声明",
			wantUnknown: true,
			pod: &corev1.Pod{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "sync"}},
			}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := extractPodSecurity(tc.pod)
			if got.RunAsRoot != tc.wantRoot {
				t.Errorf("RunAsRoot = %v，want %v", got.RunAsRoot, tc.wantRoot)
			}
			if got.RootUnknown != tc.wantUnknown {
				t.Errorf("RootUnknown = %v，want %v", got.RootUnknown, tc.wantUnknown)
			}
		})
	}
}

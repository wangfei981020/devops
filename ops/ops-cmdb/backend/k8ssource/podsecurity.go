package k8ssource

import (
	"database/sql"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// Pod 安全上下文采集。全部来自 pod spec，复用采 Pod 那次 List，不需要任何新权限。
//
// 补的是「安全审计全盲」：此前 CMDB 答不出「哪些 Pod 是特权容器」「谁挂了
// docker.sock」「谁开了 hostNetwork」——这些正是容器逃逸和横向移动的入口。

// podSecurity 一个 Pod 的安全相关属性。
type podSecurity struct {
	HostNetwork, HostPID, HostIPC bool
	Privileged                    []string // 特权容器名
	// RunAsRoot 主容器里**确定**以 uid 0 跑的。
	//
	// ⚠️ 这三个字段是 OPSCMDB-032 拆出来的，原来只有一个 RunAsRoot，
	// 判据是 `userZero || !nonRoot` —— 只要没显式写 runAsNonRoot: true 就算 root，
	// 而且 init 容器和主容器压在同一个 bool 里。UAT 实测 168 条里 92 条是误报（55%），
	// 直接导致非 root 整改**无法验收**：改完一批数字不降，改过的和没改的长得一样。
	RunAsRoot bool
	// RootInit init 容器里有 root 的。
	//
	// ⚠️ 必须和 RunAsRoot 分开：这两件事的整改动作完全不同。
	// 前端那批 `fix-nginx-cache-perm` 是 root init + 非 root 主容器 ——
	// 合规上确实过不了 restricted PSA（要报），但它和「主进程以 root 跑」
	// 的风险完全不是一回事，混成一条读者无法区分该先改哪个。
	RootInit bool
	// RootUnknown 两级都没声明 runAsUser / runAsNonRoot，**判不了**。
	//
	// ⚠️ 未声明 ≠ 以 root 跑：实际 uid 由镜像的 USER 决定，而 CMDB 采不到镜像。
	// 原来把这一类直接判成 root，正是误报的最大来源。
	// 判不了就说判不了，让人去查镜像 —— 而不是猜一个方向然后当成事实。
	RootUnknown bool
	PrivEsc     bool
	AddedCaps   []string
	HostPaths   []string
}

// effUID 一个容器的有效 uid 判定结果。
type uidVerdict int

const (
	uidUnknown uidVerdict = iota // 两级都没声明，要查镜像 USER
	uidRoot                      // 确定 uid 0
	uidNonRoot                   // 确定非 0
)

// containerUID 算一个容器的有效 uid 归属。容器级覆盖 Pod 级。
//
// 判定顺序（先命中先返回）：
//  1. 有效 runAsUser 有值 → 按它是不是 0 定；这是**最硬的证据**
//  2. runAsNonRoot=true → 非 root。kubelet 会在启动前校验镜像 USER，
//     是 root 就拒绝启动，所以这个标志为真时可以信
//  3. runAsNonRoot=false → 仍然**不能**判成 root：它只是"不强制校验"，
//     镜像里可能就是个非 root 用户。落到未知
//  4. 都没声明 → 未知
func containerUID(sc *corev1.SecurityContext, podUser *int64, podNonRoot *bool) uidVerdict {
	user, nonRoot := podUser, podNonRoot
	if sc != nil {
		if sc.RunAsUser != nil {
			user = sc.RunAsUser
		}
		if sc.RunAsNonRoot != nil {
			nonRoot = sc.RunAsNonRoot
		}
	}
	if user != nil {
		if *user == 0 {
			return uidRoot
		}
		return uidNonRoot
	}
	if nonRoot != nil && *nonRoot {
		return uidNonRoot
	}
	return uidUnknown
}

// extractPodSecurity 从 pod spec 提取安全属性。
//
// root 判定按**有效 uid**算（见 containerUID），不再看 runAsNonRoot 标志位，
// 且 init 容器与主容器分开统计。理由见 podSecurity 上面那三个字段的注释。
func extractPodSecurity(p *corev1.Pod) podSecurity {
	s := podSecurity{
		HostNetwork: p.Spec.HostNetwork,
		HostPID:     p.Spec.HostPID,
		HostIPC:     p.Spec.HostIPC,
	}

	// Pod 级默认值。⚠️ 存指针而不是拍平成 bool ——
	// 「没声明」和「声明为 false」是两种不同的事实，拍平就再也分不开了。
	var podUser *int64
	var podNonRoot *bool
	if psc := p.Spec.SecurityContext; psc != nil {
		podUser, podNonRoot = psc.RunAsUser, psc.RunAsNonRoot
	}

	capSet := map[string]bool{}
	// init 与主容器分开走：特权/提权/capabilities 两边都要采（风险是一样的），
	// 但 root 归属要分开记。
	scan := func(cts []corev1.Container, isInit bool) {
		for _, ct := range cts {
			sc := ct.SecurityContext
			if sc != nil {
				if sc.Privileged != nil && *sc.Privileged {
					s.Privileged = append(s.Privileged, ct.Name)
				}
				if sc.AllowPrivilegeEscalation != nil && *sc.AllowPrivilegeEscalation {
					s.PrivEsc = true
				}
				if sc.Capabilities != nil {
					for _, c := range sc.Capabilities.Add {
						if c != "" {
							capSet[string(c)] = true
						}
					}
				}
			}
			switch containerUID(sc, podUser, podNonRoot) {
			case uidRoot:
				if isInit {
					s.RootInit = true
				} else {
					s.RunAsRoot = true
				}
			case uidUnknown:
				// ⚠️ 未知只记在主容器上。init 容器绝大多数是不声明的一次性小工具，
				// 把它们也标成「待查」会让待查清单淹没在噪音里。
				if !isInit {
					s.RootUnknown = true
				}
			}
		}
	}
	scan(p.Spec.InitContainers, true)
	scan(p.Spec.Containers, false)
	for c := range capSet {
		s.AddedCaps = append(s.AddedCaps, c)
	}
	sort.Strings(s.AddedCaps)

	pathSet := map[string]bool{}
	for _, v := range p.Spec.Volumes {
		if v.HostPath != nil && v.HostPath.Path != "" {
			pathSet[v.HostPath.Path] = true
		}
	}
	for pth := range pathSet {
		s.HostPaths = append(s.HostPaths, pth)
	}
	sort.Strings(s.HostPaths)
	return s
}

// syncPodSecurity 落库。只记录「有任一风险属性」的 Pod——绝大多数 Pod 什么都没开，
// 全量落库只会让表里 95% 是无意义的零值行。
func syncPodSecurity(db *sql.DB, cid int, pods []corev1.Pod) error {
	rows := make([][]any, 0, 32)
	for i := range pods {
		p := &pods[i]
		s := extractPodSecurity(p)
		if !s.HostNetwork && !s.HostPID && !s.HostIPC && len(s.Privileged) == 0 &&
			!s.RunAsRoot && !s.RootInit && !s.RootUnknown &&
			!s.PrivEsc && len(s.AddedCaps) == 0 && len(s.HostPaths) == 0 {
			continue
		}
		rows = append(rows, []any{
			cid, p.Namespace, p.Name, ownerWorkload(p),
			b2i(s.HostNetwork), b2i(s.HostPID), b2i(s.HostIPC),
			strings.Join(s.Privileged, ","), b2i(s.RunAsRoot), b2i(s.PrivEsc),
			strings.Join(s.AddedCaps, ","), strings.Join(s.HostPaths, ","),
			b2i(s.RootInit), b2i(s.RootUnknown),
		})
	}
	_, err := writeRows(db, "k8s_pod_security", []string{
		"cluster_id", "namespace", "pod_name", "workload",
		"host_network", "host_pid", "host_ipc", "privileged",
		"run_as_root", "priv_esc", "added_caps", "host_paths",
		"root_init", "root_unknown",
	}, cid, rows, "cluster_id", "namespace", "pod_name")
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

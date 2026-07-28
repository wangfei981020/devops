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
	RunAsRoot                     bool
	PrivEsc                       bool
	AddedCaps                     []string
	HostPaths                     []string
}

// extractPodSecurity 从 pod spec 提取安全属性。
//
// runAsRoot 的判定要看 Pod 级和容器级两层，容器级覆盖 Pod 级；
// 两层都没声明时按 K8s 的实际行为算作「可能以 root 运行」——镜像默认用户通常就是 root。
// 这里宁可报出来让人确认，也不要因为「没声明」就当成安全。
func extractPodSecurity(p *corev1.Pod) podSecurity {
	s := podSecurity{
		HostNetwork: p.Spec.HostNetwork,
		HostPID:     p.Spec.HostPID,
		HostIPC:     p.Spec.HostIPC,
	}

	// Pod 级默认值
	podNonRoot, podUserZero := false, false
	if psc := p.Spec.SecurityContext; psc != nil {
		podNonRoot = psc.RunAsNonRoot != nil && *psc.RunAsNonRoot
		podUserZero = psc.RunAsUser != nil && *psc.RunAsUser == 0
	}

	containers := make([]corev1.Container, 0, len(p.Spec.InitContainers)+len(p.Spec.Containers))
	containers = append(containers, p.Spec.InitContainers...)
	containers = append(containers, p.Spec.Containers...)
	capSet := map[string]bool{}
	for _, ct := range containers {
		sc := ct.SecurityContext
		nonRoot, userZero := podNonRoot, podUserZero
		if sc != nil {
			if sc.Privileged != nil && *sc.Privileged {
				s.Privileged = append(s.Privileged, ct.Name)
			}
			if sc.AllowPrivilegeEscalation != nil && *sc.AllowPrivilegeEscalation {
				s.PrivEsc = true
			}
			if sc.RunAsNonRoot != nil {
				nonRoot = *sc.RunAsNonRoot
			}
			if sc.RunAsUser != nil {
				userZero = *sc.RunAsUser == 0
			}
			if sc.Capabilities != nil {
				for _, c := range sc.Capabilities.Add {
					if c != "" {
						capSet[string(c)] = true
					}
				}
			}
		}
		if userZero || !nonRoot {
			s.RunAsRoot = true
		}
	}
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
			!s.RunAsRoot && !s.PrivEsc && len(s.AddedCaps) == 0 && len(s.HostPaths) == 0 {
			continue
		}
		rows = append(rows, []any{
			cid, p.Namespace, p.Name, ownerWorkload(p),
			b2i(s.HostNetwork), b2i(s.HostPID), b2i(s.HostIPC),
			strings.Join(s.Privileged, ","), b2i(s.RunAsRoot), b2i(s.PrivEsc),
			strings.Join(s.AddedCaps, ","), strings.Join(s.HostPaths, ","),
		})
	}
	_, err := replaceAll(db, "k8s_pod_security", []string{
		"cluster_id", "namespace", "pod_name", "workload",
		"host_network", "host_pid", "host_ipc", "privileged",
		"run_as_root", "priv_esc", "added_caps", "host_paths",
	}, cid, rows)
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

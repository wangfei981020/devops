package handlers

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// 容器安全上下文审计。
//
// 补 CMDB-003 里「容器安全上下文完全查不到，安全审计盲区」：出了容器逃逸类问题，
// 此前连「哪些 Pod 有这个能力」都答不上来。
//
// 设计上最花心思的不是识别风险属性（那只是读 spec），而是**把平台组件和业务负载分开**：
// CNI、CSI、node-exporter、kube-proxy 天生需要 privileged 和 hostPath，
// 全报出来就是几十条永远不会处理的条目，真正该看的业务侧特权容器反而被埋掉。
// 这是 CMDB-005 已经付过一次学费的教训。

// dangerousHostPath 挂上就等于拿到宿主机/集群控制权的路径。
// 这几条与「平台组件通常需要」无关——即使是系统组件挂了也该被看见，只是不算异常。
var dangerousHostPath = []struct{ prefix, why string }{
	{"/var/run/docker.sock", "挂载 Docker socket 等同于宿主机 root：可直接起特权容器接管节点"},
	{"/run/containerd/containerd.sock", "挂载 containerd socket 等同于宿主机 root"},
	{"/var/run/crio/crio.sock", "挂载 CRI-O socket 等同于宿主机 root"},
	{"/var/lib/kubelet", "kubelet 目录含所有已挂载 Secret 的明文，等同于读取全节点凭证"},
	{"/etc/kubernetes", "含 kubeconfig 与证书，可直接取得集群管理员权限"},
	{"/root", "宿主机 root 家目录，通常含 SSH 私钥"},
	{"/etc/shadow", "宿主机密码哈希"},
}

// sensitiveCaps 授予后可突破容器边界的 capability。
var sensitiveCaps = map[string]string{
	"SYS_ADMIN":    "近乎等同于 privileged，可挂载文件系统、操作 cgroup",
	"SYS_PTRACE":   "可注入/调试其它进程",
	"SYS_MODULE":   "可加载内核模块，直接控制宿主机内核",
	"NET_ADMIN":    "可改宿主机网络配置、抓包",
	"DAC_OVERRIDE": "绕过文件权限检查",
	"SYS_BOOT":     "可重启宿主机",
}

// platformNamespace 平台组件所在命名空间。这些地方的特权是设计使然，
// 报为异常没有意义——但仍然列出（标 info），因为「知道谁有特权」本身是有价值的。
func platformNamespace(ns string) bool {
	switch ns {
	case "kube-system", "kube-public", "kube-node-lease", "istio-system",
		"cert-manager", "monitoring", "kubesphere-system", "kubesphere-monitoring-system",
		"cattle-system", "calico-system", "tigera-operator", "longhorn-system", "rook-ceph":
		return true
	}
	return strings.HasPrefix(ns, "gke-") || strings.HasPrefix(ns, "gmp-") ||
		strings.HasPrefix(ns, "kubesphere-") || strings.HasPrefix(ns, "openebs")
}

type secFinding struct {
	Severity  string   `json:"severity"`
	Namespace string   `json:"namespace"`
	Pod       string   `json:"pod"`
	Workload  string   `json:"workload,omitempty"`
	Risks     []string `json:"risks"`
	Platform  bool     `json:"platform_component"`
	Note      string   `json:"note,omitempty"`
	// PodCount 这条问题影响多少个 Pod（同一 workload 的副本归并成一条）。
	// 必须显式给出：归并之后"16 条"变"1 条"，不说清影响面会显得问题变小了。
	PodCount int `json:"pod_count"`
	// SamplePods 归并掉的 Pod 名（最多留几个），排查时要能落到具体实例
	SamplePods []string `json:"sample_pods,omitempty"`
}

// mergeByWorkload 把同一 (namespace, workload, 风险集合) 的多个 Pod 合成一条。
//
//	为什么要带上风险集合做键：同一个 workload 的不同 Pod **理论上**可能有
//	不同的风险（比如滚动更新中新旧两版 spec 并存）。只按 ns+workload 合并
//	会把其中一版的风险吞掉——那又是一种"少报"。
//
//	workload 为空的（裸 Pod、采集没关联上）不合并：它们本来就是独立对象，
//	按 Pod 名各算一条才对。
func mergeByWorkload(in []secFinding) []secFinding {
	const maxSamples = 5
	out := make([]secFinding, 0, len(in))
	idx := map[string]int{}

	for _, f := range in {
		if f.Workload == "" {
			f.PodCount = 1
			out = append(out, f)
			continue
		}
		key := f.Namespace + "\x00" + f.Workload + "\x00" + strings.Join(f.Risks, "|")
		if i, ok := idx[key]; ok {
			out[i].PodCount++
			if len(out[i].SamplePods) < maxSamples {
				out[i].SamplePods = append(out[i].SamplePods, f.Pod)
			}
			continue
		}
		f.PodCount = 1
		f.SamplePods = []string{f.Pod}
		// 合并后这一行代表的是 workload 而不是某个 Pod，Pod 字段留空避免误导
		f.Pod = ""
		idx[key] = len(out)
		out = append(out, f)
	}
	return out
}

type secRow struct {
	ns, pod, workload         string
	hostNet, hostPID, hostIPC bool
	privileged, caps, paths   string
	runAsRoot, privEsc        bool
}

// SecurityAudit 列出有安全风险属性的 Pod，按严重度排序。
// 参数：cluster_id(必填)、namespace(可选)、include_platform(可选，1=包含平台组件)
func (h *K8sResourceHandler) SecurityAudit(c *gin.Context) {
	cid, ok := requireCluster(c, h.DB)
	if !ok {
		return
	}
	q := `SELECT namespace,pod_name,workload,host_network,host_pid,host_ipc,
	        COALESCE(privileged,''),run_as_root,priv_esc,COALESCE(added_caps,''),COALESCE(host_paths,'')
	      FROM k8s_pod_security WHERE cluster_id=?`
	args := []any{cid}
	if ns := c.Query("namespace"); ns != "" {
		q += " AND namespace=?"
		args = append(args, ns)
	}
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	includePlatform := c.Query("include_platform") == "1"
	findings := []secFinding{}
	platformHidden := 0
	for rows.Next() {
		var r secRow
		var hn, hp, hi, rr, pe int
		if rows.Scan(&r.ns, &r.pod, &r.workload, &hn, &hp, &hi,
			&r.privileged, &rr, &pe, &r.caps, &r.paths) != nil {
			continue
		}
		r.hostNet, r.hostPID, r.hostIPC = hn == 1, hp == 1, hi == 1
		r.runAsRoot, r.privEsc = rr == 1, pe == 1

		f := judgeSecurity(r)
		if f == nil {
			continue
		}
		if f.Platform && !includePlatform {
			platformHidden++
			continue
		}
		findings = append(findings, *f)
	}

	// ⚠️ 按 ns+workload 归并。
	//
	//	原来是**逐 Pod 一行**：一个 DaemonSet 在 16 个节点上就是 16 条，
	//	而它其实是**同一个问题**（同一份 spec）。生产上 high=16 全是 filebeat
	//	的 16 个副本，实际就 1 个问题；promtail 一家占了 35 行。
	//	整体虚高 4.8 倍（523 → 112），这个数字是拿来排优先级的。
	//
	//	归并但不隐藏影响面：pod_count 和样例 Pod 名都带上，
	//	否则"16 条变 1 条"会让人以为问题缩小了。
	findings = mergeByWorkload(findings)

	rank := map[string]int{"critical": 0, "high": 1, "medium": 2, "info": 3}
	sort.SliceStable(findings, func(i, j int) bool {
		return rank[findings[i].Severity] < rank[findings[j].Severity]
	})
	sum := map[string]int{}
	for _, f := range findings {
		sum[f.Severity]++
	}

	out := gin.H{"cluster_id": cid, "summary": sum, "count": len(findings), "findings": findings}
	if platformHidden > 0 {
		// 隐藏了什么必须说出来，否则这份结果看着像「全集群只有这几个特权容器」。
		out["platform_hidden"] = platformHidden
		out["note"] = "已隐藏 " + itoa(platformHidden) + " 个平台组件（CNI/CSI/监控等，特权是设计使然）；" +
			"传 include_platform=1 可一并列出"
	}
	c.JSON(http.StatusOK, out)
}

// judgeSecurity 把一行属性翻译成风险清单与严重度。
func judgeSecurity(r secRow) *secFinding {
	f := secFinding{Namespace: r.ns, Pod: r.pod, Workload: r.workload, Platform: platformNamespace(r.ns)}
	sev := ""
	bump := func(s string) {
		rank := map[string]int{"": 0, "info": 1, "medium": 2, "high": 3, "critical": 4}
		if rank[s] > rank[sev] {
			sev = s
		}
	}

	// 危险宿主机路径。
	//
	// 严重度按「谁挂的」区分：K8s 控制面（etcd/apiserver/controller-manager）挂
	// /etc/kubernetes 是它们的本职工作，每个集群都有这几条，判 critical 只会训练人忽略告警；
	// 但**业务 Pod** 挂同样的路径就是重大问题——它没有任何理由需要集群证书。
	// 平台侧仍然列出（high），因为「谁能读到集群证书」本身必须可见。
	for _, p := range splitNonEmpty(r.paths) {
		matched := false
		for _, d := range dangerousHostPath {
			if strings.HasPrefix(p, d.prefix) {
				f.Risks = append(f.Risks, "挂载宿主机路径 "+p+"："+d.why)
				bump(ternarySev(f.Platform, "high", "critical"))
				matched = true
				break
			}
		}
		if !matched {
			f.Risks = append(f.Risks, "挂载宿主机路径 "+p)
			bump(ternarySev(f.Platform, "info", "medium"))
		}
	}

	if pv := splitNonEmpty(r.privileged); len(pv) > 0 {
		f.Risks = append(f.Risks, "特权容器："+strings.Join(pv, ", ")+"——可访问所有宿主机设备，容器边界形同虚设")
		if f.Platform {
			bump("info")
		} else {
			bump("critical")
		}
	}
	if r.hostPID {
		f.Risks = append(f.Risks, "hostPID：可看到并操作宿主机全部进程")
		bump(ternarySev(f.Platform, "info", "high"))
	}
	if r.hostIPC {
		f.Risks = append(f.Risks, "hostIPC：共享宿主机进程间通信，可读其它进程共享内存")
		bump(ternarySev(f.Platform, "info", "high"))
	}
	if r.hostNet {
		f.Risks = append(f.Risks, "hostNetwork：直接使用宿主机网络栈，可监听任意端口、绕过 NetworkPolicy")
		bump(ternarySev(f.Platform, "info", "high"))
	}
	for _, cp := range splitNonEmpty(r.caps) {
		if why, ok := sensitiveCaps[strings.ToUpper(cp)]; ok {
			f.Risks = append(f.Risks, "额外 capability "+cp+"："+why)
			bump(ternarySev(f.Platform, "info", "high"))
		} else {
			f.Risks = append(f.Risks, "额外 capability "+cp)
			bump("medium")
		}
	}
	if r.privEsc {
		f.Risks = append(f.Risks, "允许提权 allowPrivilegeEscalation=true：容器内进程可获得比父进程更多的权限")
		bump("medium")
	}
	if r.runAsRoot {
		// 单独的 runAsRoot 极其普遍，报 high 会淹掉真问题；它的价值在于与其它风险叠加。
		f.Risks = append(f.Risks, "以 root 运行（未设置 runAsNonRoot）")
		bump("medium")
	}

	if len(f.Risks) == 0 {
		return nil
	}
	f.Severity = sev
	if f.Platform {
		f.Note = "平台组件，上述权限通常是其正常工作所必需——列出是为了「知道谁有特权」，不代表配置错误"
	}
	return &f
}

func ternarySev(platform bool, ifPlatform, otherwise string) string {
	if platform {
		return ifPlatform
	}
	return otherwise
}

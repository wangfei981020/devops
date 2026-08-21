package handlers

import "fmt"

// 节点磁盘水位的**唯一**口径。四处（cluster_health / obs_query / disk_watch /
// node_health）以前各写各的 `mountpoint="/"`，同一个错误复制了四份。
//
// 🔴 为什么不能用 mountpoint="/"
//
// GKE 的 COS 节点上 `/` 是**只读的启动镜像**（/dev/root，ext2，仅 1.93 GB）。
// 真正会被容器镜像 / 可写层 / emptyDir 撑满的是 /dev/sda1（980 GB），
// 它挂在 /var/lib/kubelet、/var/lib/containerd、/mnt/stateful_partition… 十几个点上。
//
// 实测 g32-prod：按 `/` 算，35 个节点**全部是 74.0656%**，
// 一模一样到小数点后 14 位、而且永远不变 —— 因为那是同一个只读镜像。
// 也就是说磁盘水位判定和磁盘告警在三个 GKE 集群（UAT / g32-prod / infra）上
// **从来没有真正生效过**：它不报错、不为空，只是一直在回答另一个问题。
// DEV 那次磁盘打满能被发现，纯粹因为 DEV 是 k3s，`/` 恰好就是可写分区。
//
// ⚠️ 改这里之前先读完下面三个坑，全都是实测栽出来的。
const (
	//  坑 1：只读**不能按设备排除**。
	//   同一块 /dev/sda1 在 /var/lib/kubelet 是 rw(readonly=0)、
	//   在 /home/containerd 是 ro(readonly=1)。
	//   写成 `unless max by(device)(readonly==1)` 会因为一个只读绑定挂载
	//   把整块盘丢掉，结果**整个查询返回空**（栽过一次）。
	//   必须逐 series（含 mountpoint）做 unless —— 就是下面这个写法。
	nodeFsWritable = ` unless node_filesystem_readonly%[1]s == 1`

	//  坑 2：**不能把所有非虚拟文件系统都算进来**。
	//   node-exporter 也会上报 CSI 挂上来的 /dev/sdb（PVC 的 globalmount）。
	//   那是 PV 的用量，不是节点磁盘。算进来会把「某个 PVC 快满了」
	//   显示成「节点磁盘快满了」，把人指向完全错误的处置。PV 归 pvc_usage 管。
	//
	//   所以取白名单：kubelet 真正会因之驱逐 Pod 的节点级挂载点
	//   （nodefs=/var/lib/kubelet，imagefs=/var/lib/containerd），加各发行版等价位置。
	//   `/` 留着是给 k3s / kubeadm 用的；在 COS 上它会被上面那步自己排掉。
	nodeFsMounts = `mountpoint=~"/|/var|/var/lib|/var/lib/kubelet|/var/lib/containerd|` +
		`/var/lib/docker|/var/lib/rancher|/mnt/stateful_partition|/run/containerd"`
)

//  坑 3：**绝不能对白名单里的 size 做 sum**。
//   同一块盘挂 12 个点就会被累加 12 次，980 GB 变成 11 TB，
//   于是水位被稀释到接近 0 —— 又是一个"不报错只是答错"。
//   所以聚合放在 Go 里做：逐挂载点算好占比，每个节点只取**最满的那一个**，
//   并且总量/已用量都取自**同一个**挂载点，三个数字必须自洽。

// NodeFs 一个节点上最吃紧的那个节点级文件系统。
type NodeFs struct {
	Mountpoint string
	Pct        float64 // 已用百分比
	TotalBytes float64
	UsedBytes  float64
}

// nodeFsLabels 拼出带集群选择器 + 挂载点白名单的 label 串。
func nodeFsLabels(sel string) string { return promLabels(sel, nodeFsMounts) }

// nodeFsWritableSuffix 排除只读挂载的 unless 子句（坑 1）。
func nodeFsWritableSuffix(lbl string) string { return fmt.Sprintf(nodeFsWritable, lbl) }

// nodeFsUsage 取每个节点**最吃紧**的可写节点级文件系统。
// 键是节点名（没有 node 标签时退回 instance —— DEV 的 node-exporter 就没有 node）。
func nodeFsUsage(base, token, sel string) (map[string]NodeFs, error) {
	lbl := nodeFsLabels(sel)
	ro := nodeFsWritableSuffix(lbl)

	size, err := promInstant(base, token, `node_filesystem_size_bytes`+lbl+ro)
	if err != nil {
		return nil, err
	}
	avail, err := promInstant(base, token, `node_filesystem_avail_bytes`+lbl+ro)
	if err != nil {
		return nil, err
	}

	// 按 (节点, 挂载点) 关联。用 device 做键会在 bind mount 下重复（同 host_disk_usage 的教训）。
	type fsKey struct{ node, mp string }
	sizes := map[fsKey]float64{}
	for _, s := range size {
		if n := promNodeName(s.Metric); n != "" && s.Value > 0 {
			sizes[fsKey{n, s.Metric["mountpoint"]}] = s.Value
		}
	}

	out := map[string]NodeFs{}
	for _, s := range avail {
		n := promNodeName(s.Metric)
		if n == "" {
			continue
		}
		total, ok := sizes[fsKey{n, s.Metric["mountpoint"]}]
		if !ok {
			continue // 只有 avail 没有 size：算不出占比，宁可缺这一条也不要瞎猜
		}
		pct := (1 - s.Value/total) * 100
		// 每个节点只留最满的那个挂载点（坑 3：取 max，不是 sum）
		if cur, seen := out[n]; seen && cur.Pct >= pct {
			continue
		}
		out[n] = NodeFs{
			Mountpoint: s.Metric["mountpoint"],
			Pct:        pct,
			TotalBytes: total,
			UsedBytes:  total - s.Value,
		}
	}
	return out, nil
}

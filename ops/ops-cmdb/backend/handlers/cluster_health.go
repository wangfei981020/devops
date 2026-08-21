package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"ops-cmdb-backend/internal/httpx"
	"ops-cmdb-backend/logx"

	"github.com/gin-gonic/gin"
)

// 集群体检总入口：一次问出「这个集群现在有什么问题」。
//
// 做这个是因为一份人工体检要串十几个接口、再用脚本交叉比对才能得出结论，
// 过程里极容易漏（比如只看 conditions 没看 conditions_json、只查某个 ns 就断定全集群没有）。
// 这里把判定固化下来，按严重度排好序直接给结论和处置建议。
//
// 只做能从 CMDB 现有数据可靠判定的项；判不准的宁可不报，避免噪声淹没真问题。

type healthFinding struct {
	Severity string `json:"severity"` // critical/warning/info
	Category string `json:"category"` // 数据可信度/工作负载/节点/存储/成本/治理
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	// Action **给人看的处置动作**。
	//
	//	⚠️ 这里绝不能写 MCP 工具名。
	//
	//	原来 13 条里有 9 条写的是「list_pods 按 restarts 排序，再用 diagnose_pod 查根因」——
	//	那是给 AI 看的文案，被原样搬到了给人看的界面上。一个运维打开这一页，
	//	看到「用 list_pods」，他在网页里执行不了它，也不知道那是什么（OPSCMDB-031 P0-5）。
	//	更糟的是其中还混着一条真能直接跑的 kubectl 命令 ——
	//	作者在两种读者之间摇摆，于是两边都没伺候好。
	//
	//	人的下一步在界面上本来就有：点计数看下钻明细（Key 字段）。
	//	所以 Action 只写**真正的处置动作**（清理什么、改什么配置），
	//	要看清单就点那个数字。
	Action string `json:"action,omitempty"`
	// MCPHint 给 AI 看的工具链。只在 MCP 输出里有意义，界面不显示。
	//
	//	和 Action 分成两个字段，两个读者才能各得其所 ——
	//	共用一句话的话，无论怎么写都有一方读不懂。
	MCPHint string `json:"mcp_hint,omitempty"`
	Count   int    `json:"count,omitempty"`
	// Unit Count 数的是**什么东西**：pod / node / workload / pvc / hpa / resource / image。
	//
	//	⚠️ 没有这个字段时，界面上计数紧挨着 Category 渲染，于是
	//	  「Pod 重启次数异常高(>100)   33   工作负载」
	//	被读成「33 个工作负载」——而重启的是 **Pod**，两者可能差好几倍
	//	（一个工作负载有 N 个副本）。这个数会被用来估算影响面，
	//	差几倍的影响面估算等于没估（OPSCMDB-031 P1-10）。
	//
	//	Category 是**归类**（这条属于哪个体检维度），不是单位。两者不能混。
	Unit string `json:"unit,omitempty"`
	// Key 用于下钻：界面上点「查看」时带这个 key 调 /k8s/health/detail 取明细。
	// 只说「有 56 个 Pod 重启超 100 次」没法处置，得能点进去看是哪 56 个。
	Key string `json:"key,omitempty"`
}

// healthFail 由各 check 项在查询失败时调用，把错误上报给总入口。
// 体检的输出是「没问题」这种断言，查询失败却当成「没查到问题」是最糟的失效模式，
// 所以任何一项查不成，整个体检就不出结论（CMDB-013）。
type healthFail func(item string, err error)

// ClusterHealth GET /api/k8s/health?cluster_id=
func (h *K8sResourceHandler) ClusterHealth(c *gin.Context) {
	cidNum, ok := requireCluster(c, h.DB)
	if !ok {
		return
	}
	cid := itoa(cidNum)
	var firstErr error
	var firstItem string
	fail := func(item string, err error) {
		if firstErr == nil {
			firstErr, firstItem = err, item
		}
		logx.J("cluster_health", "check_fail", map[string]any{"cluster_id": cid, "item": item, "err": err.Error()})
	}
	// 🔴 检查项清单要**显式列出来并发给前端**。
	//
	//	只返回"命中了什么"的话，两个集群输出条数不同时，人无法判断是
	//	  ① 这项跑了、没命中
	//	  ② 这项没跑（缺数据源）
	//	  ③ 这项不适用
	//	三者的下一步完全不同，而界面上长得一模一样（OPSCMDB-076）。
	//
	// ⚠️ 顺序即执行顺序，checkDataFreshness 必须在最前 ——
	//	数据本身不新鲜的话，后面所有结论都不可信。
	checks := []struct {
		Key string
		Run func() []healthFinding
	}{
		{"data_freshness", func() []healthFinding { return h.checkDataFreshness(cid, fail) }},
		{"node_disk", func() []healthFinding { return h.checkNodeDisk(cid) }},
		{"nodes", func() []healthFinding { return h.checkNodes(cid, fail) }},
		{"pods", func() []healthFinding { return h.checkPods(cid, fail) }},
		{"workloads", func() []healthFinding { return h.checkWorkloads(cid, fail) }},
		{"orphans", func() []healthFinding { return h.checkOrphans(cid, fail) }},
		{"images", func() []healthFinding { return h.checkImages(cid, fail) }},
	}
	fs := []healthFinding{}
	checkKeys := make([]string, 0, len(checks))
	for _, ck := range checks {
		fs = append(fs, ck.Run()...)
		checkKeys = append(checkKeys, ck.Key)
	}

	if firstErr != nil {
		// ⚠️ 任何一项查不成，整个体检就不出结论 —— 体检的输出是"没问题"这种断言，
		// 把"查询失败"当成"没查到问题"是最糟的失效模式。
		//
		// 文案走语言包（error.healthIncomplete）：后端拼好中文句子发过来的话，
		// 英文界面上这条永远是中文，而它只在失败路径上出现，平时测不到。
		httpx.FailKey(c, httpx.CodeInternal, "error.healthIncomplete", firstErr,
			map[string]any{"item": firstItem})
		return
	}

	sort.SliceStable(fs, func(i, j int) bool {
		return healthRank(fs[i].Severity) < healthRank(fs[j].Severity)
	})
	sum := gin.H{"total": len(fs), "critical": 0, "warning": 0, "info": 0}
	for _, f := range fs {
		sum[f.Severity] = sum[f.Severity].(int) + 1
	}
	// checks 是分母：**这一轮跑了哪几项**。
	// 有了它，"g32-prod 没有重启异常这一条"才能被读成"跑了没命中"
	// 而不是"这项没跑"。
	c.JSON(http.StatusOK, gin.H{"summary": sum, "findings": fs, "checks": checkKeys})
}

// checkDataFreshness 放在最前面：数据本身不新鲜的话，后面所有结论都不可信。
func (h *K8sResourceHandler) checkDataFreshness(cid string, fail healthFail) []healthFinding {
	var failed, stale int
	// COALESCE 不能省：集群没有 sync_state 行时 SUM 返回 NULL，Scan 会报错，
	// 那属于"没数据"而不是"查询失败"，不该触发体检中止。
	if err := h.DB.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN ok=0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN last_sync IS NULL OR last_sync < NOW() - INTERVAL ? SECOND THEN 1 ELSE 0 END),0)
		FROM k8s_sync_state WHERE cluster_id=?`, staleFactor*120, cid).Scan(&failed, &stale); err != nil {
		fail("data_freshness", err)
		return nil
	}
	out := []healthFinding{}
	if failed > 0 {
		out = append(out, healthFinding{
			Severity: "critical", Category: "数据可信度", Count: failed,
			Key: "sync_failed", Title: "有资源类型采集失败", Unit: "resource",
			Detail:  "本次体检的其余结论基于可能过期的数据，先修采集再看下面的问题",
			Action:  "点右侧的计数看是哪几类资源失败、报什么错；采集修好再看下面的结论",
			MCPHint: "调 data_freshness 看具体是哪类资源、报什么错",
		})
	} else if stale > 0 {
		out = append(out, healthFinding{
			Severity: "warning", Category: "数据可信度", Count: stale,
			Key: "sync_stale", Title: "有资源类型的数据已超出新鲜期", Unit: "resource",
			Detail:  "采集器可能已停止，数据可能不反映现状",
			Action:  "点右侧的计数看是哪几类；如果采集器已停，先把它恢复",
			MCPHint: "调 data_freshness 确认",
		})
	}
	return out
}

// 磁盘水位阈值。85% 起提示、92% 起告警——留出的余量要够撑到人来处理，
// 因为磁盘满不是"性能变差"而是"发布直接失败、Pod 被驱逐"，没有缓冲期。
const (
	diskWarnPct     = 85.0
	diskCriticalPct = 92.0
)

// checkNodeDisk 节点磁盘水位。这是此前完全缺失的一块：
// 「镜像 GC 回收不出空间」只能等它触发事件后从侧面撞见，而那时往往已经在影响发布了。
func (h *K8sResourceHandler) checkNodeDisk(cid string) []healthFinding {
	usage, err := h.nodeDiskUsage(cid)
	if err != nil || len(usage) == 0 {
		// 不能静默返回 nil：那样体检报告看上去像"磁盘检查过了、没问题"。
		// 磁盘满是能直接打垮整个平台的故障（CMDB-012），"没检查"必须说出来。
		reason, action := h.diskUnknownReason(cid, err)
		return []healthFinding{{
			Severity: "info", Category: "数据可信度",
			Key: "node_disk_unknown", Title: "节点磁盘水位未检查", Unit: "node",
			Detail: reason + "；本次体检不覆盖磁盘水位，不代表磁盘没问题",
			Action: action,
		}}
	}
	var warn, crit []string
	for node, pct := range usage {
		switch {
		case pct >= diskCriticalPct:
			crit = append(crit, fmt.Sprintf("%s(%.0f%%)", node, pct))
		case pct >= diskWarnPct:
			warn = append(warn, fmt.Sprintf("%s(%.0f%%)", node, pct))
		}
	}
	sort.Strings(crit)
	sort.Strings(warn)
	out := []healthFinding{}
	if len(crit) > 0 {
		out = append(out, healthFinding{
			Severity: "critical", Category: "节点", Count: len(crit),
			Key: "node_disk_critical", Title: "节点磁盘水位过高(≥92%)", Unit: "node",
			Detail: strings.Join(crit, "、") + "；磁盘满会直接导致镜像拉取失败、Pod 被驱逐，发布随之失败",
			Action: "先清理无用镜像与日志；若 GC 回收不出空间，多为镜像层被正在运行的容器占用，需扩容磁盘",
		})
	}
	if len(warn) > 0 {
		out = append(out, healthFinding{
			Severity: "warning", Category: "节点", Count: len(warn),
			Key: "node_disk_warn", Title: "节点磁盘水位偏高(≥85%)", Unit: "node",
			Detail: strings.Join(warn, "、"),
			Action: "提前清理或扩容，别等触发 DiskPressure 驱逐",
		})
	}
	return out
}

// nodeDiskUsage 取各节点**可写节点级文件系统**的最高水位。
// ⚠️ 不是「根分区」——在 GKE COS 上根分区是只读启动镜像，见下方注释。
func (h *K8sResourceHandler) nodeDiskUsage(cid string) (map[string]float64, error) {
	out := map[string]float64{}
	n, err := strconv.Atoi(cid)
	if err != nil {
		return out, err
	}
	obs := NewObsQueryHandler(h.Store, h.DB, h.Cipher)
	base, token, clusterLabel, err := resolveEndpointFull(h.DB, obs.Cipher, "prometheus", obs.clusterEnv(n), n)
	if err != nil {
		return out, err
	}
	// 口径统一在 nodefs.go —— 曾经四处各写一份 `mountpoint="/"`，同一个错复制了四份。
	fs, err := nodeFsUsage(base, token, clusterSelector(h.DB, clusterLabel, n))
	if err != nil {
		return out, err
	}
	for k, v := range fs {
		out[k] = v.Pct
	}
	return out, nil
}

func (h *K8sResourceHandler) checkNodes(cid string, fail healthFail) []healthFinding {
	out := []healthFinding{}
	var stuck, notReady, pressure int
	if err := h.DB.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN stuck=1 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN ready_status<>'Ready' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN COALESCE(conditions,'')<>'' THEN 1 ELSE 0 END),0)
		FROM k8s_nodes WHERE cluster_id=?`, cid).Scan(&stuck, &notReady, &pressure); err != nil {
		fail("nodes", err)
		return nil
	}
	if stuck > 0 {
		out = append(out, healthFinding{Severity: "critical", Category: "节点", Count: stuck,
			Key: "node_stuck", Title: "节点卡死/失联", Unit: "node", Detail: "Ready 心跳长时间未更新，其上 Pod 可能已不可用",
			Action:  "点计数看是哪些节点；驱逐或重启前先在「变更前检查」页评估影响面",
			MCPHint: "list_nodes 看 health 列，再用 node_impact 评估影响面"})
	}
	if notReady > stuck {
		out = append(out, healthFinding{Severity: "critical", Category: "节点", Count: notReady - stuck,
			Key: "node_notready", Title: "节点未就绪", Unit: "node",
			Action:  "点计数看是哪些节点及各自的状态",
			MCPHint: "list_nodes 查 ready_status"})
	}
	if pressure > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "节点", Count: pressure,
			Key: "node_pressure", Title: "节点存在资源压力", Unit: "node", Detail: "磁盘/内存/PID 压力会触发 Pod 驱逐",
			Action:  "点计数看是哪些节点、承受的是哪类压力（内存/磁盘/PID）",
			MCPHint: "list_nodes 看 conditions 列具体是哪类压力"})
	}
	// 节点版本漂移：同集群不同 kubelet 版本，升级窗口没拉齐
	var versions int
	if err := h.DB.QueryRow(`SELECT COUNT(DISTINCT kubelet_version) FROM k8s_nodes WHERE cluster_id=?`, cid).Scan(&versions); err != nil {
		fail("node_kubelet_drift", err)
		return out
	}
	if versions > 1 {
		out = append(out, healthFinding{Severity: "info", Category: "节点", Count: versions,
			Key: "node_kubelet_drift", Title: "节点 kubelet 版本不一致", Unit: "node", Detail: "存在版本漂移，建议统一升级窗口",
			Action:  "点计数看各节点的 kubelet 版本；版本参差是升级前最该先解决的事",
			MCPHint: "list_nodes 对比 kubelet_version"})
	}
	return out
}

func (h *K8sResourceHandler) checkPods(cid string, fail healthFail) []healthFinding {
	out := []healthFinding{}
	var failed, pending, oom, highRestart int
	if err := h.DB.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN phase='Failed' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN phase='Pending' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN reason='OOMKilled' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN restarts>100 THEN 1 ELSE 0 END),0)
		FROM k8s_pods WHERE cluster_id=?`, cid).Scan(&failed, &pending, &oom, &highRestart); err != nil {
		fail("pods", err)
		return nil
	}
	if highRestart > 0 {
		out = append(out, healthFinding{Severity: "critical", Category: "工作负载", Count: highRestart,
			Key: "pod_high_restart", Title: "Pod 重启次数异常高(>100)", Unit: "pod", Detail: "持续 CrashLoop 的服务，且往往长期无人发现",
			Action:  "点计数看是哪些 Pod 和各自的重启次数、原因",
			MCPHint: "list_pods 按 restarts 排序，再用 diagnose_pod 查根因"})
	}
	if oom > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "工作负载", Count: oom,
			Key: "pod_oomkilled", Title: "Pod 被 OOMKilled", Unit: "pod", Detail: "内存 limit 不足或存在泄漏",
			Action:  "去「闲置与浪费」页看实测用量和建议值，据此调 limit",
			MCPHint: "resource_waste 看实际用量，据此调 limit"})
	}
	if failed > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "工作负载", Count: failed,
			Key: "pod_failed", Title: "存在 Failed 状态的 Pod", Unit: "pod", Detail: "Failed Pod 不会自动清理，会一直占用 etcd 对象",
			Action: "点计数确认这些 Pod 确实可以清理，再执行：kubectl delete pod -A --field-selector=status.phase=Failed"})
	}
	if pending > 0 {
		// 只说「有 N 个 Pending」等于没说——真正要答的是「为什么排不进去/缺什么」。
		// 原因在 k8s_pods.reason 里已经采到了，按原因归类直接给出来。
		f := healthFinding{Severity: "warning", Category: "工作负载", Count: pending,
			Key: "pod_pending", Title: "存在 Pending 的 Pod", Unit: "pod",
			Action:  "点计数看是哪些 Pod 及卡住的原因（多为资源不足或调度约束）",
			MCPHint: "pod_events 看具体某个 Pod 的完整事件"}
		if reasons := h.pendingReasons(cid, fail); len(reasons) > 0 {
			parts := make([]string, 0, len(reasons))
			for _, r := range reasons {
				parts = append(parts, fmt.Sprintf("%s × %d（%s）", r.reason, r.count, explainPendingReason(r.reason)))
			}
			f.Detail = strings.Join(parts, "；")
		} else {
			f.Detail = "调度不上去，常见原因是资源不足或亲和性无法满足"
		}
		out = append(out, f)
	}
	// BestEffort：节点内存压力下最先被驱逐，控制面组件落在这类里尤其危险
	var bestEffort int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_pods
		WHERE cluster_id=? AND phase='Running' AND cpu_req_m=0 AND mem_req_mi=0`, cid).Scan(&bestEffort); err != nil {
		fail("pod_besteffort", err)
		return out
	}
	if bestEffort > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "工作负载", Count: bestEffort,
			Key: "pod_besteffort", Title: "BestEffort Pod（未配 request/limit）", Unit: "pod",
			Detail: "节点内存压力时最先被驱逐；若控制面组件在其中，故障时会先死控制面",
			Action: "给关键组件补 request，或用 LimitRange 兜底"})
	}
	return out
}

type pendingReason struct {
	reason string
	count  int
}

// pendingReasons 把 Pending/启动失败的 Pod 按原因归类。
// 「有 12 个 Pod Pending」这种结论没法行动，「10 个卡在缺 ConfigMap/Secret、2 个资源不足」才有用。
func (h *K8sResourceHandler) pendingReasons(cid string, fail healthFail) []pendingReason {
	rows, err := h.DB.Query(`SELECT COALESCE(reason,''), COUNT(*) FROM k8s_pods
		WHERE cluster_id=? AND phase='Pending' AND COALESCE(reason,'')<>''
		GROUP BY reason ORDER BY COUNT(*) DESC`, cid)
	if err != nil {
		fail("pending_reasons", err)
		return nil
	}
	defer rows.Close()
	out := []pendingReason{}
	for rows.Next() {
		var r pendingReason
		if rows.Scan(&r.reason, &r.count) == nil {
			out = append(out, r)
		}
	}
	return out
}

// explainPendingReason 把 K8s 的原因码翻成「缺什么、该去查什么」。
// 这些码本身对不熟悉 K8s 的人几乎没有信息量，而它们恰恰是发布失败最常见的落点。
func explainPendingReason(reason string) string {
	switch {
	case strings.Contains(reason, "CreateContainerConfigError"):
		return "引用的 ConfigMap/Secret 不存在或键名对不上，容器配置装配不出来"
	case strings.Contains(reason, "ImagePullBackOff"), strings.Contains(reason, "ErrImagePull"):
		return "镜像拉不下来：镜像不存在、tag 写错，或缺 imagePullSecret"
	case strings.Contains(reason, "Unschedulable"):
		return "没有节点能容纳：资源不足、taint 未容忍，或亲和性/拓扑约束无法满足"
	case strings.Contains(reason, "CreateContainerError"):
		return "容器创建失败：常见于挂载路径冲突或运行时报错"
	case strings.Contains(reason, "Init"):
		return "卡在 init 容器：多为它依赖的服务还没就绪"
	case strings.Contains(reason, "ContainerStatusUnknown"):
		return "容器状态未知，通常是节点失联或 kubelet 异常"
	default:
		return "原因码见 pod_events"
	}
}

func (h *K8sResourceHandler) checkWorkloads(cid string, fail healthFail) []healthFinding {
	out := []healthFinding{}
	var degraded, scaledZero int
	if err := h.DB.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN replicas_desired>0 AND replicas_ready<replicas_desired THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN replicas_desired=0 AND kind IN ('Deployment','StatefulSet') THEN 1 ELSE 0 END),0)
		FROM k8s_workloads WHERE cluster_id=?`, cid).Scan(&degraded, &scaledZero); err != nil {
		fail("workloads", err)
		return nil
	}
	if degraded > 0 {
		out = append(out, healthFinding{Severity: "critical", Category: "工作负载", Count: degraded,
			Key: "workload_replica_gap", Title: "工作负载副本未达期望", Unit: "workload", Action: "点计数看是哪些工作负载、就绪几个/期望几个",
			MCPHint: "list_workloads 看 replicas_ready/replicas_desired"})
	}
	if scaledZero > 0 {
		out = append(out, healthFinding{Severity: "info", Category: "治理", Count: scaledZero,
			Key: "workload_scaled_zero", Title: "被缩容到 0 的工作负载", Unit: "workload", Detail: "长期为 0 的多是遗留，占着配置与 HPA",
			Action: "确认是否已废弃，是则连同其 HPA/Service 一并清理"})
	}
	return out
}

func (h *K8sResourceHandler) checkOrphans(cid string, fail healthFail) []healthFinding {
	out := []healthFinding{}
	if n := h.countOrphanHPAs(cid, fail); n > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "治理", Count: n,
			Key: "orphan_hpa", Title: "HPA 指向已不存在的工作负载", Unit: "hpa",
			Detail:  "controller 每 15 秒重试一次并报错，长期累积成海量噪声事件",
			Action:  "点计数看清单；确认废弃后连同其 HPA/Service 一并清理",
			MCPHint: "list_orphans kind=hpa 拿到清单和删除命令"})
	}
	var orphanPVC int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_pvcs p
		LEFT JOIN k8s_pod_volumes v ON v.cluster_id=p.cluster_id AND v.namespace=p.namespace AND v.pvc_name=p.name
		WHERE p.cluster_id=? AND v.id IS NULL`, cid).Scan(&orphanPVC); err != nil {
		fail("orphan_pvc", err)
		return out
	}
	if orphanPVC > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "成本", Count: orphanPVC,
			Key: "orphan_pvc", Title: "PVC 无人挂载但仍在计费", Unit: "pvc",
			Detail:  "多为缩容/迁移/组件卸载后遗留的盘",
			Action:  "点计数看每个卷的容量和月成本；先打快照再删",
			MCPHint: "list_orphans kind=pvc 看逐项金额，快照后删除"})
	}
	return out
}

func (h *K8sResourceHandler) countOrphanHPAs(cid string, fail healthFail) int {
	var n int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_hpas hpa
		LEFT JOIN k8s_workloads w ON w.cluster_id=hpa.cluster_id AND w.namespace=hpa.namespace
			AND w.name=hpa.target_name AND w.kind=hpa.target_kind
		WHERE hpa.cluster_id=? AND w.id IS NULL`, cid).Scan(&n); err != nil {
		fail("orphan_hpa", err)
		return 0
	}
	return n
}

func (h *K8sResourceHandler) checkImages(cid string, fail healthFail) []healthFinding {
	var mutable int
	if err := h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_workloads
		WHERE cluster_id=? AND (image_tag IN ('latest','master','main','dev','stable','') OR image_tag LIKE '%SNAPSHOT%')`,
		cid).Scan(&mutable); err != nil {
		fail("mutable_image_tag", err)
		return nil
	}
	if mutable > 0 {
		return []healthFinding{{Severity: "info", Category: "治理", Count: mutable,
			Key: "workload_mutable_tag", Title: "使用可变镜像 tag（latest/SNAPSHOT 等）", Unit: "workload",
			Detail: "同一 tag 内容会变，故障时无法复现当时的版本，也难以回滚",
			Action: "改用不可变 tag（构建号/commit）"}}
	}
	return nil
}

func healthRank(s string) int {
	switch s {
	case "critical":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

// diskUnknownReason 查不到磁盘水位时，分清到底是哪一种「查不到」。
//
// # 🔴 为什么必须分开
//
// 原来三种情况压成一句「该集群未配置 Prometheus 观测数据源」，
// 处置一律写「给该集群绑定 Prometheus」。
//
// 而真实架构是**一套 VictoriaMetrics 集中采多个集群**
// （infra-01 那套已经采了 UAT / g32-prod / infra-01）——
// 指标一直都在，只是 CMDB 里没把这个集群绑上去、或者 cluster 标签值不对。
//
// 照那句处置去做，就是去装一个本不该装的 Prometheus。
// 🔴 **一条把人引向错误动作的提示，比没有提示更糟。**
//
// 实测 2026-08-19：g32-prod（cluster_id=9）就是这么被报成"未配置"的。
//
// # 三种情况
//
//	① 真的没有任何可用数据源      → 去接一个（可以复用已有的共享源）
//	② 有数据源，但集群标签值不对   → 改标签值，别去装新的
//	③ 数据源和标签都对，就是没数据 → node_exporter 可能没部署
func (h *K8sResourceHandler) diskUnknownReason(cid string, queryErr error) (reason, action string) {
	n, _ := strconv.Atoi(cid)
	obs := NewObsQueryHandler(h.Store, h.DB, h.Cipher)
	base, token, clusterLabel, e := resolveEndpointFull(h.DB, obs.Cipher, "prometheus", obs.clusterEnv(n), n)

	// ① 一个都没匹配上
	if e != nil {
		logx.J("cluster_health", "node_disk_skip", map[string]any{"cluster_id": cid, "err": e.Error()})
		return "没有匹配到可用的指标数据源",
			"到「管理 → 观测端点」接一个。" +
				"⚠️ 如果你们是**一套 VictoriaMetrics 集中采多个集群**，不要为这个集群单独装 Prometheus——" +
				"把已有的那个数据源的适用范围放开（集群留空=不限定），再给本集群配好「指标集群标签值」即可"
	}

	// ② 有源，但这个集群的标签值在数据源里根本不存在 —— 所有隔离查询都会返回空
	if label, value := clusterSelectorParts(h.DB, clusterLabel, n); label != "" {
		if bad := verifyClusterValue(base, token, label, value); bad != nil {
			logx.J("cluster_health", "node_disk_cluster_value_bad", map[string]any{
				"cluster_id": cid, "label": label, "configured": value,
			})
			if msg, ok := bad["error"].(string); ok {
				return "数据源是通的，但**集群隔离标签值配错了**：" + msg,
					"到「集群 → 集群」编辑该集群，改「指标里的集群标签值」，不用动观测端点本身"
			}
		}
	}

	// ③ 源和标签都对，查询就是空
	if queryErr != nil {
		logx.J("cluster_health", "node_disk_skip", map[string]any{"cluster_id": cid, "err": queryErr.Error()})
		return "查询指标源失败：" + queryErr.Error(),
			"先确认端点本身是否可达（「管理 → 观测端点」有连通性测试）"
	}
	return "数据源和集群标签都正常，但查不到任何 node_filesystem 指标",
		"多为这些节点上**没有部署 node_exporter**（或它的 target 全 down）。" +
			"⚠️ 别再去动数据源配置——问题在采集端不在查询端"
}

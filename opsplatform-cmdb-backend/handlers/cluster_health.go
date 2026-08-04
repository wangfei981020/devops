package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"opsplatform-cmdb-backend/logx"

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
	Action   string `json:"action,omitempty"`
	Count    int    `json:"count,omitempty"`
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
	fs := []healthFinding{}
	fs = append(fs, h.checkDataFreshness(cid, fail)...)
	fs = append(fs, h.checkNodeDisk(cid)...)
	fs = append(fs, h.checkNodes(cid, fail)...)
	fs = append(fs, h.checkPods(cid, fail)...)
	fs = append(fs, h.checkWorkloads(cid, fail)...)
	fs = append(fs, h.checkOrphans(cid, fail)...)
	fs = append(fs, h.checkImages(cid, fail)...)

	if firstErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "集群体检未完成(" + firstItem + "): " + firstErr.Error() +
				"；本次未得出任何结论，请勿据此认为集群无问题",
		})
		return
	}

	sort.SliceStable(fs, func(i, j int) bool {
		return healthRank(fs[i].Severity) < healthRank(fs[j].Severity)
	})
	sum := gin.H{"total": len(fs), "critical": 0, "warning": 0, "info": 0}
	for _, f := range fs {
		sum[f.Severity] = sum[f.Severity].(int) + 1
	}
	c.JSON(http.StatusOK, gin.H{"summary": sum, "findings": fs})
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
			Key: "sync_failed", Title: "有资源类型采集失败",
			Detail: "本次体检的其余结论基于可能过期的数据，先修采集再看下面的问题",
			Action: "调 data_freshness 看具体是哪类资源、报什么错",
		})
	} else if stale > 0 {
		out = append(out, healthFinding{
			Severity: "warning", Category: "数据可信度", Count: stale,
			Key: "sync_stale", Title: "有资源类型的数据已超出新鲜期",
			Detail: "采集器可能已停止，数据可能不反映现状",
			Action: "调 data_freshness 确认",
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
		reason := "该集群未配置 Prometheus 观测数据源"
		if err != nil {
			reason = "查询观测数据源失败：" + err.Error()
			logx.J("cluster_health", "node_disk_skip", map[string]any{"cluster_id": cid, "err": err.Error()})
		}
		return []healthFinding{{
			Severity: "info", Category: "数据可信度",
			Key: "node_disk_unknown", Title: "节点磁盘水位未检查",
			Detail: reason + "；本次体检不覆盖磁盘水位，不代表磁盘没问题",
			Action: "在「接入管理 → 观测数据源」给该集群绑定 Prometheus 后重跑体检",
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
			Key: "node_disk_critical", Title: "节点磁盘水位过高(≥92%)",
			Detail: strings.Join(crit, "、") + "；磁盘满会直接导致镜像拉取失败、Pod 被驱逐，发布随之失败",
			Action: "先清理无用镜像与日志；若 GC 回收不出空间，多为镜像层被正在运行的容器占用，需扩容磁盘",
		})
	}
	if len(warn) > 0 {
		out = append(out, healthFinding{
			Severity: "warning", Category: "节点", Count: len(warn),
			Key: "node_disk_warn", Title: "节点磁盘水位偏高(≥85%)",
			Detail: strings.Join(warn, "、"),
			Action: "提前清理或扩容，别等触发 DiskPressure 驱逐",
		})
	}
	return out
}

// nodeDiskUsage 复用 node-usage 的口径取各节点根分区水位。
func (h *K8sResourceHandler) nodeDiskUsage(cid string) (map[string]float64, error) {
	out := map[string]float64{}
	n, err := strconv.Atoi(cid)
	if err != nil {
		return out, err
	}
	obs := NewObsQueryHandler(h.DB, h.Cipher)
	base, token, clusterLabel, err := resolveEndpointFull(h.DB, obs.Cipher, "prometheus", obs.clusterEnv(n), n)
	if err != nil {
		return out, err
	}
	lbl := promLabels(clusterSelector(h.DB, clusterLabel, n), `mountpoint="/"`, `fstype!~"tmpfs|overlay|squashfs|iso9660"`)
	rows, err := promInstant(base, token,
		`(1 - sum by(node,instance)(node_filesystem_avail_bytes`+lbl+`) / sum by(node,instance)(node_filesystem_size_bytes`+lbl+`)) * 100`)
	if err != nil {
		return out, err
	}
	for _, s := range rows {
		k := s.Metric["node"]
		if k == "" {
			k = s.Metric["instance"]
		}
		if k != "" {
			out[k] = s.Value
		}
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
			Key: "node_stuck", Title: "节点卡死/失联", Detail: "Ready 心跳长时间未更新，其上 Pod 可能已不可用",
			Action: "list_nodes 看 health 列，再用 node_impact 评估影响面"})
	}
	if notReady > stuck {
		out = append(out, healthFinding{Severity: "critical", Category: "节点", Count: notReady - stuck,
			Key: "node_notready", Title: "节点未就绪", Action: "list_nodes 查 ready_status"})
	}
	if pressure > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "节点", Count: pressure,
			Key: "node_pressure", Title: "节点存在资源压力", Detail: "磁盘/内存/PID 压力会触发 Pod 驱逐",
			Action: "list_nodes 看 conditions 列具体是哪类压力"})
	}
	// 节点版本漂移：同集群不同 kubelet 版本，升级窗口没拉齐
	var versions int
	if err := h.DB.QueryRow(`SELECT COUNT(DISTINCT kubelet_version) FROM k8s_nodes WHERE cluster_id=?`, cid).Scan(&versions); err != nil {
		fail("node_kubelet_drift", err)
		return out
	}
	if versions > 1 {
		out = append(out, healthFinding{Severity: "info", Category: "节点", Count: versions,
			Key: "node_kubelet_drift", Title: "节点 kubelet 版本不一致", Detail: "存在版本漂移，建议统一升级窗口",
			Action: "list_nodes 对比 kubelet_version"})
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
			Key: "pod_high_restart", Title: "Pod 重启次数异常高(>100)", Detail: "持续 CrashLoop 的服务，且往往长期无人发现",
			Action: "list_pods 按 restarts 排序，再用 diagnose_pod 查根因"})
	}
	if oom > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "工作负载", Count: oom,
			Key: "pod_oomkilled", Title: "Pod 被 OOMKilled", Detail: "内存 limit 不足或存在泄漏",
			Action: "resource_waste 看实际用量，据此调 limit"})
	}
	if failed > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "工作负载", Count: failed,
			Key: "pod_failed", Title: "存在 Failed 状态的 Pod", Detail: "Failed Pod 不会自动清理，会一直占用 etcd 对象",
			Action: "kubectl delete pod -A --field-selector=status.phase=Failed"})
	}
	if pending > 0 {
		// 只说「有 N 个 Pending」等于没说——真正要答的是「为什么排不进去/缺什么」。
		// 原因在 k8s_pods.reason 里已经采到了，按原因归类直接给出来。
		f := healthFinding{Severity: "warning", Category: "工作负载", Count: pending,
			Key: "pod_pending", Title: "存在 Pending 的 Pod", Action: "pod_events 看具体某个 Pod 的完整事件"}
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
			Key: "pod_besteffort", Title: "BestEffort Pod（未配 request/limit）",
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
			Key: "workload_replica_gap", Title: "工作负载副本未达期望", Action: "list_workloads 看 replicas_ready/replicas_desired"})
	}
	if scaledZero > 0 {
		out = append(out, healthFinding{Severity: "info", Category: "治理", Count: scaledZero,
			Key: "workload_scaled_zero", Title: "被缩容到 0 的工作负载", Detail: "长期为 0 的多是遗留，占着配置与 HPA",
			Action: "确认是否已废弃，是则连同其 HPA/Service 一并清理"})
	}
	return out
}

func (h *K8sResourceHandler) checkOrphans(cid string, fail healthFail) []healthFinding {
	out := []healthFinding{}
	if n := h.countOrphanHPAs(cid, fail); n > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "治理", Count: n,
			Key: "orphan_hpa", Title: "HPA 指向已不存在的工作负载",
			Detail: "controller 每 15 秒重试一次并报错，长期累积成海量噪声事件",
			Action: "list_orphans kind=hpa 拿到清单和删除命令"})
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
			Key: "orphan_pvc", Title: "PVC 无人挂载但仍在计费",
			Detail: "多为缩容/迁移/组件卸载后遗留的盘",
			Action: "list_orphans kind=pvc 看逐项金额，快照后删除"})
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
			Key: "workload_mutable_tag", Title: "使用可变镜像 tag（latest/SNAPSHOT 等）",
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

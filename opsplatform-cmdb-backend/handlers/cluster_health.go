package handlers

import (
	"net/http"
	"sort"

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
}

// ClusterHealth GET /api/k8s/health?cluster_id=
func (h *K8sResourceHandler) ClusterHealth(c *gin.Context) {
	cid := c.Query("cluster_id")
	if cid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster_id 必填"})
		return
	}
	fs := []healthFinding{}
	fs = append(fs, h.checkDataFreshness(cid)...)
	fs = append(fs, h.checkNodes(cid)...)
	fs = append(fs, h.checkPods(cid)...)
	fs = append(fs, h.checkWorkloads(cid)...)
	fs = append(fs, h.checkOrphans(cid)...)
	fs = append(fs, h.checkImages(cid)...)

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
func (h *K8sResourceHandler) checkDataFreshness(cid string) []healthFinding {
	var failed, stale int
	_ = h.DB.QueryRow(`SELECT
		SUM(CASE WHEN ok=0 THEN 1 ELSE 0 END),
		SUM(CASE WHEN last_sync IS NULL OR last_sync < NOW() - INTERVAL ? SECOND THEN 1 ELSE 0 END)
		FROM k8s_sync_state WHERE cluster_id=?`, staleFactor*120, cid).Scan(&failed, &stale)
	out := []healthFinding{}
	if failed > 0 {
		out = append(out, healthFinding{
			Severity: "critical", Category: "数据可信度", Count: failed,
			Title:  "有资源类型采集失败",
			Detail: "本次体检的其余结论基于可能过期的数据，先修采集再看下面的问题",
			Action: "调 data_freshness 看具体是哪类资源、报什么错",
		})
	} else if stale > 0 {
		out = append(out, healthFinding{
			Severity: "warning", Category: "数据可信度", Count: stale,
			Title:  "有资源类型的数据已超出新鲜期",
			Detail: "采集器可能已停止，数据可能不反映现状",
			Action: "调 data_freshness 确认",
		})
	}
	return out
}

func (h *K8sResourceHandler) checkNodes(cid string) []healthFinding {
	out := []healthFinding{}
	var stuck, notReady, pressure int
	_ = h.DB.QueryRow(`SELECT
		SUM(CASE WHEN stuck=1 THEN 1 ELSE 0 END),
		SUM(CASE WHEN ready_status<>'Ready' THEN 1 ELSE 0 END),
		SUM(CASE WHEN COALESCE(conditions,'')<>'' THEN 1 ELSE 0 END)
		FROM k8s_nodes WHERE cluster_id=?`, cid).Scan(&stuck, &notReady, &pressure)
	if stuck > 0 {
		out = append(out, healthFinding{Severity: "critical", Category: "节点", Count: stuck,
			Title: "节点卡死/失联", Detail: "Ready 心跳长时间未更新，其上 Pod 可能已不可用",
			Action: "list_nodes 看 health 列，再用 node_impact 评估影响面"})
	}
	if notReady > stuck {
		out = append(out, healthFinding{Severity: "critical", Category: "节点", Count: notReady - stuck,
			Title: "节点未就绪", Action: "list_nodes 查 ready_status"})
	}
	if pressure > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "节点", Count: pressure,
			Title: "节点存在资源压力", Detail: "磁盘/内存/PID 压力会触发 Pod 驱逐",
			Action: "list_nodes 看 conditions 列具体是哪类压力"})
	}
	// 节点版本漂移：同集群不同 kubelet 版本，升级窗口没拉齐
	var versions int
	_ = h.DB.QueryRow(`SELECT COUNT(DISTINCT kubelet_version) FROM k8s_nodes WHERE cluster_id=?`, cid).Scan(&versions)
	if versions > 1 {
		out = append(out, healthFinding{Severity: "info", Category: "节点", Count: versions,
			Title: "节点 kubelet 版本不一致", Detail: "存在版本漂移，建议统一升级窗口",
			Action: "list_nodes 对比 kubelet_version"})
	}
	return out
}

func (h *K8sResourceHandler) checkPods(cid string) []healthFinding {
	out := []healthFinding{}
	var failed, pending, oom, highRestart int
	_ = h.DB.QueryRow(`SELECT
		SUM(CASE WHEN phase='Failed' THEN 1 ELSE 0 END),
		SUM(CASE WHEN phase='Pending' THEN 1 ELSE 0 END),
		SUM(CASE WHEN reason='OOMKilled' THEN 1 ELSE 0 END),
		SUM(CASE WHEN restarts>100 THEN 1 ELSE 0 END)
		FROM k8s_pods WHERE cluster_id=?`, cid).Scan(&failed, &pending, &oom, &highRestart)
	if highRestart > 0 {
		out = append(out, healthFinding{Severity: "critical", Category: "工作负载", Count: highRestart,
			Title: "Pod 重启次数异常高(>100)", Detail: "持续 CrashLoop 的服务，且往往长期无人发现",
			Action: "list_pods 按 restarts 排序，再用 diagnose_pod 查根因"})
	}
	if oom > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "工作负载", Count: oom,
			Title: "Pod 被 OOMKilled", Detail: "内存 limit 不足或存在泄漏",
			Action: "resource_waste 看实际用量，据此调 limit"})
	}
	if failed > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "工作负载", Count: failed,
			Title: "存在 Failed 状态的 Pod", Detail: "Failed Pod 不会自动清理，会一直占用 etcd 对象",
			Action: "kubectl delete pod -A --field-selector=status.phase=Failed"})
	}
	if pending > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "工作负载", Count: pending,
			Title: "存在 Pending 的 Pod", Detail: "调度不上去，常见原因是资源不足或亲和性无法满足",
			Action: "pod_events 看具体调度失败原因"})
	}
	// BestEffort：节点内存压力下最先被驱逐，控制面组件落在这类里尤其危险
	var bestEffort int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_pods
		WHERE cluster_id=? AND phase='Running' AND cpu_req_m=0 AND mem_req_mi=0`, cid).Scan(&bestEffort)
	if bestEffort > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "工作负载", Count: bestEffort,
			Title:  "BestEffort Pod（未配 request/limit）",
			Detail: "节点内存压力时最先被驱逐；若控制面组件在其中，故障时会先死控制面",
			Action: "给关键组件补 request，或用 LimitRange 兜底"})
	}
	return out
}

func (h *K8sResourceHandler) checkWorkloads(cid string) []healthFinding {
	out := []healthFinding{}
	var degraded, scaledZero int
	_ = h.DB.QueryRow(`SELECT
		SUM(CASE WHEN replicas_desired>0 AND replicas_ready<replicas_desired THEN 1 ELSE 0 END),
		SUM(CASE WHEN replicas_desired=0 AND kind IN ('Deployment','StatefulSet') THEN 1 ELSE 0 END)
		FROM k8s_workloads WHERE cluster_id=?`, cid).Scan(&degraded, &scaledZero)
	if degraded > 0 {
		out = append(out, healthFinding{Severity: "critical", Category: "工作负载", Count: degraded,
			Title: "工作负载副本未达期望", Action: "list_workloads 看 replicas_ready/replicas_desired"})
	}
	if scaledZero > 0 {
		out = append(out, healthFinding{Severity: "info", Category: "治理", Count: scaledZero,
			Title: "被缩容到 0 的工作负载", Detail: "长期为 0 的多是遗留，占着配置与 HPA",
			Action: "确认是否已废弃，是则连同其 HPA/Service 一并清理"})
	}
	return out
}

func (h *K8sResourceHandler) checkOrphans(cid string) []healthFinding {
	out := []healthFinding{}
	if n := h.countOrphanHPAs(cid); n > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "治理", Count: n,
			Title:  "HPA 指向已不存在的工作负载",
			Detail: "controller 每 15 秒重试一次并报错，长期累积成海量噪声事件",
			Action: "list_orphans kind=hpa 拿到清单和删除命令"})
	}
	var orphanPVC int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_pvcs p
		LEFT JOIN k8s_pod_volumes v ON v.cluster_id=p.cluster_id AND v.namespace=p.namespace AND v.pvc_name=p.name
		WHERE p.cluster_id=? AND v.id IS NULL`, cid).Scan(&orphanPVC)
	if orphanPVC > 0 {
		out = append(out, healthFinding{Severity: "warning", Category: "成本", Count: orphanPVC,
			Title:  "PVC 无人挂载但仍在计费",
			Detail: "多为缩容/迁移/组件卸载后遗留的盘",
			Action: "list_orphans kind=pvc 看逐项金额，快照后删除"})
	}
	return out
}

func (h *K8sResourceHandler) countOrphanHPAs(cid string) int {
	var n int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_hpas hpa
		LEFT JOIN k8s_workloads w ON w.cluster_id=hpa.cluster_id AND w.namespace=hpa.namespace
			AND w.name=hpa.target_name AND w.kind=hpa.target_kind
		WHERE hpa.cluster_id=? AND w.id IS NULL`, cid).Scan(&n)
	return n
}

func (h *K8sResourceHandler) checkImages(cid string) []healthFinding {
	var mutable int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_workloads
		WHERE cluster_id=? AND (image_tag IN ('latest','master','main','dev','stable','') OR image_tag LIKE '%SNAPSHOT%')`,
		cid).Scan(&mutable)
	if mutable > 0 {
		return []healthFinding{{Severity: "info", Category: "治理", Count: mutable,
			Title:  "使用可变镜像 tag（latest/SNAPSHOT 等）",
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

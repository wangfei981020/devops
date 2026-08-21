package handlers

import (
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// 体检项下钻：把「有 56 个 Pod 重启超 100 次」变成「是哪 56 个」。
//
// 汇总数字本身无法处置——看到 56 之后必须能点进去看清单，否则还得自己去列表页
// 拼筛选条件，而那些条件（restarts>100、cpu_req_m=0 且 mem_req_mi=0、
// tag in latest/SNAPSHOT）在界面上根本没法表达。
//
// 每个 key 对应一条查询，与 cluster_health 里的判定条件**必须保持一致**：
// 汇总用 COUNT、明细用同样的 WHERE，两边条件对不上就会出现「说有 56 个但只列出 40 个」。

const detailLimit = 300

type healthDetail struct {
	sql     string
	columns []string // 展示列名（与 SELECT 顺序一一对应）
	note    string   // 明细的读法提示，可空
}

// healthDetailQuery key → 明细查询。SQL 里第一个 ? 一律是 cluster_id。
var healthDetailQuery = map[string]healthDetail{
	"pod_high_restart": {
		sql: `SELECT namespace,name,restarts,COALESCE(reason,''),phase,COALESCE(node_name,'')
		      FROM k8s_pods WHERE cluster_id=? AND restarts>100 ORDER BY restarts DESC`,
		columns: []string{"命名空间", "Pod", "重启次数", "上次终止原因", "状态", "节点"},
		note:    "按重启次数倒序。拿 diagnose_pod 查根因——它会把日志末尾的报错直接放进 evidence",
	},
	"pod_oomkilled": {
		sql: `SELECT namespace,name,restarts,mem_req_mi,mem_lim_mi,COALESCE(node_name,'')
		      FROM k8s_pods WHERE cluster_id=? AND reason='OOMKilled' ORDER BY restarts DESC`,
		columns: []string{"命名空间", "Pod", "重启次数", "内存 request(Mi)", "内存 limit(Mi)", "节点"},
		note:    "reason=OOMKilled 是「上次终止原因」，不代表此刻正在 OOM；用 resource_waste 看实测用量再定 limit",
	},
	"pod_failed": {
		sql: `SELECT namespace,name,COALESCE(reason,''),COALESCE(node_name,''),start_time
		      FROM k8s_pods WHERE cluster_id=? AND phase='Failed' ORDER BY namespace,name`,
		columns: []string{"命名空间", "Pod", "原因", "节点", "启动时间"},
		note:    "Failed Pod 不会自动清理，一直占 etcd 对象。Evicted 的多为节点资源压力时被驱逐",
	},
	"pod_pending": {
		sql: `SELECT namespace,name,COALESCE(reason,''),COALESCE(node_name,''),start_time
		      FROM k8s_pods WHERE cluster_id=? AND phase='Pending' ORDER BY namespace,name`,
		columns: []string{"命名空间", "Pod", "原因", "节点", "创建时间"},
		note:    "CreateContainerConfigError 用 config_audit 查缺哪个 ConfigMap/Secret；调度类原因看 pod_events",
	},
	"pod_besteffort": {
		sql: `SELECT namespace,name,COALESCE(workload,''),COALESCE(node_name,'')
		      FROM k8s_pods WHERE cluster_id=? AND phase='Running' AND cpu_req_m=0 AND mem_req_mi=0
		      ORDER BY namespace,name`,
		columns: []string{"命名空间", "Pod", "工作负载", "节点"},
		note:    "节点内存压力时这些最先被驱逐。若控制面/网关组件在其中，故障时会先死它们",
	},
	"node_stuck": {
		sql: `SELECT name,COALESCE(pool,''),ready_status,last_heartbeat,COALESCE(conditions,'')
		      FROM k8s_nodes WHERE cluster_id=? AND stuck=1 ORDER BY name`,
		columns: []string{"节点", "节点池", "Ready", "最后心跳", "Conditions"},
		note:    "心跳长时间未更新。其上 Pod 可能已不可用但 API 里仍显示 Running",
	},
	"node_notready": {
		sql: `SELECT name,COALESCE(pool,''),ready_status,last_heartbeat,COALESCE(conditions,'')
		      FROM k8s_nodes WHERE cluster_id=? AND ready_status<>'Ready' ORDER BY name`,
		columns: []string{"节点", "节点池", "Ready", "最后心跳", "Conditions"},
	},
	"node_pressure": {
		sql: `SELECT name,COALESCE(pool,''),COALESCE(conditions,''),ready_status
		      FROM k8s_nodes WHERE cluster_id=? AND conditions<>'' AND conditions IS NOT NULL ORDER BY name`,
		columns: []string{"节点", "节点池", "压力类型", "Ready"},
		note:    "磁盘/内存/PID 压力都会触发 Pod 驱逐",
	},
	"node_kubelet_drift": {
		sql: `SELECT name,COALESCE(pool,''),kubelet_version,COALESCE(os_image,'')
		      FROM k8s_nodes WHERE cluster_id=? ORDER BY kubelet_version,name`,
		columns: []string{"节点", "节点池", "kubelet 版本", "OS"},
		note:    "按版本排序，一眼能看出哪些节点落后",
	},
	"workload_replica_gap": {
		sql: `SELECT namespace,kind,name,replicas_desired,replicas_ready,COALESCE(image,'')
		      FROM k8s_workloads WHERE cluster_id=? AND replicas_desired>0 AND replicas_ready<replicas_desired
		      ORDER BY (replicas_desired-replicas_ready) DESC`,
		columns: []string{"命名空间", "类型", "名称", "期望副本", "就绪副本", "镜像"},
		note:    "按缺口大小倒序。完全起不来的（就绪 0）优先看",
	},
	"workload_scaled_zero": {
		sql: `SELECT namespace,kind,name,COALESCE(image,''),COALESCE(image_tag,'')
		      FROM k8s_workloads WHERE cluster_id=? AND replicas_desired=0 ORDER BY namespace,name`,
		columns: []string{"命名空间", "类型", "名称", "镜像", "tag"},
		note:    "长期为 0 的多是遗留，占着配置与 HPA。确认废弃后连同 HPA/Service 一并清理",
	},
	"workload_mutable_tag": {
		sql: `SELECT namespace,kind,name,COALESCE(image,''),COALESCE(image_tag,'')
		      FROM k8s_workloads WHERE cluster_id=? AND (
		        image_tag='latest' OR image_tag='' OR image_tag LIKE '%SNAPSHOT%'
		        OR image_tag='master' OR image_tag='main' OR image_tag='dev')
		      ORDER BY namespace,name`,
		columns: []string{"命名空间", "类型", "名称", "镜像", "tag"},
		note:    "同一 tag 内容会变，故障时无法复现当时版本，也难以回滚。改用构建号/commit",
	},
	"orphan_hpa": {
		sql: `SELECT h.namespace,h.name,h.target_kind,h.target_name,h.min_replicas,h.max_replicas
		      FROM k8s_hpas h WHERE h.cluster_id=? AND NOT EXISTS (
		        SELECT 1 FROM k8s_workloads w WHERE w.cluster_id=h.cluster_id
		          AND w.namespace=h.namespace AND w.name=h.target_name)
		      ORDER BY h.namespace,h.name`,
		columns: []string{"命名空间", "HPA", "目标类型", "目标名称", "最小副本", "最大副本"},
		note:    "controller 每 15 秒重试一次并报错，长期累积成海量噪声事件",
	},
	"orphan_pvc": {
		sql: `SELECT p.namespace,p.name,p.capacity,COALESCE(p.storage_class,''),p.status
		      FROM k8s_pvcs p WHERE p.cluster_id=? AND NOT EXISTS (
		        SELECT 1 FROM k8s_pod_volumes v WHERE v.cluster_id=p.cluster_id
		          AND v.namespace=p.namespace AND v.pvc_name=p.name)
		      ORDER BY p.namespace,p.name`,
		columns: []string{"命名空间", "PVC", "容量", "storageClass", "状态"},
		note:    "用 list_orphans kind=pvc 可看逐项金额与删除命令。删前先做快照",
	},
	"sync_failed": {
		sql: `SELECT resource,last_sync,COALESCE(err,''),count FROM k8s_sync_state
		      WHERE cluster_id=? AND ok=0 ORDER BY resource`,
		columns: []string{"资源类型", "最后同步", "错误", "条数"},
		note:    "这些资源的数据是上一次成功采集的旧值，先解决采集报错再下结论",
	},
	"sync_stale": {
		sql: `SELECT resource,last_sync,ok,count FROM k8s_sync_state
		      WHERE cluster_id=? ORDER BY last_sync`,
		columns: []string{"资源类型", "最后同步", "上次是否成功", "条数"},
		note:    "按同步时间正序，最旧的在最前",
	},
}

// noDrillHint 明确不支持下钻的项，各自说明原因与替代查法。
//
// 比笼统的「不支持」有用得多——尤其磁盘水位这种：它来自 Prometheus 实时查询、不落库，
// 拿 SQL 查 k8s_nodes 只能返回全部节点。返回 15 个节点却说是「2 个水位偏高」，
// 比不返回更容易让人看错。
var noDrillHint = map[string]string{
	"node_disk_critical": "磁盘水位来自 Prometheus 实时查询、不落库，库里查不到「哪几个节点超阈值」。" +
		"体检项的 detail 里已列出具体节点与百分比；完整数据看「资源使用率」页的 disk_pct 列",
	"node_disk_warn": "同上：磁盘水位是实时指标不落库。体检项 detail 已列出具体节点与百分比",
}

// HealthDetail GET /api/k8s/health/detail?cluster_id=&key=
func (h *K8sResourceHandler) HealthDetail(c *gin.Context) {
	key := c.Query("key")
	if key == "" {
		httpx.RequiredAll(c, "cluster_id", "key")
		return
	}
	cidNum, ok2 := requireCluster(c, h.DB)
	if !ok2 {
		return
	}
	cid := itoa(cidNum)
	if hint, ok := noDrillHint[key]; ok {
		c.JSON(http.StatusOK, gin.H{"key": key, "rows": []any{}, "unsupported": hint})
		return
	}
	d, ok := healthDetailQuery[key]
	if !ok {
		// 明确列出支持哪些 key，而不是给个空结果让人以为「这项没有明细」
		keys := make([]string, 0, len(healthDetailQuery))
		for k := range healthDetailQuery {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		c.JSON(http.StatusOK, gin.H{
			"key": key, "rows": []any{},
			"unsupported": "该体检项暂不支持下钻。已支持: " + strings.Join(keys, ", "),
		})
		return
	}

	rows, err := h.DB.Query(d.sql+" LIMIT "+itoa(detailLimit+1), cid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	out := make([][]any, 0, 64)
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if rows.Scan(ptrs...) != nil {
			continue
		}
		// []byte → string，否则前端拿到的是 base64
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		out = append(out, vals)
	}

	resp := gin.H{"key": key, "columns": d.columns, "rows": out, "count": len(out)}
	if d.note != "" {
		resp["note"] = d.note
	}
	if ps := concentrations(d.columns, out); len(ps) > 0 {
		resp["patterns"] = ps
	}
	if len(out) > detailLimit {
		resp["rows"] = out[:detailLimit]
		resp["count"] = detailLimit
		resp["truncated"] = "结果超过 " + itoa(detailLimit) + " 条，只返回前 " + itoa(detailLimit) +
			" 条；用对应的列表页加筛选条件看全部"
	}
	c.JSON(http.StatusOK, resp)
}

// concentrations 找出「这批问题对象是不是挤在同一个地方」。
//
// # 为什么要有这个
//
// 实测 DEV：11 个 Failed Pod 里 **10 个在 node12**，其中 8 个是 ContainerStatusUnknown。
// 人扫一眼就看出来「这是节点的问题」，而工具逐个报 Pod，一个字都没提这件事 ——
// 于是同一个节点故障被读成 10 个互不相干的 Pod 故障，处置方向完全不同：
//
//	10 个独立故障  → 一个个去查各自的应用
//	1 个节点故障    → 查那个节点，Pod 是结果不是原因
//
// 🔴 这类线索**只在把对象放在一起看时才存在**，逐条诊断永远发现不了。
//
// # 判据
//
// 只看有语义的几列（节点/命名空间/原因），且要求：
//   - 总数 ≥ 4（太少时"集中"没有意义，2 个里 2 个在一起是巧合）
//   - 最大占比 ≥ 60% 且该值至少出现 3 次
//   - **不是全部**都在同一个值上时才更有信息量；全在一起也报，但措辞不同
//
// ⚠️ 阈值宁可保守：一个总在喊"发现规律"的工具，和不喊的一样没人看。
func concentrations(columns []string, rows [][]any) []gin.H {
	const minRows, minHits = 4, 3
	interesting := map[string]string{
		"节点":     "同一个节点",
		"命名空间":   "同一个命名空间",
		"原因":     "同一个原因",
		"上次终止原因": "同一个终止原因",
	}
	out := []gin.H{}
	if len(rows) < minRows {
		return out
	}
	for ci, col := range columns {
		label, ok := interesting[col]
		if !ok {
			continue
		}
		counts := map[string]int{}
		for _, r := range rows {
			if ci >= len(r) {
				continue
			}
			v := strings.TrimSpace(fmt.Sprint(r[ci]))
			if v == "" || v == "<nil>" {
				continue
			}
			counts[v]++
		}
		top, topN := "", 0
		for v, n := range counts {
			if n > topN {
				top, topN = v, n
			}
		}
		if topN < minHits || topN*100 < len(rows)*60 {
			continue
		}
		// 🔴 「原因」这类列往往**就是这个体检项的筛选条件**（pod_failed 全是 Error、
		//	pod_high_restart 全是 CrashLoopBackOff），报"全都一样"是同义反复，不是发现。
		//	所以这两列要求至少有两种取值，集中才算信息。
		//
		//	节点/命名空间不同：没有任何机制强迫这些 Pod 挤在同一个节点上，
		//	**全同反而是最强的信号**，必须报。
		if (col == "原因" || col == "上次终止原因") && len(counts) < 2 {
			continue
		}
		hint := fmt.Sprintf("%d 条里有 %d 条挤在%s（%s）", len(rows), topN, label, top)
		switch col {
		case "节点":
			// 节点是最值得先看的一维：它能把 N 个"应用故障"一次性解释掉
			hint += "。这更像**这个节点**的问题，而不是 " + itoa(topN) + " 个互不相干的故障 —— 先查它"
		case "命名空间":
			hint += "。多为该命名空间整体的配置或依赖问题（缺密钥、依赖服务没起来），别一个个查"
		default:
			hint += "。同因同源，很可能一次就能全修掉"
		}
		out = append(out, gin.H{"column": col, "value": top, "count": topN, "total": len(rows), "hint": hint})
	}
	return out
}

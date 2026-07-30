package handlers

import (
	"net/http"
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
	cid := c.Query("cluster_id")
	key := c.Query("key")
	if cid == "" || key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster_id 和 key 必填"})
		return
	}
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
	if len(out) > detailLimit {
		resp["rows"] = out[:detailLimit]
		resp["count"] = detailLimit
		resp["truncated"] = "结果超过 " + itoa(detailLimit) + " 条，只返回前 " + itoa(detailLimit) +
			" 条；用对应的列表页加筛选条件看全部"
	}
	c.JSON(http.StatusOK, resp)
}

package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// parseCPUCores 把节点 cpu 容量("32"/"32000m") 转毫核。
func parseCPUCores(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if strings.HasSuffix(s, "m") {
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "m"), 64)
		return int64(v)
	}
	v, _ := strconv.ParseFloat(s, 64)
	return int64(v * 1000)
}

// parseMemMi 把节点内存容量(Ki/Mi/Gi) 转 Mi。
func parseMemMi(s string) int64 {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasSuffix(s, "Ki"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "Ki"), 64)
		return int64(v / 1024)
	case strings.HasSuffix(s, "Mi"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "Mi"), 64)
		return int64(v)
	case strings.HasSuffix(s, "Gi"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "Gi"), 64)
		return int64(v * 1024)
	default:
		v, _ := strconv.ParseFloat(s, 64) // 裸字节
		return int64(v / 1024 / 1024)
	}
}

// NodeCapacity 每节点：可分配 vs 已 request vs limit（答"节点够不够 request/还能排多少"）。
func (h *K8sResourceHandler) NodeCapacity(c *gin.Context) {
	cid := c.Query("cluster_id")
	if cid == "" {
		c.JSON(400, gin.H{"error": "cluster_id 必填"})
		return
	}
	// 每节点 request/limit 汇总
	type agg struct{ cpuReq, memReq, cpuLim, memLim, pods int64 }
	sums := map[string]*agg{}
	if rows, _ := h.DB.Query(`SELECT node_name, COALESCE(SUM(cpu_req_m),0), COALESCE(SUM(mem_req_mi),0),
		COALESCE(SUM(cpu_lim_m),0), COALESCE(SUM(mem_lim_mi),0), COUNT(*)
		FROM k8s_pods WHERE cluster_id=? AND node_name<>'' AND phase NOT IN ('Succeeded','Failed') GROUP BY node_name`, cid); rows != nil {
		for rows.Next() {
			var n string
			a := &agg{}
			_ = rows.Scan(&n, &a.cpuReq, &a.memReq, &a.cpuLim, &a.memLim, &a.pods)
			sums[n] = a
		}
		rows.Close()
	}
	out := []gin.H{}
	rows, err := h.DB.Query(`SELECT name,pool,cpu_cap,mem_cap FROM k8s_nodes WHERE cluster_id=? ORDER BY name`, cid)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	for rows.Next() {
		var name, pool, cpuCap, memCap string
		_ = rows.Scan(&name, &pool, &cpuCap, &memCap)
		allocCPU := parseCPUCores(cpuCap)
		allocMem := parseMemMi(memCap)
		a := sums[name]
		if a == nil {
			a = &agg{}
		}
		pct := func(used, alloc int64) float64 {
			if alloc <= 0 {
				return 0
			}
			return float64(used) / float64(alloc) * 100
		}
		out = append(out, gin.H{
			"node": name, "pool": pool, "pods": a.pods,
			"alloc_cpu_m": allocCPU, "req_cpu_m": a.cpuReq, "lim_cpu_m": a.cpuLim, "cpu_req_pct": pct(a.cpuReq, allocCPU), "cpu_lim_pct": pct(a.cpuLim, allocCPU),
			"alloc_mem_mi": allocMem, "req_mem_mi": a.memReq, "lim_mem_mi": a.memLim, "mem_req_pct": pct(a.memReq, allocMem), "mem_lim_pct": pct(a.memLim, allocMem),
		})
	}
	c.JSON(http.StatusOK, out)
}

// NsOverview 命名空间/项目 Pod 概览：总/Running/失败/Pending + 失败清单(带原因)。
func (h *K8sResourceHandler) NsOverview(c *gin.Context) {
	cid := c.Query("cluster_id")
	if cid == "" {
		c.JSON(400, gin.H{"error": "cluster_id 必填"})
		return
	}
	ns := c.Query("namespace")
	project := c.Query("project") // 按项目=该项目映射到的命名空间集合
	where := "cluster_id=?"
	args := []any{cid}
	if ns != "" {
		where += " AND namespace=?"
		args = append(args, ns)
	}
	if project != "" {
		where += " AND namespace IN (SELECT namespace FROM k8s_ns_project WHERE cluster_id=? AND project=?)"
		args = append(args, cid, project)
	}
	total, running, pending, failed := 0, 0, 0, 0
	failures := []gin.H{}
	rows, err := h.DB.Query(`SELECT namespace,name,phase,COALESCE(reason,''),restarts,workload FROM k8s_pods WHERE `+where+` ORDER BY namespace,name`, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	for rows.Next() {
		var pns, name, phase, reason, wl string
		var restarts int
		_ = rows.Scan(&pns, &name, &phase, &reason, &restarts, &wl)
		total++
		switch phase {
		case "Running", "Succeeded":
			running++
		case "Pending":
			pending++
		default:
			failed++
		}
		// 失败/异常:非 Running 且非 Succeeded，或有原因，或重启多
		if (phase != "Running" && phase != "Succeeded") || reason != "" {
			failures = append(failures, gin.H{"namespace": pns, "pod": name, "phase": phase, "reason": reason, "restarts": restarts, "workload": wl})
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"total": total, "running": running, "pending": pending, "failed": failed, "failures": failures,
	})
}

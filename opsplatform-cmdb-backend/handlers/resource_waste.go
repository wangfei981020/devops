package handlers

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"
)

// 资源浪费与闲置成本。
//
// K8s 里「申请了多少」和「实际用了多少」是两回事，而钱是按申请（request 影响调度装箱）
// 和机器规格（实付）算的。UAT 实测：CPU request 191 核、实际只用 19.5 核，利用率 10.2%；
// 节点实付 $9,707/月，但按 request 只分摊出 $5,674，**差额 $4,033/月是买了没人用的闲置容量**，
// 这笔钱在任何按 request 分摊的成本看板里都是隐形的。
//
// 这两个接口把「浪费在哪、还能省多少」直接算出来，免得每次靠人工导数据比对。

type wasteItem struct {
	Namespace    string  `json:"namespace"`
	Workload     string  `json:"workload"`
	Replicas     int     `json:"replicas"`
	CPUReqM      int     `json:"cpu_req_m"`
	CPUUsedM     float64 `json:"cpu_used_m"`
	CPUUsagePct  float64 `json:"cpu_usage_pct"`
	MemReqMi     int     `json:"mem_req_mi"`
	MemUsedMi    float64 `json:"mem_used_mi"`
	MemUsagePct  float64 `json:"mem_usage_pct"`
	SuggestCPUM  int     `json:"suggest_cpu_req_m"` // 建议 request（实测 × 安全系数）
	SuggestMemMi int     `json:"suggest_mem_req_mi"`
	Note         string  `json:"note,omitempty"`
}

// suggestFactor 推荐 request = 实测用量 × 该系数。
// 取 1.5 是留出突发余量又不至于回到原来的虚高；低于实测值会导致调度装箱过密、
// 节点一有压力就驱逐，所以不能按实测值本身给。
const suggestFactor = 1.5

// ResourceWaste GET /api/k8s/resource-waste?cluster_id=&namespace=&top=
// 按「浪费绝对量」排序，先解决大头。
func (h *ObsQueryHandler) ResourceWaste(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	if cid == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "cluster_id 必填"})
		return
	}
	usage, err := h.podUsageMap(cid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": "取实际用量失败(Prometheus 不可用?): " + err.Error()})
		return
	}
	if len(usage) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": false,
			"error": "没有取到任何 Pod 实际用量，无法计算浪费；请先确认该集群的 Prometheus 数据源已配置且可达"})
		return
	}

	type agg struct {
		reqCPU, reqMem   int
		usedCPU, usedMem float64
		replicas         int
	}
	byWorkload := map[string]*agg{}
	nsFilter := c.Query("namespace")

	rows, err := h.DB.Query(`SELECT namespace, name, COALESCE(workload,''), cpu_req_m, mem_req_mi
		FROM k8s_pods WHERE cluster_id=? AND phase='Running'`, cid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	for rows.Next() {
		var ns, pod, wl string
		var reqCPU, reqMem int
		if rows.Scan(&ns, &pod, &wl, &reqCPU, &reqMem) != nil {
			continue
		}
		if nsFilter != "" && ns != nsFilter {
			continue
		}
		if wl == "" {
			wl = pod
		}
		u := usage[ns+"/"+pod]
		k := ns + "/" + wl
		a := byWorkload[k]
		if a == nil {
			a = &agg{}
			byWorkload[k] = a
		}
		a.reqCPU += reqCPU
		a.reqMem += reqMem
		a.usedCPU += u.CPUM
		a.usedMem += u.MemMi
		a.replicas++
	}

	items := []wasteItem{}
	for k, a := range byWorkload {
		ns, wl := splitKey(k)
		it := wasteItem{
			Namespace: ns, Workload: wl, Replicas: a.replicas,
			CPUReqM: a.reqCPU, CPUUsedM: round2(a.usedCPU),
			MemReqMi: a.reqMem, MemUsedMi: round2(a.usedMem),
		}
		if a.reqCPU > 0 {
			it.CPUUsagePct = round2(a.usedCPU * 100 / float64(a.reqCPU))
		}
		if a.reqMem > 0 {
			it.MemUsagePct = round2(a.usedMem * 100 / float64(a.reqMem))
		}
		// 没配 request 的不给建议：那是另一类问题（BestEffort，节点压力下最先被驱逐）
		if a.reqCPU == 0 && a.reqMem == 0 {
			it.Note = "未配置 request（BestEffort，节点内存压力时最先被驱逐）"
		} else {
			it.SuggestCPUM = suggestValue(a.usedCPU/float64(max1(a.replicas)), 10)
			it.SuggestMemMi = suggestValue(a.usedMem/float64(max1(a.replicas)), 64)
		}
		items = append(items, it)
	}
	// 按 CPU 浪费绝对量排序：先动大头，收益最直接
	sort.Slice(items, func(i, j int) bool {
		wi := float64(items[i].CPUReqM) - items[i].CPUUsedM
		wj := float64(items[j].CPUReqM) - items[j].CPUUsedM
		return wi > wj
	})
	if top, _ := strconv.Atoi(c.Query("top")); top > 0 && len(items) > top {
		items = items[:top]
	}

	var totReqCPU, totReqMem int
	var totUsedCPU, totUsedMem float64
	for _, it := range items {
		totReqCPU += it.CPUReqM
		totReqMem += it.MemReqMi
		totUsedCPU += it.CPUUsedM
		totUsedMem += it.MemUsedMi
	}
	c.JSON(http.StatusOK, gin.H{
		"ok": true,
		"summary": gin.H{
			"cpu_request_cores": round2(float64(totReqCPU) / 1000),
			"cpu_used_cores":    round2(totUsedCPU / 1000),
			"cpu_usage_pct":     pct(totUsedCPU, float64(totReqCPU)),
			"cpu_wasted_cores":  round2(float64(totReqCPU-int(totUsedCPU)) / 1000),
			"mem_request_gi":    round2(float64(totReqMem) / 1024),
			"mem_used_gi":       round2(totUsedMem / 1024),
			"mem_usage_pct":     pct(totUsedMem, float64(totReqMem)),
			"mem_wasted_gi":     round2((float64(totReqMem) - totUsedMem) / 1024),
			"suggest_factor":    suggestFactor,
		},
		"items": items,
	})
}

// IdleCost GET /api/k8s/idle-cost?cluster_id=
// 把「实付 / 已分摊 / 闲置」拆开。按 request 分摊的成本视图看不见闲置那部分，
// 而缩容能省的正是它。
func (h *ObsQueryHandler) IdleCost(c *gin.Context) {
	cidStr := c.Query("cluster_id")
	rows, err := h.DB.Query(`SELECT n.cluster_id, c.name, n.name, n.cpu_cap, n.mem_cap,
		COALESCE(n.monthly_cost_override,0), COALESCE(c.location,''), COALESCE(c.cost_mode,'cloud'), n.machine_type
		FROM k8s_nodes n JOIN k8s_clusters c ON c.id=n.cluster_id
		WHERE (?='' OR n.cluster_id=?)`, cidStr, cidStr)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	rc := newRateCache(h.DB)
	type clusterAgg struct {
		name              string
		nodes             int
		actualUSD         float64
		cpuCapM, memCapMi int
	}
	byCluster := map[int]*clusterAgg{}
	for rows.Next() {
		var cid int
		var cname, nname, cpuCap, memCap, loc, mode, mt string
		var override float64
		if rows.Scan(&cid, &cname, &nname, &cpuCap, &memCap, &override, &loc, &mode, &mt) != nil {
			continue
		}
		if mode == "none" {
			continue // 本地集群不计费
		}
		cpuM, memMi := coresToM(cpuCap), memToMi(memCap)
		monthly := override
		if monthly <= 0 {
			hourly, _, _, _ := rc.hostHourly(loc, familyOf(mt), cpuM/1000, memMi, "RUNNING", nil)
			monthly = round2(hourly * 730)
		}
		a := byCluster[cid]
		if a == nil {
			a = &clusterAgg{name: cname}
			byCluster[cid] = a
		}
		a.nodes++
		a.actualUSD += monthly
		a.cpuCapM += cpuM
		a.memCapMi += memMi
	}

	out := []gin.H{}
	for cid, a := range byCluster {
		var reqCPU, reqMem int
		_ = h.DB.QueryRow(`SELECT COALESCE(SUM(cpu_req_m),0), COALESCE(SUM(mem_req_mi),0)
			FROM k8s_pods WHERE cluster_id=? AND phase='Running'`, cid).Scan(&reqCPU, &reqMem)
		// 与成本模型保持同一口径：CPU 与内存各占节点成本的一半
		allocated := 0.0
		if a.cpuCapM > 0 {
			allocated += a.actualUSD * 0.5 * float64(reqCPU) / float64(a.cpuCapM)
		}
		if a.memCapMi > 0 {
			allocated += a.actualUSD * 0.5 * float64(reqMem) / float64(a.memCapMi)
		}
		idle := a.actualUSD - allocated
		out = append(out, gin.H{
			"cluster_id": cid, "cluster": a.name, "nodes": a.nodes,
			"actual_monthly_usd":    round2(a.actualUSD),
			"allocated_monthly_usd": round2(allocated),
			"idle_monthly_usd":      round2(idle),
			"idle_pct":              pct(idle, a.actualUSD),
			"idle_yearly_usd":       round2(idle * 12),
			"cpu_request_pct":       pct(float64(reqCPU), float64(a.cpuCapM)),
			"mem_request_pct":       pct(float64(reqMem), float64(a.memCapMi)),
			"note": "闲置 = 实付 − 已按 request 分摊。这部分买了但没有任何工作负载申请，" +
				"是缩容能直接省下的上限；先用 resource_waste 校准 request，再缩节点。",
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["idle_monthly_usd"].(float64) > out[j]["idle_monthly_usd"].(float64)
	})
	c.JSON(http.StatusOK, gin.H{"clusters": out})
}

// podUsageMap 取全集群 Pod 实时用量（复用 PodUsage 的 PromQL 口径）。
func (h *ObsQueryHandler) podUsageMap(cid int) (map[string]podUse, error) {
	base, token, clusterLabel, err := resolveEndpointFull(h.DB, h.Cipher, "prometheus", h.clusterEnv(cid), cid)
	if err != nil {
		return nil, err
	}
	// 浪费排行/闲置成本是拿实测用量去比 request，混进别的集群的同名 Pod 会直接算错钱。
	lbl := promLabels(clusterSelector(h.DB, clusterLabel, cid), `container!=""`, `container!="POD"`)
	out := map[string]podUse{}
	// PROD-014：容器级指标被重复采集 3 份，不去重的话这里算出来的用量是真值的 3 倍，
	// 而浪费排行/闲置成本正是拿它去比 request——结论会整个反过来
	warnIfDuplicateSeries(base, token, lbl)
	cpu, err := promInstant(base, token,
		dedupContainerSum("namespace,pod", `rate(container_cpu_usage_seconds_total`+lbl+`[5m])`)+` * 1000`)
	if err != nil {
		return nil, err
	}
	for _, s := range cpu {
		k := s.Metric["namespace"] + "/" + s.Metric["pod"]
		u := out[k]
		u.CPUM = s.Value
		out[k] = u
	}
	mem, err := promInstant(base, token,
		dedupContainerSum("namespace,pod", `container_memory_working_set_bytes`+lbl)+` / 1024 / 1024`)
	if err != nil {
		return out, nil // CPU 已经拿到，内存失败不至于整体报废
	}
	for _, s := range mem {
		k := s.Metric["namespace"] + "/" + s.Metric["pod"]
		u := out[k]
		u.MemMi = s.Value
		out[k] = u
	}
	return out, nil
}

type podUse struct{ CPUM, MemMi float64 }

// suggestValue 实测值 × 安全系数，并向上取整到 step 的整数倍（避免给出 137m 这种没人会填的数）。
func suggestValue(used float64, step int) int {
	v := int(used*suggestFactor + 0.5)
	if v < step {
		return step
	}
	if r := v % step; r != 0 {
		v += step - r
	}
	return v
}

func splitKey(k string) (string, string) {
	for i := 0; i < len(k); i++ {
		if k[i] == '/' {
			return k[:i], k[i+1:]
		}
	}
	return "", k
}

func pct(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return round2(a * 100 / b)
}

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

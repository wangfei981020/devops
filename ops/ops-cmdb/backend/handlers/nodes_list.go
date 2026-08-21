package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strings"
	"time"

	"ops-cmdb-backend/k8ssource"
	"ops-cmdb-backend/logx"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/httpx"
)

// 节点列表。主机页的**对偶**：同一台机器，两个问题。
//
//	主机页  这台机器在不在、多少钱、谁的
//	节点页  它还能不能调度、上面跑了什么、心跳还在不在
//
// 所以两页有意重叠对象，并互相链接（CONVENTIONS §2.7.1）。

type NodeListHandler struct{ DB *sql.DB }

func NewNodeListHandler(db *sql.DB) *NodeListHandler { return &NodeListHandler{DB: db} }

func (h *NodeListHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/node-list", h.List)
}

// heartbeatStaleAfter 心跳多久没更新就认为"这个状态已经不可信了"。
//
// kubelet 默认每 10s 上报一次，node-monitor-grace-period 是 40s。
// 取 5 分钟：足够宽，不会把一次网络抖动判成失联；又足够短，
// 真失联时不会让人对着一个几小时前的 Ready 做决定。
// 判定本身在采集时刻做（k8ssource.HeartbeatStaleAfter），这里只消费结果。
// 保留引用是为了让「阈值改了但这边忘了跟」不可能发生——只有一个定义。
const heartbeatStaleAfter = k8ssource.HeartbeatStaleAfter

// collectionStaleAfter 采集本身多久没跑就认为整份数据不可信。
//
// 取同步周期的 3 倍：偶尔一轮慢了不算，连续错过三轮才报——
// 那时候已经不是抖动了。
const collectionStaleAfter = 3 * k8ssource.DefaultSyncIntervalSec * time.Second

type nodeOut struct {
	ClusterID int `json:"cluster_id"`
	// ClusterName **技术名**（kubectl / kubeconfig 里的那个），不是别名。
	//
	// 🔴 这里原来装的是 `COALESCE(display_name, name)` —— 一个字段两种含义，
	//	前端拿到「开发环境集群」就只能显示这一个值，
	//	想按全站统一的 `别名（技术名）` 渲染也拿不到技术名（OPSCMDB-078）。
	//	技术名是与 kubectl 对照、搜索、提工单时唯一可靠的标识，必须传过来。
	ClusterName string `json:"cluster_name"`
	// ClusterDisplay 别名。没设别名时等于技术名 —— 让前端不必判空。
	ClusterDisplay string `json:"cluster_display_name"`
	Name           string `json:"name"`
	Pool           string `json:"pool"`
	Roles          string `json:"roles"`
	InternalIP     string `json:"internal_ip"`
	MachineType    string `json:"machine_type"`
	CPUCap         string `json:"cpu_cap"`
	MemCap         string `json:"mem_cap"`
	OSImage        string `json:"os_image"`
	Kubelet        string `json:"kubelet_version"`

	// ReadyStatus 节点自报的状态，**原样透传**（Ready / NotReady / Unknown）。
	// 不要在后端把它归并成布尔量：Unknown 是"控制面也联系不上它"，
	// 与 NotReady（联系得上，但它说自己没准备好）是两回事。
	ReadyStatus string `json:"ready_status"`

	// HeartbeatStale 状态是否已经过期。
	//
	// ⚠️ 这是这个接口最重要的一个字段。kubelet 停止上报时，
	// ready_status 会**停在最后一次的值**上 —— 一个已经失联两小时的节点
	// 在库里仍然是 Ready。只显示 ready_status 等于告诉用户"它好着呢"。
	HeartbeatStale bool   `json:"heartbeat_stale"`
	LastHeartbeat  string `json:"last_heartbeat,omitempty"`
	// StaleReason 状态不可信的原因：heartbeat=节点心跳停了；collection=采集本身停了。
	// ⚠️ 两者处置完全不同，界面不能都写成「节点失联」。
	StaleReason string `json:"stale_reason,omitempty"`

	// —— 容量与装箱 ——
	//
	// 这几项原本只有 /k8s/node-capacity 有，节点页看不到，于是"这台还能不能再排"
	// 只能回 kubectl describe。合并进列表接口而不是让前端发两个请求再 join：
	// 分两个接口的话，按节点池筛选就没法走服务端，翻页也会错位。
	//
	// ⚠️ 这里全是 **request/limit 的装箱率**，不是实际用量。
	// 两者差别极大（UAT 实测 request 装箱 48%，实际 CPU 只用了 5%），
	// 不能拿装箱率回答"这台机器忙不忙"。实际用量要 Prometheus，见 UsageAvailable。
	AllocCPUm  int64   `json:"alloc_cpu_m"`
	ReqCPUm    int64   `json:"req_cpu_m"`
	LimCPUm    int64   `json:"lim_cpu_m"`
	CPUReqPct  float64 `json:"cpu_req_pct"`
	CPULimPct  float64 `json:"cpu_lim_pct"`
	AllocMemMi int64   `json:"alloc_mem_mi"`
	ReqMemMi   int64   `json:"req_mem_mi"`
	LimMemMi   int64   `json:"lim_mem_mi"`
	MemReqPct  float64 `json:"mem_req_pct"`
	MemLimPct  float64 `json:"mem_lim_pct"`

	// Conditions 压力位摘要（MemoryPressure / DiskPressure 等）。
	// 空字符串 = 没有压力位，不是"没采到"——采不到时整行都不会有。
	Conditions string `json:"conditions"`

	// PodCount 节点自报的 Pod 数；PodsCollected 是我们实际采到的行数。
	// 两个都给：对不上说明两张表来自不同轮次的同步，而我们不知道哪份新。
	PodCount      int    `json:"pod_count"`
	PodsCollected *int64 `json:"pods_collected"`

	// HostCIID 关联到的主机台账 ID。
	//
	// ⚠️ 0 表示**没关联上**，不是"没有主机"。常见原因：自建机不在云台账里、
	// 节点名与实例名不一致且 IP 也对不上。界面要显式说"未关联"并给出
	// 可以去查什么，而不是留白——留白会被当成"这台机器不用管"。
	HostCIID int    `json:"host_ci_id"`
	HostName string `json:"host_name,omitempty"`

	SyncedAt string `json:"synced_at,omitempty"`
}

// List GET /api/k8s/node-list
//
//	@Summary		节点列表
//	@Description	含心跳新鲜度、压力位与主机台账关联。心跳过期时状态不可信，字段 heartbeat_stale 为 true。
//	@Tags			k8s
//	@Produce		json
//	@Param			page	query		int		false	"页码，从 1 开始"
//	@Param			size	query		int		false	"每页条数"
//	@Param			q		query		string	false	"按节点名/IP/节点池搜索"
//	@Param			cluster	query		string	false	"集群名筛选，all 或集群名"
//	@Param			status	query		string	false	"状态筛选。pressure=磁盘/内存/PID 压力（节点仍 Ready 但快撑不住）"	Enums(all, ready, notready, stale, pressure)
//	@Param			sort	query		string	false	"排序字段，前缀 - 为降序"	Enums(name, cluster, status, pods, heartbeat)
//	@Success		200		{object}	httpx.ListResponse[handlers.nodeOut]
//	@Router			/k8s/node-list [get]
func (h *NodeListHandler) List(c *gin.Context) {
	q := httpx.BindPage(c, "cluster", "status", "pool")

	rows, err := h.DB.Query(`SELECT n.cluster_id, COALESCE(cl.name, ''), COALESCE(cl.display_name, cl.name, ''),
		n.name, n.pool, n.roles, n.internal_ip, n.machine_type, n.cpu_cap, n.mem_cap,
		n.os_image, n.kubelet_version, n.ready_status, n.last_heartbeat, n.conditions,
		n.pod_count, n.synced_at, n.hb_stale,
		COALESCE(hc.id, 0), COALESCE(hc.name, '')
		FROM k8s_nodes n
		-- 🔴 INNER JOIN：与 overview 的 nodesStale 计数保持同一口径。
		-- 用 LEFT JOIN 的话，集群已删的孤儿行会带着空集群名混进列表；
		-- 而 overview 那边曾经算进计数、这边又展示不出来，两边对不上。
		-- 孤儿行由 overview 的 orphanNodeRows 单独报，不在这里混淆节点健康。
		JOIN k8s_clusters cl ON cl.id = n.cluster_id
		-- 关联主机台账：先按内网 IP，再按名字兜底。
		-- 只按名字的话，k3s 这类改过 --node-name 的集群永远关联不上，
		-- 而且不会报错，只会一直显示"未关联"。
		LEFT JOIN hosts h ON (h.internal_ip = n.internal_ip AND n.internal_ip <> '')
		LEFT JOIN cis hc ON hc.id = h.ci_id AND hc.type = 'host'
		ORDER BY n.cluster_id, n.name`)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	now := time.Now()
	all := []nodeOut{}
	for rows.Next() {
		var o nodeOut
		var hb, syncedAt sql.NullTime
		var hbStale int
		if err := rows.Scan(&o.ClusterID, &o.ClusterName, &o.ClusterDisplay, &o.Name, &o.Pool, &o.Roles,
			&o.InternalIP, &o.MachineType, &o.CPUCap, &o.MemCap, &o.OSImage, &o.Kubelet,
			&o.ReadyStatus, &hb, &o.Conditions, &o.PodCount, &syncedAt, &hbStale,
			&o.HostCIID, &o.HostName); err != nil {
			continue
		}
		if hb.Valid {
			o.LastHeartbeat = hb.Time.Format(time.RFC3339)
		}
		// 🔴 不要拿 last_heartbeat 减 NOW() 来判失联。
		// 那个值被 bucketHeartbeat 取整到 5 分钟了，年龄天然虚高，
		// 用 5 分钟阈值去卡会把健康节点判成失联（UAT 16 台里误报 8 台）。
		// 判定在采集时刻用未取整的心跳算好，存在 hb_stale 列里。
		o.HeartbeatStale = hbStale != 0
		if o.HeartbeatStale {
			o.StaleReason = "heartbeat"
		}
		if syncedAt.Valid {
			o.SyncedAt = syncedAt.Time.Format(time.RFC3339)
			// ⚠️ 采集停了的话，hb_stale 会**冻结在最后一次的值**上，
			// 于是一屋子节点显示全绿——「采集挂了」被渲染成「一切正常」。
			// 所以采集本身过期时，这一行的状态一律标为不可信，
			// 但原因写成 collection：处置完全不同（去看采集，不是去看节点）。
			if now.Sub(syncedAt.Time) > collectionStaleAfter {
				o.HeartbeatStale = true
				o.StaleReason = "collection"
			}
		} else {
			o.HeartbeatStale = true
			o.StaleReason = "collection"
		}
		all = append(all, o)
	}

	h.fillPodCounts(all)
	h.fillCapacity(all)

	items, total, facets := nodePage(all, q)
	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(facets))
}

// fillCapacity 一次把所有集群的 request/limit 汇总查出来再归位，避免 N+1。
//
// ⚠️ 只统计**在跑的** Pod：Succeeded/Failed 的 Pod 已经不占资源了，
// 把它们算进去会让装箱率虚高，看着像"排满了"其实还有位置。
// 判据与 /k8s/node-capacity 保持一致，两个接口不能给出不同的数字。
func (h *NodeListHandler) fillCapacity(all []nodeOut) {
	if len(all) == 0 {
		return
	}
	type key struct {
		cid  int
		name string
	}
	type agg struct{ cpuReq, memReq, cpuLim, memLim int64 }
	sums := map[key]*agg{}
	rows, err := h.DB.Query(`SELECT cluster_id, node_name,
		COALESCE(SUM(cpu_req_m),0), COALESCE(SUM(mem_req_mi),0),
		COALESCE(SUM(cpu_lim_m),0), COALESCE(SUM(mem_lim_mi),0)
		FROM k8s_pods WHERE node_name<>'' AND phase NOT IN ('Succeeded','Failed')
		GROUP BY cluster_id, node_name`)
	if err != nil {
		// 查不出来就让容量字段保持零值。零值在前端会被渲染成「未采到」而不是 0%，
		// 见 columns.tsx —— 装箱率恰好是 0 的节点极其罕见，把 0 当"没数据"更安全
		logx.J("nodes", "capacity_query_failed", map[string]any{"err": err.Error()})
		return
	}
	for rows.Next() {
		var cid int
		var name string
		a := &agg{}
		if rows.Scan(&cid, &name, &a.cpuReq, &a.memReq, &a.cpuLim, &a.memLim) == nil {
			sums[key{cid, name}] = a
		}
	}
	rows.Close()

	pct := func(used, alloc int64) float64 {
		if alloc <= 0 {
			return 0
		}
		return float64(used) / float64(alloc) * 100
	}
	for i := range all {
		n := &all[i]
		n.AllocCPUm = parseCPUCores(n.CPUCap)
		n.AllocMemMi = parseMemMi(n.MemCap)
		a := sums[key{n.ClusterID, n.Name}]
		if a == nil {
			a = &agg{}
		}
		n.ReqCPUm, n.LimCPUm = a.cpuReq, a.cpuLim
		n.ReqMemMi, n.LimMemMi = a.memReq, a.memLim
		n.CPUReqPct, n.CPULimPct = pct(a.cpuReq, n.AllocCPUm), pct(a.cpuLim, n.AllocCPUm)
		n.MemReqPct, n.MemLimPct = pct(a.memReq, n.AllocMemMi), pct(a.memLim, n.AllocMemMi)
	}
}

// fillPodCounts 一次查全部 Pod 计数再归位，避免 N+1。
func (h *NodeListHandler) fillPodCounts(all []nodeOut) {
	if len(all) == 0 {
		return
	}
	type key struct {
		cid  int
		name string
	}
	counts := map[key]int64{}
	if rows, err := h.DB.Query(
		`SELECT cluster_id, node_name, COUNT(*) FROM k8s_pods GROUP BY cluster_id, node_name`); err == nil {
		for rows.Next() {
			var cid int
			var name string
			var n int64
			if rows.Scan(&cid, &name, &n) == nil {
				counts[key{cid, name}] = n
			}
		}
		rows.Close()
	}
	// ⚠️ 「这个节点上没有 Pod」和「这个集群的 Pod 压根没采过」不能用
	// 同一条查询区分：两种情况下这个节点都没有行。
	//
	// 判据要抬到**集群**这一层：集群里只要有任意一行 Pod，就说明采集跑过了，
	// 那么这个节点没有行就是真的没有（刚扩容、被 drain 了）；
	// 整个集群一行都没有，才是"没采过"。
	//
	// 不这么分的话，主机详情抽屉说"节点上没有 Pod"、节点列表说"未接入"，
	// 同一份数据两个答案 —— 而看的人不知道该信哪个。
	collectedClusters := map[int]bool{}
	for k := range counts {
		collectedClusters[k.cid] = true
	}
	for i := range all {
		if n, ok := counts[key{all[i].ClusterID, all[i].Name}]; ok {
			v := n
			all[i].PodsCollected = &v
			continue
		}
		if collectedClusters[all[i].ClusterID] {
			zero := int64(0)
			all[i].PodsCollected = &zero
		}
		// 集群一行都没有 → 保持 nil（没采过）
	}
}

// nodeStatusKey 状态筛选用的归类键。
//
// ⚠️ `stale` 独立成一档，不并进 notready：心跳过期时我们**不知道**它是什么状态，
// 而库里那个 Ready 是过期数据。把它算成 ready 是在替一个联系不上的节点担保。
func nodeStatusKey(n nodeOut) string {
	if n.HeartbeatStale {
		return "stale"
	}
	// 🔴 压力位必须参与状态判定，而且优先于 Ready。
	//
	//	磁盘 100%、kubelet 反复 ImageGC 失败、已经在驱逐 Pod 的节点，
	//	ready_status 依然是 Ready —— 于是节点列表把它显示成「正常」，
	//	按状态筛也永远筛不出来（OPSCMDB-042）。
	//
	//	实测 DEV node12 正是这个状态，而它同时把 Jenkins 构建拖到 12 分钟拉不下镜像。
	//	「快撑不住但还没倒」是节点最危险的一段，恰恰是这一段看不见。
	//
	// ⚠️ conditions 列存的是采集时算好的压力位摘要（只认 MemoryPressure/
	//	DiskPressure/PIDPressure/NetworkUnavailable 为真压力），非空即有压力。
	if strings.TrimSpace(n.Conditions) != "" {
		return "pressure"
	}
	if n.ReadyStatus == "Ready" {
		return "ready"
	}
	return "notready"
}

// nodePage 纯函数：筛选 + 分面 + 排序 + 分页。
//
// ⚠️ 同 clusterPage：节点是百量级，内存处理够用。
// 到万级（大集群全量节点）时必须改成 SQL 分页，别照抄。
func nodePage(all []nodeOut, q httpx.PageQuery) ([]nodeOut, int64, map[string]map[string]int64) {
	facets := map[string]map[string]int64{"cluster": {}, "status": {}, "pool": {}}
	cluster, status := q.Filters["cluster"], q.Filters["status"]
	pool := q.Filters["pool"]

	// 分面按「不含该维度自身的筛选」统计，否则切过去的条数全是 0。
	// 三个维度各自要排除自己，所以这里写成显式的三段而不是循环——
	// 循环写法看着短，但"排除自己"这件事会藏进索引比较里，改的时候极易漏掉一个
	hitOther := func(n nodeOut, skip string) bool {
		if skip != "cluster" && cluster != "" && cluster != "all" && n.ClusterName != cluster {
			return false
		}
		if skip != "status" && status != "" && status != "all" && nodeStatusKey(n) != status {
			return false
		}
		if skip != "pool" && pool != "" && pool != "all" && n.Pool != pool {
			return false
		}
		return true
	}
	for _, n := range all {
		if q.Keyword != "" && !matchNode(n, q.Keyword) {
			continue
		}
		if hitOther(n, "status") {
			facets["status"][nodeStatusKey(n)]++
			facets["status"]["all"]++
		}
		if hitOther(n, "cluster") {
			facets["cluster"][n.ClusterName]++
			facets["cluster"]["all"]++
		}
		if hitOther(n, "pool") {
			// 节点池可能是空串（自建集群没有节点池概念）。
			// 归到 "-" 而不是丢掉：丢掉的话分面数字加起来对不上总数，
			// 用户会以为筛选漏了节点
			k := n.Pool
			if k == "" {
				k = "-"
			}
			facets["pool"][k]++
			facets["pool"]["all"]++
		}
	}

	filtered := make([]nodeOut, 0, len(all))
	for _, n := range all {
		if q.Keyword != "" && !matchNode(n, q.Keyword) {
			continue
		}
		if cluster != "" && cluster != "all" && n.ClusterName != cluster {
			continue
		}
		if status != "" && status != "all" && nodeStatusKey(n) != status {
			continue
		}
		if pool != "" && pool != "all" {
			k := n.Pool
			if k == "" {
				k = "-"
			}
			if k != pool {
				continue
			}
		}
		filtered = append(filtered, n)
	}

	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		var less bool
		switch q.SortBy {
		case "cluster":
			less = a.ClusterName < b.ClusterName
		case "status":
			// 排序按**严重度**，不按字母：人是来找"哪个不对劲"的
			less = statusSeverity(a) < statusSeverity(b)
		case "pods":
			less = derefI64(a.PodsCollected) < derefI64(b.PodsCollected)
		case "heartbeat":
			less = a.LastHeartbeat < b.LastHeartbeat
		default:
			less = a.Name < b.Name
		}
		if q.SortDesc {
			return !less
		}
		return less
	})

	total := int64(len(filtered))
	lo := min(q.Offset(), len(filtered))
	hi := min(lo+q.Size, len(filtered))
	return filtered[lo:hi], total, facets
}

// statusSeverity 越小越该被先看到。
//
//	0 心跳过期  我们对它一无所知，最该查
//	1 NotReady  明确不健康
//	2 Ready
func statusSeverity(n nodeOut) int {
	switch nodeStatusKey(n) {
	case "stale":
		return 0
	case "notready":
		return 1
	default:
		return 2
	}
}

func derefI64(p *int64) int64 {
	if p == nil {
		return -1
	}
	return *p
}

func matchNode(n nodeOut, kw string) bool {
	kw = strings.ToLower(kw)
	return strings.Contains(strings.ToLower(n.Name), kw) ||
		strings.Contains(strings.ToLower(n.InternalIP), kw) ||
		strings.Contains(strings.ToLower(n.Pool), kw)
}

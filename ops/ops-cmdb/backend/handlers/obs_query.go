package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"ops-cmdb-backend/internal/httpx"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/store"

	"ops-cmdb-backend/crypto"
)

// rawJSON 把外部返回的 JSON 文本解析成对象嵌入响应（解析失败则原样返回字符串）。
func rawJSON(s string) any {
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return v
	}
	return s
}

// ObsQueryHandler 查外部数据源：资源使用率(Prometheus/VM)、Loki 日志、KubeSphere 流水线。
// 本地不存历史，实时打这些源；地址来自 obs_endpoints（按 env/cluster 解析）。
type ObsQueryHandler struct {
	Store  *store.Store
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewObsQueryHandler(st *store.Store, db *sql.DB, cipher *crypto.Cipher) *ObsQueryHandler {
	return &ObsQueryHandler{Store: st, DB: db, Cipher: cipher}
}

func (h *ObsQueryHandler) RegisterInsights(r *gin.RouterGroup) {
	r.GET("/k8s/resource-waste", h.ResourceWaste) // request vs 实测用量 + 推荐值
	r.GET("/k8s/idle-cost", h.IdleCost)           // 实付/已分摊/闲置三段拆分
}

func (h *ObsQueryHandler) Register(r *gin.RouterGroup) {
	r.GET("/obs/usage", h.Usage)           // 资源使用率(Prometheus): cluster_id,env?,target,namespace?,name,metric,minutes,query?
	r.GET("/obs/loki", h.Loki)             // Loki 日志: env?/cluster_id?,query(LogQL),minutes
	r.GET("/obs/kubesphere", h.KubeSphere) // KubeSphere 透传: env?/cluster_id?,path(kapis 路径)
	r.GET("/k8s/pod-usage", h.PodUsage)    // 全 Pod 实时用量(cpu_m/mem_mi) map，供 Pod 页列展示
	r.GET("/k8s/node-usage", h.NodeUsage)  // 全节点实时用量(cpu%/mem%) map，供节点页列展示
	r.GET("/k8s/pvc-usage", h.PVCUsage)    // 全 PVC 使用率(used/cap/pct) map，供存储页列展示
	r.GET("/obs/host-usage", h.HostUsage)  // 云主机(非K8s)用量排行: env?/project?/team?
}

// HostUsage 列云主机实时用量，按内存降序（先看谁快撑爆）。
//
// 为什么单独一个接口：主机不在任何 K8s 集群里，node-usage 那套按 node 标签的口径覆盖不到它们。
// 通用数据源里主机全在 cluster="ecs" 下，靠 env/project/team 三个标签区分归属——
// 这也是"某个项目下哪台机器内存快满了"这类问题唯一能一次问出来的地方。
//
// 只看主机不看 K8s 节点：K8s 节点的 node-exporter 指标没有 env/project/team 标签，
// 用 env!="" 就能把它们排除干净，不必依赖 cluster="ecs"（老数据源没有 cluster 标签）。
func (h *ObsQueryHandler) HostUsage(c *gin.Context) {
	base, token, _, ok := h.prom(c)
	if !ok {
		return
	}
	sel := hostSelector(c.Query("env"), c.Query("project"), c.Query("team"))
	if sel == "" {
		sel = `env!=""` // 没给筛选条件：取全部主机，同时排除掉 K8s 节点
	}
	by := "by(instance,env,project,team)"
	rows := map[string]map[string]any{}
	get := func(m map[string]string) map[string]any {
		ip := m["instance"]
		if i := strings.IndexByte(ip, ':'); i > 0 {
			ip = ip[:i]
		}
		if rows[ip] == nil {
			rows[ip] = map[string]any{"ip": ip, "env": m["env"], "project": m["project"], "team": m["team"]}
		}
		return rows[ip]
	}
	lbl := promLabels("", sel)
	cpuLbl := promLabels("", sel, `mode="idle"`)
	if cpu, err := promInstant(base, token,
		`(1 - avg `+by+`(rate(node_cpu_seconds_total`+cpuLbl+`[5m]))) * 100`); err == nil {
		for _, s := range cpu {
			get(s.Metric)["cpu_pct"] = round2(s.Value)
		}
	}
	if mem, err := promInstant(base, token,
		`(1 - sum `+by+`(node_memory_MemAvailable_bytes`+lbl+`) / sum `+by+`(node_memory_MemTotal_bytes`+lbl+`)) * 100`); err == nil {
		for _, s := range mem {
			get(s.Metric)["mem_pct"] = round2(s.Value)
		}
	}
	if len(rows) == 0 {
		c.JSON(http.StatusOK, gin.H{"ok": false, "items": []any{},
			"error": "没查到任何主机指标。可能是：数据源里主机指标没有 env/project/team 标签，" +
				"或筛选条件(env/project/team)写错了——标签值通常是小写，具体取值可在数据源里用 prom_labels 查"})
		return
	}
	// 补 CMDB 台账里的主机名/规格，让"哪台机器"直接可读，不用再拿 IP 去查一遍。
	out := make([]map[string]any, 0, len(rows))
	for ip, r := range rows {
		var name string
		var vcpu, memMB int
		if h.DB.QueryRow(`SELECT name, COALESCE(vcpu,0), COALESCE(mem_mb,0) FROM hosts WHERE internal_ip=? LIMIT 1`, ip).
			Scan(&name, &vcpu, &memMB) == nil {
			r["host_name"], r["vcpu"], r["mem_gb"] = name, vcpu, memMB/1024
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return num(out[i]["mem_pct"]) > num(out[j]["mem_pct"]) })
	c.JSON(http.StatusOK, gin.H{"ok": true, "count": len(out), "items": out})
}

func num(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return -1 // 没采到用量的排最后，别把它们顶到"最危险"的位置
}

// PVCUsage 返回 {"ns/pvc": {used_gi, cap_gi, pct}}（kubelet volume 指标）。
func (h *ObsQueryHandler) PVCUsage(c *gin.Context) {
	base, token, label, value, ok := h.promParts(c)
	if !ok {
		return
	}
	sel := ""
	if label != "" {
		sel = fmt.Sprintf("%s=%q", label, value)
	}
	usage := map[string]map[string]float64{}
	get := func(k string) map[string]float64 {
		if usage[k] == nil {
			usage[k] = map[string]float64{}
		}
		return usage[k]
	}
	lbl := promLabels(sel)
	if used, err := promInstant(base, token, `kubelet_volume_stats_used_bytes`+lbl); err == nil {
		for _, s := range used {
			get(s.Metric["namespace"] + "/" + s.Metric["persistentvolumeclaim"])["used_gi"] = s.Value / 1073741824
		}
	}
	if cap, err := promInstant(base, token, `kubelet_volume_stats_capacity_bytes`+lbl); err == nil {
		for _, s := range cap {
			m := get(s.Metric["namespace"] + "/" + s.Metric["persistentvolumeclaim"])
			m["cap_gi"] = s.Value / 1073741824
			if m["cap_gi"] > 0 {
				m["pct"] = m["used_gi"] / m["cap_gi"] * 100
			}
		}
	}
	shared := h.sharedFSPVCs(c.Query("cluster_id"))
	out := gin.H{"ok": true, "usage": usage, "accuracy": h.pvcAccuracy(usage, shared)}
	// 一个卷都没查到时，先排除「集群标签值配错」——否则返回的空 map 会被当成「该集群没有 PVC」。
	if len(usage) == 0 && label != "" {
		if bad := verifyClusterValue(base, token, label, value); bad != nil {
			out["ok"] = false
			out["cluster_label_error"] = bad
		} else {
			out["empty_hint"] = "该集群没查到任何 PVC 用量。集群标签值是对的，" +
				"可能是数据源未采集 kubelet_volume_stats_* 指标，或该集群确实没有 PVC"
		}
	}
	c.JSON(http.StatusOK, out)
}

// sharedFSPVCs 找出「与宿主机共用文件系统」的 PVC（key 为 ns/name）。
//
// k3s 的 local-path、hostPath 这类卷本质就是宿主机上的一个目录，没有独立块设备或配额。
// kubelet 对它们上报的 kubelet_volume_stats_* 是**整个宿主机文件系统**的容量和用量，
// 于是同一节点上所有 PVC 报出来的数完全一样——DEV 集群 53 个 PVC 全是 4030.64Gi / 61.53%。
//
// 按 storageClass 判定，而不是按「数值相同」猜：数值相同也可能是真巧合，
// 而 storageClass 是这类卷的定义性特征。
func (h *ObsQueryHandler) sharedFSPVCs(clusterID string) map[string]string {
	out := map[string]string{}
	if clusterID == "" {
		return out
	}
	rows, err := h.DB.Query(`SELECT namespace, name, COALESCE(storage_class,'') FROM k8s_pvcs WHERE cluster_id=?`, clusterID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var ns, name, sc string
		if rows.Scan(&ns, &name, &sc) != nil {
			continue
		}
		if isSharedFSStorageClass(sc) {
			out[ns+"/"+name] = sc
		}
	}
	return out
}

// isSharedFSStorageClass 这些 StorageClass 分配出来的卷与宿主机共用文件系统。
var sharedFSClasses = []string{"local-path", "hostpath", "local-storage", "manual", "nfs"}

func isSharedFSStorageClass(sc string) bool {
	sc = strings.ToLower(strings.TrimSpace(sc))
	if sc == "" {
		return false
	}
	for _, c := range sharedFSClasses {
		if strings.Contains(sc, c) {
			return true
		}
	}
	return false
}

// pvcAccuracy 为每个 PVC 标注这个数字有多可信。
//
// 不直接把失真的数据藏起来：宿主机水位本身是有用的（正好对应节点磁盘吃紧的问题），
// 藏了反而少一个信息源。但必须让调用方知道「这不是本卷的用量」，
// 否则会据此判断某个 PVC 快满了——而实际上它可能几乎是空的。
func (h *ObsQueryHandler) pvcAccuracy(usage map[string]map[string]float64, shared map[string]string) map[string]any {
	out := map[string]any{}
	for k := range usage {
		if sc, ok := shared[k]; ok {
			out[k] = map[string]string{
				"level": "node-fs",
				"note": "该卷由 " + sc + " 分配，与宿主机共用文件系统，此处显示的是「宿主机整体水位」，" +
					"不代表本卷实际用量；同节点上的卷会看到相同数值",
			}
		}
	}
	return out
}

type promSample struct {
	Metric map[string]string
	Value  float64
}

// promInstant 打一次 /api/v1/query，返回样本列表。
func promInstant(base, token, query string) ([]promSample, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s", base, url.QueryEscape(query))
	code, body, err := obsGet(u, token, 15*time.Second)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("prometheus HTTP %d", code)
	}
	var r struct {
		Data struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  []any             `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return nil, err
	}
	out := make([]promSample, 0, len(r.Data.Result))
	for _, res := range r.Data.Result {
		val := 0.0
		if len(res.Value) == 2 {
			if s, ok := res.Value[1].(string); ok {
				val, _ = strconv.ParseFloat(s, 64)
			}
		}
		out = append(out, promSample{Metric: res.Metric, Value: val})
	}
	return out, nil
}

// prom 解析 Prometheus 端点，并返回把查询限定到本集群的标签选择器。
// selector 为空表示该源只有一个集群的数据，无需隔离。
func (h *ObsQueryHandler) prom(c *gin.Context) (base, token, selector string, ok bool) {
	base, token, label, value, ok := h.promParts(c)
	if !ok {
		return "", "", "", false
	}
	if label == "" {
		return base, token, "", true
	}
	return base, token, fmt.Sprintf("%s=%q", label, value), true
}

// promParts 同 prom，但把隔离标签拆成名/值返回，供「隔离查询返回空」时的自检使用
// （标签值配错和组件真没数据，返回的都是空，必须能分开）。
func (h *ObsQueryHandler) promParts(c *gin.Context) (base, token, label, value string, ok bool) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	env := c.Query("env")
	if env == "" && cid > 0 {
		env = h.clusterEnv(cid)
	}
	base, token, clusterLabel, err := resolveEndpointFull(h.DB, h.Cipher, "prometheus", env, cid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return "", "", "", "", false
	}
	label, value = clusterSelectorParts(h.DB, clusterLabel, cid)
	return base, token, label, value, true
}

// PodUsage 返回 {"ns/pod": {cpu_m, mem_mi}}，一次拉全集群 Pod 实时用量。
func (h *ObsQueryHandler) PodUsage(c *gin.Context) {
	base, token, sel, ok := h.prom(c)
	if !ok {
		return
	}
	usage := map[string]map[string]float64{}
	get := func(k string) map[string]float64 {
		if usage[k] == nil {
			usage[k] = map[string]float64{}
		}
		return usage[k]
	}
	lbl := promLabels(sel, `container!=""`)
	// PROD-014：同一容器有 3 条重复 series，不去重会虚高 3 倍
	warnIfDuplicateSeries(base, token, lbl)
	if cpu, err := promInstant(base, token, dedupContainerSum("namespace,pod", `rate(container_cpu_usage_seconds_total`+lbl+`[5m])`)); err == nil {
		for _, s := range cpu {
			get(s.Metric["namespace"] + "/" + s.Metric["pod"])["cpu_m"] = s.Value * 1000
		}
	}
	if mem, err := promInstant(base, token, dedupContainerSum("namespace,pod", `container_memory_working_set_bytes`+lbl)); err == nil {
		for _, s := range mem {
			get(s.Metric["namespace"] + "/" + s.Metric["pod"])["mem_mi"] = s.Value / 1048576
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "usage": usage})
}

// NodeUsage 返回 {"node": {cpu_pct, mem_pct}}（来自 node-exporter）。
func (h *ObsQueryHandler) NodeUsage(c *gin.Context) {
	base, token, sel, ok := h.prom(c)
	if !ok {
		return
	}
	usage := map[string]map[string]float64{}
	get := func(k string) map[string]float64 {
		if usage[k] == nil {
			usage[k] = map[string]float64{}
		}
		return usage[k]
	}
	// 兼容两种标签:标准 Prometheus 用 node;KubeSphere/whizard 把 instance relabel 成节点名(无 node 标签)。
	// 用 by(node,instance) 同时保留两者,取值时 node 优先、回落 instance。
	// ⚠️ 必须和磁盘那段（nodeFsUsage → promNodeName）用**同一个**取键函数：
	//	两边回落顺序不同的话，同一台机器会被拆成两行 —— 一行只有 CPU/内存、
	//	一行只有磁盘，界面上看着像"采集缺了一半"，其实只是键对不上。
	nodeKey := promNodeName
	cpuLbl, memLbl := promLabels(sel, `mode="idle"`), promLabels(sel)
	if cpu, err := promInstant(base, token, `(1 - avg by(node,instance)(rate(node_cpu_seconds_total`+cpuLbl+`[5m]))) * 100`); err == nil {
		for _, s := range cpu {
			if k := nodeKey(s.Metric); k != "" {
				get(k)["cpu_pct"] = s.Value
			}
		}
	}
	if mem, err := promInstant(base, token, `(1 - sum by(node,instance)(node_memory_MemAvailable_bytes`+memLbl+`) / sum by(node,instance)(node_memory_MemTotal_bytes`+memLbl+`)) * 100`); err == nil {
		for _, s := range mem {
			if k := nodeKey(s.Metric); k != "" {
				get(k)["mem_pct"] = s.Value
			}
		}
	}
	// 磁盘水位。此前完全没采，导致「镜像 GC 回收不出空间、磁盘吃紧」这类问题
	// 只能等它触发事件后从侧面撞见，无法提前预警——而磁盘满会直接让发布失败。
	//
	// 口径统一在 nodefs.go。⚠️ 这里以前写的是 `mountpoint="/"` + `sum by(node,instance)`，
	// 两处都错：`/` 在 GKE COS 上是只读启动镜像；而换成挂载点白名单后 sum 会把
	// 同一块盘按挂载点数量重复累加（980 GB → 11 TB）。所以聚合在 Go 里按挂载点取最满的那个。
	if fs, err := nodeFsUsage(base, token, sel); err == nil {
		for k, v := range fs {
			get(k)["disk_pct"] = v.Pct
			// 光给百分比不够用：「88%」是 98GB 的 88% 还是 1TB 的 88%，处置动作完全不同
			// （前者立刻要清，后者还能扛一阵）。总量和已用量一并给出（CMDB-011）。
			// ⚠️ 三个数必须取自**同一个**挂载点，否则会拼出一组自相矛盾的数字。
			get(k)["disk_total_gb"] = v.TotalBytes / 1024 / 1024 / 1024
			get(k)["disk_used_gb"] = v.UsedBytes / 1024 / 1024 / 1024
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "usage": usage})
}

// clusterEnv 由 cluster_id 取环境（用于按环境解析数据源）。
func (h *ObsQueryHandler) clusterEnv(cid int) string { return clusterEnvOf(h.DB, cid) }

// clusterEnvOf 同上，但不绑定 handler —— 新鲜度检查等其它 handler 也要用。
// ⚠️ 解析数据源必须用同一个 env 来源，各写一份迟早分叉。
func clusterEnvOf(db *sql.DB, cid int) string {
	var env string
	_ = db.QueryRow(`SELECT environment FROM k8s_clusters WHERE id=?`, cid).Scan(&env)
	return env
}

// Usage 资源使用率：构造 PromQL → query_range → 返回 Prometheus 原始结果（AI/前端自解析）。
func (h *ObsQueryHandler) Usage(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	env := c.Query("env")
	if env == "" && cid > 0 {
		env = h.clusterEnv(cid)
	}
	base, token, clusterLabel, err := resolveEndpointFull(h.DB, h.Cipher, "prometheus", env, cid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	sel := clusterSelector(h.DB, clusterLabel, cid)
	promql := c.Query("query")
	rawQuery := promql != ""
	if !rawQuery {
		promql = buildPromQL(c.Query("target"), c.Query("namespace"), c.Query("name"), c.Query("metric"),
			sel, hostSelector(c.Query("host_env"), c.Query("host_project"), c.Query("host_team")))
	}
	if promql == "" {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.obsQueryMissingTarget", nil, nil)
		return
	}
	// 自带 PromQL 的调用方要自己写集群条件——我们不解析、更不改写别人的表达式。
	// 但共享源上不加条件就会跨集群串数据，这里必须显式告知，不能让调用方以为拿到的是本集群的数据。
	var hint string
	if rawQuery && sel != "" {
		hint = fmt.Sprintf("该数据源同时采集多个集群，自定义 query 未自动加集群条件；"+
			"如需只看本集群请在每个指标上加 {%s}", sel)
	}
	minutes := int64(60)
	if m, e := strconv.ParseInt(c.Query("minutes"), 10, 64); e == nil && m > 0 && m <= 43200 {
		minutes = m
	}
	end := time.Now()
	start := end.Add(-time.Duration(minutes) * time.Minute)
	// 自定义起止时间(unix 秒)优先于 minutes
	if s, e1 := strconv.ParseInt(c.Query("start"), 10, 64); e1 == nil && s > 0 {
		if en, e2 := strconv.ParseInt(c.Query("end"), 10, 64); e2 == nil && en > s {
			start = time.Unix(s, 0)
			end = time.Unix(en, 0)
			minutes = (en - s) / 60
		}
	}
	step := minutes / 60
	if step < 1 {
		step = 1
	}
	u := fmt.Sprintf("%s/api/v1/query_range?query=%s&start=%d&end=%d&step=%dm",
		base, url.QueryEscape(promql), start.Unix(), end.Unix(), step)
	code, body, err := obsGet(u, token, 20*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	out := gin.H{"ok": code == 200, "status": code, "query": promql, "data": rawJSON(body)}
	if hint != "" {
		out["hint"] = hint
	}
	// ⚠️ 把 Prometheus 信封**解开**成 series，而不是让调用方自己剥。
	//
	//	# 为什么必须由后端解
	//
	//	原来这里只有 `data`（Prometheus 原始响应）。于是真实路径是
	//	`data.data.result`（两层信封：我们一层 + Prometheus 一层），
	//	而前端类型声明的是 `series` —— 取到 undefined，判成"无数据"，
	//	页面显示「这个时间范围内没有数据点。**可能是对象名写错了**」。
	//
	//	实际后端返回了 61 个点、seriesFetched=225、status=success：
	//	**查询完全成功，界面却在指责用户输错了名字**（OPSCMDB-031 P0-14）。
	//	这一条同时踩了两条红线：失败被渲染成空态，以及错误提示反过来怪用户。
	//
	//	让前端去剥 Prometheus 的 resultType/matrix/values 也不对：
	//	前端不该懂上游的报文格式，那是数据源的实现细节。
	//	`data` 保留原样不动 —— MCP 的 resource_usage 走同一个接口，
	//	AI 侧要看 stats/seriesFetched 这些诊断字段。
	series, pts := usageSeriesFrom(body)
	out["series"] = series
	out["unit"] = promUnit(c.Query("metric"))
	out["promql"] = promql
	// 三态要能分开：查失败 / 查成功但真的没有序列 / 有数据。
	// 「没有序列」的原因不能瞎猜成"名字写错了"——也可能这个对象在窗口内不存在、
	// 或者指标本身没被采集。把可能性都列出来，别只挑一个说得像是结论。
	if code == 200 && pts == 0 {
		out["empty"] = true
		out["empty_hint"] = "查询本身成功了，但这个时间范围内没有序列。可能是：对象在这段时间内不存在、" +
			"该指标未被采集、或名字与指标里的标签值不一致（用「查询」页把 PromQL 跑一遍最快确认）"
	}
	c.JSON(http.StatusOK, out)
}

// usageSeriesFrom 从 Prometheus query_range 的响应里提出**图表能直接吃的**时序。
//
//	⚠️ 和 prom_query.go 的 `promSeries` 类型刻意不同形状，不要合并：
//	  · /api/prom/query 给查询页和 AI 用，保留 Prometheus 原生的
//	    `{metric, values:[[ts,"val"]]}` —— 那边的人要看原始报文。
//	  · 这里给图表用，出 `{name, points:[{t,v}]}` —— 值已转成数字。
//	把两者硬合成一个形状，必然有一方要在自己那边再转一次。
//
//	返回 (series, 总点数)。总点数用来判"真的空"——
//	序列数不为 0 但每条都没有点，也是空，只看 len(series) 会漏。
//
//	⚠️ 值在 Prometheus 里是**字符串**（["1786940040","3.8046"]，时间戳是数字、值是字符串）。
//	解析失败的点直接丢掉而不是当 0：一个解析不出来的值被画成 0，
//	曲线上就多一个"这一刻负载归零"的假谷底。
func usageSeriesFrom(body string) ([]gin.H, int) {
	var env struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string `json:"resultType"`
			Result     []struct {
				Metric map[string]string   `json:"metric"`
				Values [][]json.RawMessage `json:"values"`
				Value  []json.RawMessage   `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(body), &env) != nil {
		return []gin.H{}, 0
	}
	out := make([]gin.H, 0, len(env.Data.Result))
	total := 0
	for _, r := range env.Data.Result {
		vals := r.Values
		if len(vals) == 0 && len(r.Value) > 0 {
			// resultType=vector（瞬时值）只有单个 value，包成一个点
			vals = [][]json.RawMessage{r.Value}
		}
		points := make([]gin.H, 0, len(vals))
		for _, pair := range vals {
			if len(pair) < 2 {
				continue
			}
			var ts float64
			var raw string
			if json.Unmarshal(pair[0], &ts) != nil || json.Unmarshal(pair[1], &raw) != nil {
				continue
			}
			v, err := strconv.ParseFloat(raw, 64)
			if err != nil {
				continue // 丢掉，别当 0
			}
			points = append(points, gin.H{"t": int64(ts), "v": v})
		}
		total += len(points)
		out = append(out, gin.H{"name": usageSeriesName(r.Metric), "points": points})
	}
	return out, total
}

// usageSeriesName 给一条序列取个能认出来的名字。
//
//	实测生成的 PromQL 大多带 sum()，聚合后 metric 是空的 {} ——
//	这时候返回空串，由前端决定显示什么（通常就是用户填的对象名），
//	而不是在这里编一个 "unknown"。
func usageSeriesName(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	for _, k := range []string{"pod", "node", "instance", "container", "workload", "__name__"} {
		if v := m[k]; v != "" {
			return v
		}
	}
	// 都没有就把标签拼出来，总比空着强
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ",")
}

// promUnit 指标单位。cpu 是核数，mem 是字节 —— 图上不写单位等于没法读数。
func promUnit(metric string) string {
	switch strings.ToLower(strings.TrimSpace(metric)) {
	case "mem", "memory":
		return "bytes"
	case "cpu":
		return "cores"
	default:
		return ""
	}
}

// hostSelector 把环境/项目/团队拼成云主机指标的过滤条件。
//
// 主机不属于任何 K8s 集群（通用源里它们在 cluster="ecs" 下），所以不能套用集群选择器，
// 靠这三个标签区分：env / project / team，具体取值由各家自己的采集配置决定。
// env 统一转小写——CMDB 里环境是 UAT/PROD 大写枚举，指标标签是小写。
func hostSelector(env, project, team string) string {
	conds := []string{}
	if env = strings.TrimSpace(env); env != "" {
		conds = append(conds, fmt.Sprintf("env=%q", strings.ToLower(env)))
	}
	if project = strings.TrimSpace(project); project != "" {
		conds = append(conds, fmt.Sprintf("project=%q", project))
	}
	if team = strings.TrimSpace(team); team != "" {
		conds = append(conds, fmt.Sprintf("team=%q", team))
	}
	return strings.Join(conds, ",")
}

// buildPromQL 按目标类型构造 PromQL。
//
// selector 是集群隔离条件，只加在 K8s 对象(pod/workload/node)上；主机(host)在通用源里
// 属于 cluster="ecs"，套集群条件会一条都查不到，所以它走 hostSel(env/project/team) 那套。
func buildPromQL(target, ns, name, metric, selector, hostSel string) string {
	if name == "" {
		return ""
	}
	// ⚠️ 内存指标的写法必须兼容 mem / memory 两种。
	//
	//	原来只认 `mem`，而前端下拉传的是 `memory` —— 于是选「内存」查出来的
	//	是**CPU 曲线**。这比查不出来危险得多：图正常画出来了，
	//	轴上也有数，只是画的不是你选的那个指标，没有任何迹象提示这一点。
	//	（这个 bug 一直被 P0-14「整页显示无数据」掩护着，所以没人验到过。）
	//	MCP 侧的调用方（AI）同样两种写法都可能写，所以在入口统一，
	//	而不是在每个分支里各判一次。
	if metric == "memory" {
		metric = "mem"
	}
	switch target {
	case "pod":
		lbl := promLabels(selector, fmt.Sprintf("namespace=%q,pod=%q", ns, name), `container!=""`)
		if metric == "mem" {
			return dedupContainerSum("", `container_memory_working_set_bytes`+lbl)
		}
		return dedupContainerSum("", `rate(container_cpu_usage_seconds_total`+lbl+`[5m])`)
	case "workload":
		// 按 Pod 分组 → 图上一个服务的每个 Pod 各一条线
		lbl := promLabels(selector, fmt.Sprintf("namespace=%q,pod=~%q", ns, name+"-.*"), `container!=""`)
		if metric == "mem" {
			return dedupContainerSum("pod", `container_memory_working_set_bytes`+lbl)
		}
		return dedupContainerSum("pod", `rate(container_cpu_usage_seconds_total`+lbl+`[5m])`)
	case "node":
		// 节点：用 node-exporter 绝对用量(核/字节)，与 Pod 单位一致；按 node 标签匹配。
		if metric == "mem" {
			lbl := promLabels(selector, fmt.Sprintf("node=%q", name))
			return `sum(node_memory_MemTotal_bytes` + lbl + `) - sum(node_memory_MemAvailable_bytes` + lbl + `)`
		}
		return `sum(rate(node_cpu_seconds_total` + promLabels(selector, `mode!="idle"`, fmt.Sprintf("node=%q", name)) + `[5m]))`
	case "host":
		// 传统主机：node-exporter，按 instance 匹配(通常 <ip>:9100)，传入主机内网IP。
		if metric == "mem" {
			lbl := promLabels("", hostSel, fmt.Sprintf("instance=~%q", name+".*"))
			return `sum(node_memory_MemTotal_bytes` + lbl + `) - sum(node_memory_MemAvailable_bytes` + lbl + `)`
		}
		return `sum(rate(node_cpu_seconds_total` + promLabels("", hostSel, `mode!="idle"`, fmt.Sprintf("instance=~%q", name+".*")) + `[5m]))`
	}
	return ""
}

// Loki 日志检索（LogQL），透传 range 查询结果。
func (h *ObsQueryHandler) Loki(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	env := c.Query("env")
	if env == "" && cid > 0 {
		env = h.clusterEnv(cid)
	}
	base, token, err := resolveEndpoint(h.DB, h.Cipher, "loki", env, cid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	q := c.Query("query")
	if q == "" {
		httpx.Required(c, "query")
		return
	}
	minutes := int64(60)
	if m, e := strconv.ParseInt(c.Query("minutes"), 10, 64); e == nil && m > 0 {
		minutes = m
	}
	end := time.Now()
	start := end.Add(-time.Duration(minutes) * time.Minute)
	// step 必须显式给：不给的话 Loki 对聚合查询（sum/count_over_time 等）按默认步长采样，
	// 查 30 分钟能返回几百个点 × 每条序列，而且相邻点值几乎一样——对判断趋势毫无帮助，
	// 纯粹撑爆调用方的上下文。复用 PromQL 那边同一套步长策略，控制在 ~200 个点。
	step := c.Query("step")
	if step == "" {
		step = autoStep(int(minutes))
	}
	u := fmt.Sprintf("%s/loki/api/v1/query_range?query=%s&start=%d&end=%d&limit=500&step=%s",
		base, url.QueryEscape(q), start.UnixNano(), end.UnixNano(), url.QueryEscape(step))
	// ⚠️ 超时给足但必须有：宽匹配的聚合查询（如 {namespace=~".+"}）会让 Loki
	// 扫全量 chunk，实测直接把连接拖到断开——调用方看到的是"socket closed"，
	// 既不知道是超时还是服务挂了，也不知道该怎么改查询。
	code, body, err := obsGet(u, token, 45*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error(),
			"hint": "查询失败。若是超时：LogQL 的标签匹配太宽（如 namespace=~\".+\"）会让 Loki " +
				"扫描全部数据流，请收窄标签或缩短时间窗（minutes）后重试。" +
				"⚠️ 这是查询失败，不是「没有日志」"})
		return
	}
	out := gin.H{"ok": code == 200, "status": code, "step": step,
		"data": rawJSON(stripLokiStats(body))}
	// 🔴 空结果要说清可能的原因。原样透传 Loki 的 {"result":[]} 时，
	// 调用方分不清「标签写错了」「时间窗太短」和「确实没有日志」——
	// 而这三者的下一步动作完全不同
	if code == 200 && lokiResultEmpty(body) {
		out["empty_hint"] = "这次查询没有匹配到任何日志。三种可能，处置不同：" +
			"(1) 标签名/值不对——Loki 的标签体系和 K8s 不一定一致，" +
			"先用一个只带标签、不带过滤的查询（如 {namespace=\"xxx\"}）确认标签存在；" +
			"(2) 时间窗太短——当前 " + strconv.FormatInt(minutes, 10) + " 分钟，试着放大；" +
			"(3) 确实没有这条日志。⚠️ 在排除前两条之前，不要判定为「服务没打日志」"
	}
	c.JSON(http.StatusOK, out)
}

// lokiResultEmpty 判断 Loki 返回里 result 是不是空。
// 解析失败时返回 false —— 拿不准就不加提示，别给一个基于误判的结论。
func lokiResultEmpty(body string) bool {
	var r struct {
		Data struct {
			Result []json.RawMessage `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(body), &r) != nil {
		return false
	}
	return len(r.Data.Result) == 0
}

// KubeSphere 透传：拉指定 kapis 路径（如流水线运行状态/日志），交给 AI 诊断。
func (h *ObsQueryHandler) KubeSphere(c *gin.Context) {
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	env := c.Query("env")
	if env == "" && cid > 0 {
		env = h.clusterEnv(cid)
	}
	base, token, err := resolveEndpoint(h.DB, h.Cipher, "kubesphere", env, cid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	path := c.Query("path")
	if path == "" {
		httpx.Required(c, "path")
		return
	}
	code, body, err := obsGet(base+path, token, 20*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": code >= 200 && code < 300, "status": code, "data": rawJSON(body)})
}

// stripLokiStats 去掉 Loki 响应里的 stats 字段。
//
// 那一段是查询引擎的自我统计（下载了多少 chunk、解压多少字节…），对排障没有任何价值，
// 却常常比结果本身还长——实测一次事件查询里 stats 占了输出的一大半。
// 解析失败时原样返回：宁可多带一点，也不能因为裁剪逻辑出错就丢掉真正的数据。
func stripLokiStats(body string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		return body
	}
	data, ok := m["data"].(map[string]any)
	if !ok {
		return body
	}
	if _, has := data["stats"]; !has {
		return body
	}
	delete(data, "stats")
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return string(out)
}

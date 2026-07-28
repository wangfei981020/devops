package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/crypto"
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
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewObsQueryHandler(db *sql.DB, cipher *crypto.Cipher) *ObsQueryHandler {
	return &ObsQueryHandler{DB: db, Cipher: cipher}
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
// 这也是"UAT 的 g32 项目哪台机器内存快满了"这类问题唯一能一次问出来的地方。
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
				"或筛选条件(env/project/team)写错了——标签值是小写，如 env=uat、project=g32、team=dba"})
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
	c.JSON(http.StatusOK, gin.H{"ok": true, "usage": usage, "accuracy": h.pvcAccuracy(usage, shared)})
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
				"note": "该卷由 " + sc + " 分配，与宿主机共用文件系统，此处显示的是**宿主机整体水位**，" +
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
	cid, _ := strconv.Atoi(c.Query("cluster_id"))
	env := c.Query("env")
	if env == "" && cid > 0 {
		env = h.clusterEnv(cid)
	}
	base, token, clusterLabel, err := resolveEndpointFull(h.DB, h.Cipher, "prometheus", env, cid)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return "", "", "", false
	}
	return base, token, clusterSelector(h.DB, clusterLabel, cid), true
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
	if cpu, err := promInstant(base, token, `sum by(namespace,pod)(rate(container_cpu_usage_seconds_total`+lbl+`[5m]))`); err == nil {
		for _, s := range cpu {
			get(s.Metric["namespace"] + "/" + s.Metric["pod"])["cpu_m"] = s.Value * 1000
		}
	}
	if mem, err := promInstant(base, token, `sum by(namespace,pod)(container_memory_working_set_bytes`+lbl+`)`); err == nil {
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
	nodeKey := func(m map[string]string) string {
		if n := m["node"]; n != "" {
			return n
		}
		return m["instance"]
	}
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
	// 只看根分区(mountpoint="/")：容器镜像和日志都落在这里，也是真正会打爆的地方；
	// 排除各类虚拟文件系统，否则 overlay/tmpfs 会把结果搅乱。
	diskLbl := promLabels(sel, `mountpoint="/"`, `fstype!~"tmpfs|overlay|squashfs|iso9660"`)
	if disk, err := promInstant(base, token,
		`(1 - sum by(node,instance)(node_filesystem_avail_bytes`+diskLbl+`) / sum by(node,instance)(node_filesystem_size_bytes`+diskLbl+`)) * 100`); err == nil {
		for _, s := range disk {
			if k := nodeKey(s.Metric); k != "" {
				get(k)["disk_pct"] = s.Value
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "usage": usage})
}

// clusterEnv 由 cluster_id 取环境（用于按环境解析数据源）。
func (h *ObsQueryHandler) clusterEnv(cid int) string {
	var env string
	_ = h.DB.QueryRow(`SELECT environment FROM k8s_clusters WHERE id=?`, cid).Scan(&env)
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
		c.JSON(400, gin.H{"error": "缺 query 或 target/name/metric"})
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
	c.JSON(http.StatusOK, out)
}

// hostSelector 把环境/项目/团队拼成云主机指标的过滤条件。
//
// 主机不属于任何 K8s 集群（通用源里它们在 cluster="ecs" 下），所以不能套用集群选择器，
// 靠这三个标签区分：env=uat|prod、project=g01|g02|g32|g33|infra、team=app|dba|infra。
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
	switch target {
	case "pod":
		lbl := promLabels(selector, fmt.Sprintf("namespace=%q,pod=%q", ns, name), `container!=""`)
		if metric == "mem" {
			return `sum(container_memory_working_set_bytes` + lbl + `)`
		}
		return `sum(rate(container_cpu_usage_seconds_total` + lbl + `[5m]))`
	case "workload":
		// 按 Pod 分组 → 图上一个服务的每个 Pod 各一条线
		lbl := promLabels(selector, fmt.Sprintf("namespace=%q,pod=~%q", ns, name+"-.*"), `container!=""`)
		if metric == "mem" {
			return `sum by(pod)(container_memory_working_set_bytes` + lbl + `)`
		}
		return `sum by(pod)(rate(container_cpu_usage_seconds_total` + lbl + `[5m]))`
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
		c.JSON(400, gin.H{"error": "query(LogQL) 必填"})
		return
	}
	minutes := int64(60)
	if m, e := strconv.ParseInt(c.Query("minutes"), 10, 64); e == nil && m > 0 {
		minutes = m
	}
	end := time.Now()
	start := end.Add(-time.Duration(minutes) * time.Minute)
	u := fmt.Sprintf("%s/loki/api/v1/query_range?query=%s&start=%d&end=%d&limit=500",
		base, url.QueryEscape(q), start.UnixNano(), end.UnixNano())
	code, body, err := obsGet(u, token, 20*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": code == 200, "status": code, "data": rawJSON(body)})
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
		c.JSON(400, gin.H{"error": "path 必填(如 /kapis/devops.kubesphere.io/v1alpha3/...)"})
		return
	}
	code, body, err := obsGet(base+path, token, 20*time.Second)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": code >= 200 && code < 300, "status": code, "data": rawJSON(body)})
}

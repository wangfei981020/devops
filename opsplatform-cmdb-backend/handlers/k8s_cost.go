package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// K8sCostHandler 云成本（估算口径）。计费模式:cloud真实/idc迁云估算/none不计费(本地排除)。
// 维度:集群 / GCP云项目 / 业务项目 / 环境 / 类型。
type K8sCostHandler struct{ DB *sql.DB }

func NewK8sCostHandler(db *sql.DB) *K8sCostHandler { return &K8sCostHandler{DB: db} }

func (h *K8sCostHandler) Register(r *gin.RouterGroup) {
	r.GET("/k8s/cost/overview", h.Overview)
	r.GET("/k8s/cost/detail", h.Detail)
	r.GET("/k8s/cost/nodes", h.Nodes)
	r.POST("/k8s/cost/node-override", h.SetNodeOverride)
	r.POST("/k8s/cost/snapshot", h.SnapshotNow) // 立即打快照(?month=YYYY-MM 可选)
	r.GET("/k8s/cost/months", h.Months)          // 有快照的月份
	r.GET("/k8s/cost/report", h.Report)          // 月/季/年报告 + 环比
	r.GET("/k8s/cost/attribution", h.Attribution) // 环比归因(哪涨了)
}

type costItem struct {
	Source     string  `json:"source"` // pod/pvc/host
	ClusterID  int     `json:"cluster_id"`
	Cluster    string  `json:"cluster"`
	Mode       string  `json:"mode"` // cloud / idc （none 已排除）
	GcpProject string  `json:"gcp_project"`
	BizProject string  `json:"biz_project"`
	Env        string  `json:"env"`
	Type       string  `json:"type"` // k8s_compute / k8s_storage / traditional
	Namespace  string  `json:"namespace"`
	Name       string  `json:"name"`
	Node       string  `json:"node"`
	Cost       float64 `json:"cost"`
}

type clusterInfo struct {
	name    string
	env     string
	project string
	loc     string
	mode    string // 有效模式 cloud/idc/none
}

// effMode 空则按 provider 推断：gke→cloud、in-cluster→none、其它(generic)→idc。
func effMode(raw, provider string) string {
	if raw != "" {
		return raw
	}
	switch provider {
	case "gke":
		return "cloud"
	case "in-cluster":
		return "none"
	default:
		return "idc"
	}
}

func (h *K8sCostHandler) clusters() map[int]clusterInfo {
	m := map[int]clusterInfo{}
	rows, _ := h.DB.Query(`SELECT id, COALESCE(display_name,''), name, environment, project_id, location, provider, COALESCE(cost_mode,'') FROM k8s_clusters`)
	if rows == nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var disp, name, env, proj, loc, provider, mode string
		if rows.Scan(&id, &disp, &name, &env, &proj, &loc, &provider, &mode) != nil {
			continue
		}
		nm := disp
		if nm == "" {
			nm = name
		}
		m[id] = clusterInfo{name: nm, env: env, project: proj, loc: loc, mode: effMode(mode, provider)}
	}
	return m
}

func (h *K8sCostHandler) nsProjects() map[string]string {
	nsp := map[string]string{}
	if rows, _ := h.DB.Query(`SELECT cluster_id, namespace, project FROM k8s_ns_project`); rows != nil {
		for rows.Next() {
			var cid int
			var ns, p string
			if rows.Scan(&cid, &ns, &p) == nil && p != "" {
				nsp[strconv.Itoa(cid)+"|"+ns] = p
			}
		}
		rows.Close()
	}
	return nsp
}

type nodeCost struct {
	monthly float64
	cpuM    int
	memMi   int
}

func (h *K8sCostHandler) nodeCosts(rc *rateCache, cls map[int]clusterInfo) map[string]nodeCost {
	out := map[string]nodeCost{}
	rows, _ := h.DB.Query(`SELECT cluster_id,name,machine_type,cpu_cap,mem_cap,monthly_cost_override FROM k8s_nodes`)
	if rows == nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, mt, cpuCap, memCap string
		var override float64
		if rows.Scan(&cid, &name, &mt, &cpuCap, &memCap, &override) != nil {
			continue
		}
		ci := cls[cid]
		if ci.mode == "none" { // 本地不计费
			continue
		}
		cpuM, memMi := coresToM(cpuCap), memToMi(memCap)
		monthly := override
		if monthly <= 0 {
			hourly, _, _, _ := rc.hostHourly(ci.loc, familyOf(mt), cpuM/1000, memMi, "RUNNING", nil)
			monthly = round2(hourly * 730)
		}
		out[strconv.Itoa(cid)+"|"+name] = nodeCost{monthly: monthly, cpuM: cpuM, memMi: memMi}
	}
	return out
}

// buildItems 汇总所有计费项(none 集群排除)。
func (h *K8sCostHandler) buildItems() []costItem {
	rc := newRateCache(h.DB)
	cls := h.clusters()
	nsp := h.nsProjects()
	nodes := h.nodeCosts(rc, cls)
	items := []costItem{}

	// Pods（计算）
	if rows, _ := h.DB.Query(`SELECT cluster_id,namespace,name,node_name,workload,cpu_req_m,mem_req_mi FROM k8s_pods`); rows != nil {
		for rows.Next() {
			var cid, cpuReq, memReq int
			var ns, name, node, wl string
			if rows.Scan(&cid, &ns, &name, &node, &wl, &cpuReq, &memReq) != nil {
				continue
			}
			ci := cls[cid]
			if ci.mode == "none" {
				continue
			}
			nc, ok := nodes[strconv.Itoa(cid)+"|"+node]
			cost := 0.0
			if ok && nc.monthly > 0 {
				if nc.cpuM > 0 {
					cost += nc.monthly * 0.5 * float64(cpuReq) / float64(nc.cpuM)
				}
				if nc.memMi > 0 {
					cost += nc.monthly * 0.5 * float64(memReq) / float64(nc.memMi)
				}
			}
			items = append(items, costItem{"pod", cid, ci.name, ci.mode, orDash(ci.project), bizOf(nsp, cid, ns), ci.env, "k8s_compute", ns, dispName(wl, name), node, round2(cost)})
		}
		rows.Close()
	}
	// PVC（存储）
	if rows, _ := h.DB.Query(`SELECT cluster_id,namespace,name,capacity,storage_class FROM k8s_pvcs`); rows != nil {
		for rows.Next() {
			var cid int
			var ns, name, cap, sc string
			if rows.Scan(&cid, &ns, &name, &cap, &sc) != nil {
				continue
			}
			ci := cls[cid]
			if ci.mode == "none" {
				continue
			}
			cost := round2(float64(capToGB(cap)) * rc.diskRate(ci.loc, sc))
			items = append(items, costItem{"pvc", cid, ci.name, ci.mode, orDash(ci.project), bizOf(nsp, cid, ns), ci.env, "k8s_storage", ns, name, "", cost})
		}
		rows.Close()
	}
	// 传统主机（cloud 真实）
	if rows, _ := h.DB.Query(`SELECT c.name, h.project, h.region, h.machine_type, h.vcpu, h.mem_mb, h.disk_total_gb, h.status, COALESCE(h.labels,'')
		FROM cis c JOIN hosts h ON h.ci_id=c.id WHERE c.type='host' AND h.stale=0`); rows != nil {
		for rows.Next() {
			var name, proj, region, mt, status, labels string
			var vcpu, memMB, disk int
			if rows.Scan(&name, &proj, &region, &mt, &vcpu, &memMB, &disk, &status, &labels) != nil {
				continue
			}
			hourly, _, _, _ := rc.hostHourly(region, familyOf(mt), vcpu, memMB, status, []diskRow{{Type: "default", SizeGB: disk}})
			items = append(items, costItem{"host", 0, "(主机)", "cloud", orDash(proj), "未分配", hostEnv(labels), "traditional", "", name, "", round2(hourly * 730)})
		}
		rows.Close()
	}
	return items
}

func (h *K8sCostHandler) filtered(items []costItem, c *gin.Context) []costItem {
	mode := c.Query("mode") // cloud/idc/空(全部)
	env := c.Query("env")
	gcp := c.Query("gcp_project")
	biz := c.Query("biz_project")
	cluster := c.Query("cluster")
	out := []costItem{}
	for _, it := range items {
		if mode != "" && it.Mode != mode {
			continue
		}
		if env != "" && it.Env != env {
			continue
		}
		if gcp != "" && it.GcpProject != gcp {
			continue
		}
		if biz != "" && it.BizProject != biz {
			continue
		}
		if cluster != "" && it.Cluster != cluster {
			continue
		}
		out = append(out, it)
	}
	return out
}

func dimKey(it costItem, dim string) string {
	switch dim {
	case "cluster":
		return it.Cluster
	case "gcp_project":
		return it.GcpProject
	case "biz_project":
		return it.BizProject
	case "env":
		return orDash(it.Env)
	case "type":
		return it.Type
	}
	return it.BizProject
}

func (h *K8sCostHandler) Overview(c *gin.Context) {
	all := h.buildItems()
	dim := c.Query("dim")
	if dim == "" {
		dim = "biz_project"
	}
	// 总额:cloud 真实 vs idc 迁云估算（不受 mode 筛选影响,始终展示）
	var cloudTotal, idcTotal, kc, ks, trad float64
	for _, it := range all {
		if it.Mode == "cloud" {
			cloudTotal += it.Cost
			switch it.Type {
			case "k8s_compute":
				kc += it.Cost
			case "k8s_storage":
				ks += it.Cost
			case "traditional":
				trad += it.Cost
			}
		} else if it.Mode == "idc" {
			idcTotal += it.Cost
		}
	}
	// 维度分组(受筛选影响;默认只看 cloud 真实支出,mode 未传则 groups 用 cloud)
	items := h.filtered(all, c)
	if c.Query("mode") == "" {
		flt := items[:0]
		for _, it := range items {
			if it.Mode == "cloud" {
				flt = append(flt, it)
			}
		}
		items = flt
	}
	groups := map[string]float64{}
	var groupTotal float64
	for _, it := range items {
		groups[dimKey(it, dim)] += it.Cost
		groupTotal += it.Cost
	}
	c.JSON(http.StatusOK, gin.H{
		"currency":     "USD",
		"cloud_total":  round2(cloudTotal),
		"idc_estimate": round2(idcTotal),
		"by_type":      gin.H{"k8s_compute": round2(kc), "k8s_storage": round2(ks), "traditional": round2(trad)},
		"dim":          dim,
		"groups":       mapToSorted(groups),
		"group_total":  round2(groupTotal),
		"note":         "估算口径(机型费率×请求分摊)。cloud=真实云支出;idc=迁云估算(IT管实际);本地集群不计费。费率在 云资源→费率 配置。",
	})
}

func (h *K8sCostHandler) Detail(c *gin.Context) {
	items := h.filtered(h.buildItems(), c)
	var total float64
	for _, it := range items {
		total += it.Cost
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "count": len(items), "total": round2(total), "currency": "USD"})
}

func (h *K8sCostHandler) Nodes(c *gin.Context) {
	rc := newRateCache(h.DB)
	cls := h.clusters()
	loc := map[int]string{}
	over := map[string]float64{}
	// 直接从 nodeCosts 拿不到 override 原值,单独查
	out := []gin.H{}
	rows, _ := h.DB.Query(`SELECT cluster_id,name,machine_type,cpu_cap,mem_cap,monthly_cost_override FROM k8s_nodes ORDER BY cluster_id,name`)
	if rows != nil {
		for rows.Next() {
			var cid int
			var name, mt, cpuCap, memCap string
			var ov float64
			if rows.Scan(&cid, &name, &mt, &cpuCap, &memCap, &ov) != nil {
				continue
			}
			ci := cls[cid]
			monthly, src := ov, "manual"
			if ov <= 0 {
				if ci.mode == "none" {
					monthly, src = 0, "不计费(本地)"
				} else {
					hourly, _, _, matched := rc.hostHourly(ci.loc, familyOf(mt), coresToM(cpuCap)/1000, memToMi(memCap), "RUNNING", nil)
					monthly, src = round2(hourly*730), "费率:"+matched
					if ci.mode == "idc" {
						src = "迁云估算/" + src
					}
				}
			}
			out = append(out, gin.H{"cluster_id": cid, "cluster": ci.name, "name": name, "mode": ci.mode, "monthly": monthly, "source": src})
			_ = loc
			_ = over
		}
		rows.Close()
	}
	c.JSON(http.StatusOK, out)
}

func (h *K8sCostHandler) SetNodeOverride(c *gin.Context) {
	var in struct {
		ClusterID int     `json:"cluster_id"`
		Name      string  `json:"name"`
		Monthly   float64 `json:"monthly"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.ClusterID == 0 || in.Name == "" {
		c.JSON(400, gin.H{"error": "cluster_id/name 必填"})
		return
	}
	if _, err := h.DB.Exec(`UPDATE k8s_nodes SET monthly_cost_override=? WHERE cluster_id=? AND name=?`, in.Monthly, in.ClusterID, in.Name); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "set_k8s_node_cost", in.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// ---- 工具 ----

func bizOf(nsp map[string]string, cid int, ns string) string {
	if p := nsp[strconv.Itoa(cid)+"|"+ns]; p != "" {
		return p
	}
	return "未分配"
}
func orDash(s string) string {
	if s == "" {
		return "未设"
	}
	return s
}
func dispName(wl, pod string) string {
	if wl != "" {
		return wl
	}
	return pod
}
func hostEnv(labels string) string {
	if labels == "" {
		return "未知"
	}
	m := map[string]string{}
	if json.Unmarshal([]byte(labels), &m) == nil {
		for _, k := range []string{"env", "environment", "环境"} {
			if v := m[k]; v != "" {
				return v
			}
		}
	}
	return "未知"
}

func mapToSorted(m map[string]float64) []gin.H {
	out := []gin.H{}
	for k, v := range m {
		out = append(out, gin.H{"name": k, "cost": round2(v)})
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j]["cost"].(float64) > out[i]["cost"].(float64) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func coresToM(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if strings.HasSuffix(s, "m") {
		n, _ := strconv.Atoi(strings.TrimSuffix(s, "m"))
		return n
	}
	f, _ := strconv.ParseFloat(s, 64)
	return int(f * 1000)
}
func memToMi(s string) int {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasSuffix(s, "Ki"):
		n, _ := strconv.ParseFloat(strings.TrimSuffix(s, "Ki"), 64)
		return int(n / 1024)
	case strings.HasSuffix(s, "Mi"):
		n, _ := strconv.Atoi(strings.TrimSuffix(s, "Mi"))
		return n
	case strings.HasSuffix(s, "Gi"):
		n, _ := strconv.ParseFloat(strings.TrimSuffix(s, "Gi"), 64)
		return int(n * 1024)
	}
	n, _ := strconv.ParseFloat(s, 64)
	return int(n / 1024 / 1024)
}
func capToGB(s string) int {
	s = strings.TrimSpace(s)
	switch {
	case strings.HasSuffix(s, "Gi"):
		n, _ := strconv.ParseFloat(strings.TrimSuffix(s, "Gi"), 64)
		return int(n)
	case strings.HasSuffix(s, "Mi"):
		n, _ := strconv.ParseFloat(strings.TrimSuffix(s, "Mi"), 64)
		return int(n / 1024)
	case strings.HasSuffix(s, "Ti"):
		n, _ := strconv.ParseFloat(strings.TrimSuffix(s, "Ti"), 64)
		return int(n * 1024)
	}
	n, _ := strconv.ParseFloat(s, 64)
	return int(n / 1024 / 1024 / 1024)
}

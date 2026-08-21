package handlers

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/cloudsource"
	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/internal/cluster"
	"ops-cmdb-backend/internal/httpx"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
)

// HostHandler 云主机（一期只读）：云账号管理 + GCP 同步 + 主机台账 + 成本估算 + 域名关联。
type HostHandler struct {
	// Store 走租户隔离的读写，有租户语义的表一律走它。
	Store *store.Store
	// Mu 跨副本互斥。进程内的锁在多副本下形同虚设：
	// 同一项目的两次同步落到不同 Pod，各自的 map 都是空的，双双放行。
	Mu *cluster.Mutex
	// DB 迁移期保留：同步链路里若干下沉函数（SyncProjectNetwork 等）
	// 还是 *sql.DB 签名，等它们迁完这个字段就删。
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewHostHandler(st *store.Store, db *sql.DB, cipher *crypto.Cipher) *HostHandler {
	// Mu 从 Store 现造：跨副本锁只依赖 leases 表，没有额外配置
	return &HostHandler{Store: st, DB: db, Cipher: cipher, Mu: cluster.NewMutex(st)}
}

func (h *HostHandler) Register(r *gin.RouterGroup) {
	r.GET("/cloud-accounts", h.ListAccounts)
	r.POST("/cloud-accounts", h.CreateAccount)
	r.PUT("/cloud-accounts/:id", h.UpdateAccount)
	r.DELETE("/cloud-accounts/:id", h.DeleteAccount)
	r.POST("/cloud-accounts/:id/sync", h.SyncAccount)              // 同步账号下所有 project（后台异步）
	r.GET("/cloud-accounts/:id/sync-status", h.SyncStatus)         // 账号级：汇总 + 每项目明细
	r.GET("/cloud-projects/:pid/sync-status", h.ProjectSyncStatus) // 项目级进度
	r.POST("/cloud-accounts/:id/projects", h.CreateProject)
	r.PUT("/cloud-projects/:pid", h.UpdateProject)
	r.DELETE("/cloud-projects/:pid", h.DeleteProject)
	r.POST("/cloud-projects/:pid/sync", h.SyncProject) // 同步指定 project
	r.GET("/cloud-compute-rates", h.ListComputeRates)
	r.POST("/cloud-compute-rates", h.CreateComputeRate)
	r.PUT("/cloud-compute-rates/:id", h.UpdateComputeRate)
	r.DELETE("/cloud-compute-rates/:id", h.DeleteComputeRate)
	r.GET("/cloud-disk-rates", h.ListDiskRates)
	r.POST("/cloud-disk-rates", h.CreateDiskRate)
	r.PUT("/cloud-disk-rates/:id", h.UpdateDiskRate)
	r.DELETE("/cloud-disk-rates/:id", h.DeleteDiskRate)
	r.GET("/hosts", h.ListHosts)        // 只读
	r.GET("/hosts/:ciid", h.HostDetail) // 只读
}

// ---------- 费率（分档：区域×机型族 计算费率 + 区域×磁盘类型 磁盘费率）----------

// familyOf 从机型名取机型族：e2-medium→e2、n2-highmem-8→n2、custom-8-32768→custom。
func familyOf(machineType string) string {
	if i := strings.Index(machineType, "-"); i > 0 {
		return machineType[:i]
	}
	if machineType == "" {
		return "default"
	}
	return machineType
}

// rateCache 一次加载全部费率，按 region|key 查，命中不到回退 default。
type rateCache struct {
	compute map[string][2]float64 // "region|family" -> {vcpuHour, ramGbHour}
	disk    map[string]float64    // "region|disktype" -> gbMonth
}

// newRateCache 加载全部费率（供主机模块与 K8s 成本模块复用）。
//
//	⚠️ 参数**刻意**收成 *store.Scoped 而不是 *sql.DB：
//	费率是租户级数据（不同客户和云厂商谈的折扣不一样），
//	用 *sql.DB 的话每个调用方都得自己记得加 tenant 条件 —— 迟早漏。
//	收成 Scoped 之后，编译器会逼所有调用方先拿到租户上下文。
func newRateCache(sc *store.Scoped) *rateCache {
	rc := &rateCache{compute: map[string][2]float64{}, disk: map[string]float64{}}
	if rows, _ := sc.Query(`SELECT region, machine_family, vcpu_hour_usd, ram_gb_hour_usd FROM cloud_compute_rates WHERE tenant_id = ? AND provider='gcp'`); rows != nil {
		for rows.Next() {
			var region, family string
			var v, r float64
			if rows.Scan(&region, &family, &v, &r) == nil {
				rc.compute[region+"|"+family] = [2]float64{v, r}
			}
		}
		rows.Close()
	}
	if rows, _ := sc.Query(`SELECT region, disk_type, gb_month_usd FROM cloud_disk_rates WHERE tenant_id = ? AND provider='gcp'`); rows != nil {
		for rows.Next() {
			var region, dtype string
			var g float64
			if rows.Scan(&region, &dtype, &g) == nil {
				rc.disk[region+"|"+dtype] = g
			}
		}
		rows.Close()
	}
	return rc
}

// computeRate 取 (region, family) 计算费率，回退 default/default。返回单价 + 命中标识。
func (rc *rateCache) computeRate(region, family string) (vcpuHour, ramGbHour float64, matched string) {
	if v, ok := rc.compute[region+"|"+family]; ok {
		return v[0], v[1], region + "/" + family
	}
	if v, ok := rc.compute["default|default"]; ok {
		return v[0], v[1], "default"
	}
	return 0, 0, "无"
}

// diskRate 取 (region, diskType) 磁盘单价(GB/月)，回退 default/diskType 再回退 default/default。
func (rc *rateCache) diskRate(region, dtype string) float64 {
	if g, ok := rc.disk[region+"|"+dtype]; ok {
		return g
	}
	if g, ok := rc.disk["default|"+dtype]; ok {
		return g
	}
	if g, ok := rc.disk["default|default"]; ok {
		return g
	}
	return 0
}

type diskRow struct {
	Type   string
	SizeGB int
}

// hostDetailOut 主机详情的响应。
//
// 用具名类型而不是 gin.H：swag 从 gin.H 生成不出任何字段，
// 前端拿到的类型会是 `unknown`，等于白标注。
type hostDetailOut struct {
	Host           hostOut         `json:"host"`
	Disks          []hostDiskOut   `json:"disks"`
	RelatedDomains []relatedDomain `json:"related_domains"`
	CostHourly     float64         `json:"cost_hourly"`
	AsOf           string          `json:"as_of"`
	RateMatched    string          `json:"rate_matched"`
	RateVCPUHour   float64         `json:"rate_vcpu_hour"`
	RateRAMGbHour  float64         `json:"rate_ram_gb_hour"`
	RateFamily     string          `json:"rate_family"`

	// NodeLink 这台机器和 k8s 节点的关系，三态。
	//
	// ⚠️ 不能压成「有没有 Pod」一个布尔量。「不是集群节点」和
	// 「是节点但集群没接进来」在界面上必须长得不一样：
	// 前者是正常的（一台裸机就该没 Pod），后者是**采集缺口**，
	// 渲染成一样的空白，就等于把"我们没数据"伪装成"上面没东西"。
	//
	//	linked        已关联，Pods 是真实清单（可以为空 = 节点上真的没 Pod）
	//	not_ingested  hosts.is_k8s_node=1 但 k8s_nodes 里查不到 → 该集群未接入
	//	none          不是 k8s 节点
	NodeLink string       `json:"node_link"`
	Node     *hostNodeOut `json:"node,omitempty"`
	Pods     []hostPodOut `json:"pods"`
}

// nodeLinkState 判定主机与 k8s 节点的关联三态。
//
// 单拎成纯函数是因为它错了不会报错：三种情况在界面上都是"没有 Pod"，
// 只有把每种组合的期望值写成测试才拦得住。
//
//	found      k8s_nodes 里查到了这台机器
//	isK8sNode  主机同步侧标记它是集群节点
//	stale      机器已销毁（云上查不到了）
func nodeLinkState(found, isK8sNode, stale bool) string {
	if found {
		return "linked"
	}
	// ⚠️ 已销毁的机器不算采集缺口：机器都没了，节点当然跟着没。
	// 不排除的话，每台退役过的节点都会长期挂一条「集群未接入」，
	// 而这种假缺口攒多了，真的缺口就没人看了。
	if isK8sNode && !stale {
		return "not_ingested"
	}
	return "none"
}

// hostNodeOut 这台机器作为 k8s 节点的那一面。
type hostNodeOut struct {
	ClusterID   int    `json:"cluster_id"`
	ClusterName string `json:"cluster_name"`
	Name        string `json:"name"`
	Pool        string `json:"pool"`
	ReadyStatus string `json:"ready_status"`
	// PodCount 是节点自己上报的数量，和 len(Pods) 可能对不上
	// （两张表由不同轮次的同步写入）。对不上时前端要显式提示，
	// 而不是挑一个显示——挑一个就等于替用户判断哪份数据可信。
	PodCount int `json:"pod_count"`
}

// hostPodOut 跑在这台机器上的 Pod。
type hostPodOut struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Workload  string `json:"workload"`
	Phase     string `json:"phase"`
	Restarts  int    `json:"restarts"`
}

// relatedDomain 解析到这台机器的业务域名。
//
// 具名类型而不是 gin.H：swag 从 gin.H 里生成不出任何字段，
// 前端拿到的会是 unknown，等于白标注一场。
type relatedDomain struct {
	FQDN string `json:"fqdn"`
	IP   string `json:"ip"`
}

// hostDiskOut 对外的磁盘明细。
type hostDiskOut struct {
	Name   string `json:"name"`
	SizeGB int    `json:"size_gb"`
	Type   string `json:"type"`
	IsBoot bool   `json:"is_boot"`
	// UsedPercent 用指针是为了能表达 null。
	//
	// ⚠️ null ≠ 0：null 是「没采到」（集群没接 Prometheus、设备名对不上），
	// 0 是「真的是空盘」。用 0 当哨兵值会让没采到的盘被算进平均值，
	// 把整体用量算低；前端也没法区分该显示「未接入」还是「0%」。
	// omitempty 是刻意的：Swagger 2.0 没有 nullable 概念，
	// 生成出的 TS 类型是 `used_percent?: number`（optional）。
	// 若 nil 时仍输出 `null`，类型与实际就对不上了 —— 编译期看着没问题，
	// 运行时拿到 null 去做算术会得到 0，把「没采到」变成「空盘」。
	// 省略字段则与 optional 语义严格一致。
	UsedPercent *float64 `json:"used_percent,omitempty"`
	// MountPoint 挂载点。排障时它比云盘名有用得多 ——
	// 人关心的是 /var/lib/kafka 满了，不是 persistent-disk-2 满了。
	MountPoint string     `json:"mount_point,omitempty"`
	UsedAt     *time.Time `json:"used_at,omitempty"`
}

// hostHourly 估算每小时成本(USD)。停机(非 RUNNING)只算磁盘；运行算 vCPU+内存+磁盘。
func (rc *rateCache) hostHourly(region, family string, vcpu, memMB int, status string, disks []diskRow) (hourly, vcpuHour, ramGbHour float64, matched string) {
	vcpuHour, ramGbHour, matched = rc.computeRate(region, family)
	compute := 0.0
	if status == "RUNNING" {
		compute = float64(vcpu)*vcpuHour + (float64(memMB)/1024.0)*ramGbHour
	}
	disk := 0.0
	for _, d := range disks {
		disk += float64(d.SizeGB) * rc.diskRate(region, d.Type) / 730.0
	}
	return compute + disk, vcpuHour, ramGbHour, matched
}

// ---- 计算费率 CRUD ----

func (h *HostHandler) ListComputeRates(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	rows, err := sc.Query(`SELECT id, region, machine_family, vcpu_hour_usd, ram_gb_hour_usd, note FROM cloud_compute_rates WHERE tenant_id = ? AND provider='gcp' ORDER BY region, machine_family`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type rt struct {
		ID        int     `json:"id"`
		Region    string  `json:"region"`
		Family    string  `json:"machine_family"`
		VcpuHour  float64 `json:"vcpu_hour_usd"`
		RamGbHour float64 `json:"ram_gb_hour_usd"`
		Note      string  `json:"note"`
	}
	out := []rt{}
	for rows.Next() {
		var x rt
		if rows.Scan(&x.ID, &x.Region, &x.Family, &x.VcpuHour, &x.RamGbHour, &x.Note) == nil {
			out = append(out, x)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *HostHandler) CreateComputeRate(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		Region    string  `json:"region"`
		Family    string  `json:"machine_family"`
		VcpuHour  float64 `json:"vcpu_hour_usd"`
		RamGbHour float64 `json:"ram_gb_hour_usd"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Region == "" || in.Family == "" {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.rateRegionFamilyRequired", nil, nil)
		return
	}
	if _, err := sc.Insert(`INSERT INTO cloud_compute_rates (tenant_id,provider,region,machine_family,vcpu_hour_usd,ram_gb_hour_usd,note) VALUES (?,'gcp',?,?,?,?,'manual')`,
		in.Region, in.Family, in.VcpuHour, in.RamGbHour); err != nil {
		httpx.FailKey(c, httpx.CodeConflict, "error.rateAlreadyExists", err, nil)
		return
	}
	c.JSON(201, gin.H{"ok": true})
}

func (h *HostHandler) UpdateComputeRate(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 🔴 指针 = 三态。用 float64 的话，只传 vcpu 费率会把内存费率**静默置 0** ——
	//	那等于在成本核算里把内存算成免费的，而账面上一切正常（OPSCMDB-083）。
	var in struct {
		VcpuHour  *float64 `json:"vcpu_hour_usd"`
		RamGbHour *float64 `json:"ram_gb_hour_usd"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	p := &patchSet{}
	p.Add("vcpu_hour_usd", in.VcpuHour)
	p.Add("ram_gb_hour_usd", in.RamGbHour)
	if p.Empty() {
		httpx.Invalid(c, "body", "至少要传一个要改的字段")
		return
	}
	// 人工改过的标 confirmed（不再是 estimate 待核对）
	res, err := sc.Exec(`UPDATE cloud_compute_rates SET `+p.SQL()+`, note='confirmed' WHERE tenant_id = ? AND id=?`,
		append(p.Args(), c.Param("id"))...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// 404 而非 403：403 等于告诉对方"这个 id 存在，只是不给你"。
		httpx.NotFound(c, "rate")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *HostHandler) DeleteComputeRate(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := sc.Exec(`DELETE FROM cloud_compute_rates WHERE tenant_id = ? AND id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---- 磁盘费率 CRUD ----

func (h *HostHandler) ListDiskRates(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	rows, err := sc.Query(`SELECT id, region, disk_type, gb_month_usd, note FROM cloud_disk_rates WHERE tenant_id = ? AND provider='gcp' ORDER BY region, disk_type`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type rt struct {
		ID       int     `json:"id"`
		Region   string  `json:"region"`
		DiskType string  `json:"disk_type"`
		GbMonth  float64 `json:"gb_month_usd"`
		Note     string  `json:"note"`
	}
	out := []rt{}
	for rows.Next() {
		var x rt
		if rows.Scan(&x.ID, &x.Region, &x.DiskType, &x.GbMonth, &x.Note) == nil {
			out = append(out, x)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *HostHandler) CreateDiskRate(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		Region   string  `json:"region"`
		DiskType string  `json:"disk_type"`
		GbMonth  float64 `json:"gb_month_usd"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Region == "" || in.DiskType == "" {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.rateRegionDiskRequired", nil, nil)
		return
	}
	if _, err := sc.Insert(`INSERT INTO cloud_disk_rates (tenant_id,provider,region,disk_type,gb_month_usd,note) VALUES (?,'gcp',?,?,?,'manual')`,
		in.Region, in.DiskType, in.GbMonth); err != nil {
		httpx.FailKey(c, httpx.CodeConflict, "error.rateAlreadyExists", err, nil)
		return
	}
	c.JSON(201, gin.H{"ok": true})
}

func (h *HostHandler) UpdateDiskRate(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		GbMonth float64 `json:"gb_month_usd"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	res, err := sc.Exec(`UPDATE cloud_disk_rates SET gb_month_usd=?, note='confirmed' WHERE tenant_id = ? AND id=?`,
		in.GbMonth, c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.NotFound(c, "rate")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *HostHandler) DeleteDiskRate(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := sc.Exec(`DELETE FROM cloud_disk_rates WHERE tenant_id = ? AND id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---------- 云账号（分组层：name/provider/billing，无凭据） ----------

func (h *HostHandler) ListAccounts(c *gin.Context) {
	// 各 project 的主机数
	hostCnt := map[int]int{}
	if crows, _ := h.DB.Query(`SELECT p.id, COUNT(hh.ci_id)
		FROM cloud_account_projects p
		LEFT JOIN hosts hh ON hh.cloud_account_id=p.account_id AND hh.project=p.project_id AND hh.stale=0
		GROUP BY p.id`); crows != nil {
		for crows.Next() {
			var pid, n int
			if crows.Scan(&pid, &n) == nil {
				hostCnt[pid] = n
			}
		}
		crows.Close()
	}
	// 项目按账号归组
	type projOut struct {
		ID         int    `json:"id"`
		Name       string `json:"name"`
		ProjectID  string `json:"project_id"`
		HasCred    bool   `json:"has_cred"`
		LastSyncAt string `json:"last_sync_at"`
		LastResult string `json:"last_result"`
		// LastFailed 上次同步**失败了**。
		//
		//	🔴 判据必须在后端。前端原来是拿正则去认成功词
		//	（`/成功|完成|同步 \d+/`）—— 那是**正向匹配**，
		//	一条措辞不同的成功消息就会被标成"需要注意"，
		//	而两条同样成功的同步一绿一橙（OPSCMDB-031 P2-53）。
		//
		//	⚠️ 用否定判据（认失败词）而不是肯定判据（认成功词）：
		//	新增一种成功文案时，前者安静地正确，后者安静地误报。
		LastFailed bool `json:"last_failed"`
		HostCount  int  `json:"host_count"`
	}
	projByAcct := map[int][]projOut{}
	if prows, _ := h.DB.Query(`SELECT id, account_id, name, project_id, COALESCE(cred_enc,''), last_sync_at, last_result
		FROM cloud_account_projects ORDER BY id`); prows != nil {
		for prows.Next() {
			var p projOut
			var aid int
			var enc string
			var ls sql.NullTime
			if prows.Scan(&p.ID, &aid, &p.Name, &p.ProjectID, &enc, &ls, &p.LastResult) == nil {
				p.HasCred = enc != ""
				p.LastFailed = isFailedSyncResult(p.LastResult)
				p.HostCount = hostCnt[p.ID]
				if ls.Valid {
					p.LastSyncAt = ls.Time.Format("2006-01-02 15:04")
				}
				projByAcct[aid] = append(projByAcct[aid], p)
			}
		}
		prows.Close()
	}
	rows, err := h.DB.Query(`SELECT id, name, provider, billing_export_dataset FROM cloud_accounts ORDER BY id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type acct struct {
		ID        int       `json:"id"`
		Name      string    `json:"name"`
		Provider  string    `json:"provider"`
		BillingDS string    `json:"billing_export_dataset"`
		Projects  []projOut `json:"projects"`
	}
	out := []acct{}
	for rows.Next() {
		var a acct
		if rows.Scan(&a.ID, &a.Name, &a.Provider, &a.BillingDS) == nil {
			a.Projects = projByAcct[a.ID]
			if a.Projects == nil {
				a.Projects = []projOut{}
			}
			out = append(out, a)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *HostHandler) CreateAccount(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		Name      string `json:"name"`
		Provider  string `json:"provider"`
		BillingDS string `json:"billing_export_dataset"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		httpx.Required(c, "name")
		return
	}
	if in.Provider == "" {
		in.Provider = "gcp"
	}
	if _, err := sc.Insert(`INSERT INTO cloud_accounts (tenant_id, name, provider, billing_export_dataset) VALUES (?, ?, ?, ?)`,
		in.Name, in.Provider, in.BillingDS); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	SetAuditTarget(c, in.Name)
	c.JSON(201, gin.H{"ok": true})
}

func (h *HostHandler) UpdateAccount(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 指针 = 三态：没传（不动它）/ 显式清空 / 改成它（OPSCMDB-083）
	var in struct {
		Name      *string `json:"name"`
		BillingDS *string `json:"billing_export_dataset"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if requireNonBlank(c, "name", in.Name) {
		return
	}
	p := &patchSet{}
	p.Add("name", in.Name)
	p.Add("billing_export_dataset", in.BillingDS)
	if p.Empty() {
		httpx.Invalid(c, "body", "至少要传一个要改的字段")
		return
	}
	res, err := sc.Exec(`UPDATE cloud_accounts SET `+p.SQL()+` WHERE tenant_id = ? AND id=?`,
		append(p.Args(), c.Param("id"))...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.NotFound(c, "account")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *HostHandler) DeleteAccount(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 删账号：其下 project + 主机一并清（只读同步来的，无业务数据）
	//
	//	⚠️ 顺序：**先删父行**（cloud_accounts），删不到就说明这个 id
	//	不属于本租户，直接 404 返回，一行子数据都不碰。
	//	反过来先删子表的话，别的租户的 account_id 传进来会先把人家的
	//	主机、CI 全清掉，最后才发现父行删不动 —— 那时数据已经没了。
	tx, err := sc.Begin()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback() //nolint:errcheck // Commit 成功后这里是 no-op
	id := c.Param("id")
	res, err := tx.Exec(`DELETE FROM cloud_accounts WHERE tenant_id = ? AND id=?`, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.NotFound(c, "account")
		return
	}
	for _, q := range []string{
		`DELETE hd FROM host_disks hd JOIN hosts h ON h.ci_id=hd.host_ci_id WHERE hd.tenant_id = ? AND h.cloud_account_id=?`,
		`DELETE c FROM cis c JOIN hosts h ON h.ci_id=c.id WHERE c.tenant_id = ? AND h.cloud_account_id=?`,
		`DELETE FROM hosts WHERE tenant_id = ? AND cloud_account_id=?`,
		`DELETE FROM cloud_account_projects WHERE tenant_id = ? AND account_id=?`,
	} {
		if _, err := tx.Exec(q, id); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---------- 云项目（凭据在这一层：name/project_id/cred） ----------

func (h *HostHandler) CreateProject(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		Name      string `json:"name"`
		ProjectID string `json:"project_id"`
		CredJSON  string `json:"cred_json"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.ProjectID == "" {
		httpx.Required(c, "project_id")
		return
	}
	if in.Name == "" {
		in.Name = in.ProjectID
	}
	enc := ""
	if in.CredJSON != "" {
		e, err := h.Cipher.Encrypt(in.CredJSON)
		if err != nil {
			httpx.FailKey(c, httpx.CodeInternal, "error.credEncryptFailed", nil, nil)
			return
		}
		enc = e
	}
	// 父账号必须属于本租户，否则等于往别人的账号下面挂项目。
	var okAcct int
	if sc.QueryRow(`SELECT 1 FROM cloud_accounts WHERE tenant_id = ? AND id=?`, c.Param("id")).Scan(&okAcct) != nil {
		httpx.NotFound(c, "account")
		return
	}
	if _, err := sc.Insert(`INSERT INTO cloud_account_projects (tenant_id, account_id, name, project_id, cred_enc) VALUES (?, ?, ?, ?, ?)`,
		c.Param("id"), in.Name, in.ProjectID, enc); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	SetAuditTarget(c, in.ProjectID)
	c.JSON(201, gin.H{"ok": true})
}

func (h *HostHandler) UpdateProject(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		Name      string `json:"name"`
		ProjectID string `json:"project_id"`
		CredJSON  string `json:"cred_json"` // 空=不改凭据
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.ProjectID == "" {
		httpx.Required(c, "project_id")
		return
	}
	if in.Name == "" {
		in.Name = in.ProjectID
	}
	res, err := sc.Exec(`UPDATE cloud_account_projects SET name=?, project_id=? WHERE tenant_id = ? AND id=?`,
		in.Name, in.ProjectID, c.Param("pid"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.NotFound(c, "project")
		return
	}
	if in.CredJSON != "" {
		e, err := h.Cipher.Encrypt(in.CredJSON)
		if err != nil {
			// 凭据加密失败不能静默 —— 用户以为换了凭据，实际还是旧的，
			// 下次同步失败时完全对不上因果。
			httpx.FailKey(c, httpx.CodeInternal, "error.credEncryptFailed", err, nil)
			return
		}
		if _, err := sc.Exec(`UPDATE cloud_account_projects SET cred_enc=? WHERE tenant_id = ? AND id=?`, e, c.Param("pid")); err != nil {
			httpx.FailKey(c, httpx.CodeInternal, "error.credSaveFailed", err, nil)
			return
		}
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *HostHandler) DeleteProject(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	pid := c.Param("pid")
	var accountID int
	var projectID string
	if sc.QueryRow(`SELECT account_id, project_id FROM cloud_account_projects WHERE tenant_id = ? AND id=?`, pid).
		Scan(&accountID, &projectID) != nil {
		httpx.NotFound(c, "project")
		return
	}
	tx, err := sc.Begin()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback() //nolint:errcheck // Commit 成功后是 no-op
	// 同 DeleteAccount：先删父行确认归属，再动子表。
	res, err := tx.Exec(`DELETE FROM cloud_account_projects WHERE tenant_id = ? AND id=?`, pid)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.NotFound(c, "project")
		return
	}
	for _, q := range []string{
		`DELETE hd FROM host_disks hd JOIN hosts h ON h.ci_id=hd.host_ci_id WHERE hd.tenant_id = ? AND h.cloud_account_id=? AND h.project=?`,
		`DELETE c FROM cis c JOIN hosts h ON h.ci_id=c.id WHERE c.tenant_id = ? AND h.cloud_account_id=? AND h.project=?`,
		`DELETE FROM hosts WHERE tenant_id = ? AND cloud_account_id=? AND project=?`,
	} {
		if _, err := tx.Exec(q, accountID, projectID); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---------- 同步 ----------

// ---- 主机同步的后台状态（进程内，按云账号 id）----
type hostSyncState struct {
	Running               bool
	Total, Done           int // 实例总数 / 已写入
	Synced, Stale         int
	Err                   string
	StartedAt, FinishedAt time.Time
	AccountID             int
	ProjectID             int64
	ProjectName           string
	// release 释放跨副本锁。同步结束时调用；为 nil 表示没拿锁（单副本或未注入 Mu）
	release func()
	// lastPersist 上次把进度写进库的时刻，用于节流。
	// ⚠️ 不节流的话，一个上千台的项目会产生上千次 UPDATE ——
	// 把一次同步变成对库的持续小写入，而进度精确到毫秒对人毫无意义
	lastPersist time.Time
	// TenantID 落库要用；goroutine 里拿不到请求上下文，起任务时就存下来
	TenantID int64
}

// 进度按 **project** 存，不按 account。
//
// 之前 key 是 accountID，导致同一账号下所有项目共用一份进度：三个项目
// （24 台 / 38 台 / 0 台）在界面上全都显示同一个数字，其中还包括一个同步失败、
// 一台主机都没有的项目。互斥也因此被抬到账号级——同账号里同步一个项目时，
// 点另一个项目会被直接拒。两个问题同源，一起降到项目粒度。
var (
	hostSyncMu    sync.Mutex
	hostSyncStore = map[int64]*hostSyncState{}
)

// hsPersist 把进度写进 host_sync_progress，供**其他副本**读到。
//
// ⚠️ 进程内的 hostSyncStore 只是快路径。判"在不在跑"必须以库为准 ——
// 否则轮询打到没接过这次请求的副本就会得到"没在跑"（OPSCMDB-016）。
//
// force=false 时按秒节流；起始与收尾必须 force=true，那两个点丢了就全乱了。
func (h *HostHandler) hsPersist(st *hostSyncState, force bool) {
	if st == nil || h.DB == nil {
		return
	}
	hostSyncMu.Lock()
	if !force && time.Since(st.lastPersist) < time.Second {
		hostSyncMu.Unlock()
		return
	}
	st.lastPersist = time.Now()
	running := 0
	if st.Running {
		running = 1
	}
	pid, tid, acct, name := st.ProjectID, st.TenantID, st.AccountID, st.ProjectName
	total, done, synced, stale, errMsg := st.Total, st.Done, st.Synced, st.Stale, truncate(st.Err, 480)
	hostSyncMu.Unlock()

	if _, err := h.DB.Exec(`INSERT INTO host_sync_progress
		 (project_id, tenant_id, account_id, project, running, total, done, synced, stale, err, started_at, updated_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,NOW(),NOW())
		 ON DUPLICATE KEY UPDATE account_id=VALUES(account_id), project=VALUES(project),
		   running=VALUES(running), total=VALUES(total), done=VALUES(done),
		   synced=VALUES(synced), stale=VALUES(stale), err=VALUES(err), updated_at=NOW()`,
		pid, tid, acct, name, running, total, done, synced, stale, errMsg); err != nil {
		// 进度写不进去不该让同步本身失败 —— 但必须留痕，否则"进度不动"会被当成同步卡住
		logx.J("host_sync", "progress_persist_err", map[string]any{"project_id": pid, "err": err.Error()})
	}
}

// hsProgressRows 读某账号（pid>0 时读单个项目）的进度。
//
// ⚠️ running 不能直接信库里的值：进程被 kill 时它会永远停在 1。
// updated_at 超过锁 TTL（30 分钟，与 leases 对齐）就当作已死。
func (h *HostHandler) hsProgressRows(tenantID int64, accountID int, pid int64) ([]gin.H, bool) {
	q := `SELECT project_id, project, running, total, done, synced, stale, err,
	             TIMESTAMPDIFF(SECOND, updated_at, NOW())
	      FROM host_sync_progress WHERE tenant_id=?`
	args := []any{tenantID}
	if pid > 0 {
		q += ` AND project_id=?`
		args = append(args, pid)
	} else {
		q += ` AND account_id=?`
		args = append(args, accountID)
	}
	q += ` ORDER BY project_id`
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		logx.J("host_sync", "progress_read_err", map[string]any{"err": err.Error()})
		return nil, false
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id int64
		var name, errMsg string
		var running, total, done, synced, stale, ageSec int
		if rows.Scan(&id, &name, &running, &total, &done, &synced, &stale, &errMsg, &ageSec) != nil {
			continue
		}
		live := running == 1 && ageSec < int(30*time.Minute/time.Second)
		if running == 1 && !live {
			// 说出来而不是悄悄改成 false：这代表那个副本挂了，是要查的事
			errMsg = strings.TrimSpace(errMsg + " 同步进程已失联（超过 30 分钟没有更新），状态按已结束处理")
		}
		out = append(out, gin.H{
			"project_id": id, "project": name, "running": live,
			"total": total, "done": done, "synced": synced, "stale": stale, "error": errMsg,
		})
	}
	return out, true
}

func hsSet(st *hostSyncState, f func(*hostSyncState)) {
	if st == nil {
		return
	}
	hostSyncMu.Lock()
	f(st)
	hostSyncMu.Unlock()
}

// startProjectSync 起一个项目级同步状态；该项目已在跑返回 nil。
// 不同项目互不阻塞——它们用各自的凭据打各自的 GCP project，本来就没有共享资源。
//
//	⚠️ 这个 map 是**进程内**的。多副本下，同一个项目的两次同步请求
//	落到不同 Pod 时，各自那份 map 都是空的 —— 双双放行，
//	于是同一个 GCP project 被并发拉两遍：白烧配额、还可能撞上对方的写。
//	所以进程内这道只是快路径，真正的互斥用 leases 表（见下）。
func (h *HostHandler) startProjectSync(accountID int, pid int64, projName string, tenantID int64) *hostSyncState {
	hostSyncMu.Lock()
	defer hostSyncMu.Unlock()
	if st := hostSyncStore[pid]; st != nil && st.Running {
		return nil
	}
	// 跨副本互斥：拿不到说明别的副本正在同步这个项目。
	// ttl 给 30 分钟 —— 大项目同步很慢，太短会在还没跑完时把锁放掉。
	// 崩溃时锁会在 ttl 后自动过期，不会永久卡死。
	if h.Mu != nil {
		release, err := h.Mu.Lock(context.Background(),
			fmt.Sprintf("host_sync:project:%d", pid), 30*time.Minute)
		if err != nil {
			logx.J("host_sync", "skip_locked_elsewhere", map[string]any{
				"project_id": pid, "err": err.Error(),
				"note": "另一个副本正在同步这个项目，本次跳过",
			})
			return nil
		}
		defer func() {
			// 挂到状态上，finishHostSync 时释放
			if st := hostSyncStore[pid]; st != nil {
				st.release = release
			}
		}()
	}
	st := &hostSyncState{Running: true, StartedAt: time.Now(),
		AccountID: accountID, ProjectID: pid, ProjectName: projName, TenantID: tenantID}
	hostSyncStore[pid] = st
	// ⚠️ 这里**不能**落库：本函数持有 hostSyncMu，而 hsPersist 也要拿同一把锁，
	// 同步调用会死锁；改成 goroutine 又会让"POST 已返回但库里还没记录"成为常态，
	// 第一次轮询照样扑空。所以由调用方在锁释放后立刻落一次（见两处调用点）。
	return st
}

// projectMeta 取项目所属账号与显示名，供进度展示用。
func (h *HostHandler) projectMeta(pid int64) (accountID int, name string, ok bool) {
	var n, projID *string
	if h.DB.QueryRow(`SELECT account_id, name, project_id FROM cloud_account_projects WHERE id=?`, pid).
		Scan(&accountID, &n, &projID) != nil {
		return 0, "", false
	}
	switch {
	case n != nil && *n != "":
		name = *n
	case projID != nil:
		name = *projID
	}
	return accountID, name, true
}

func (h *HostHandler) finishHostSync(st *hostSyncState, errMsg string) {
	hostSyncMu.Lock()
	st.Running = false
	st.FinishedAt = time.Now()
	if errMsg != "" {
		st.Err = errMsg
	}
	release := st.release
	st.release = nil
	hostSyncMu.Unlock()
	// 收尾这一次必须写：它是"跑完了"这个事实唯一的跨副本载体
	h.hsPersist(st, true)
	// 放跨副本锁。⚠️ 必须在锁外调用：release 会写库，
	// 拿着进程内 mutex 去写库是把一次网络往返变成全局串行点
	if release != nil {
		release()
	}
}

// SyncProject 同步指定 project（后台异步 + 进度；主机多约 1-3 分钟，避免 HTTP 超时误报失败）。
func (h *HostHandler) SyncProject(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	pid, _ := strconv.ParseInt(c.Param("pid"), 10, 64)
	var accountID int
	if sc.QueryRow(`SELECT account_id FROM cloud_account_projects WHERE tenant_id = ? AND id=?`, pid).Scan(&accountID) != nil {
		httpx.NotFound(c, "project")
		return
	}
	_, projName, _ := h.projectMeta(pid)
	st := h.startProjectSync(accountID, pid, projName, int64(sc.TenantID()))
	if st == nil {
		httpx.FailKey(c, httpx.CodeConflict, "error.syncInProgress", nil, nil)
		return
	}
	// 在 POST 返回之前把"已开始"落库：前端拿到 202 立刻就会轮询，
	// 而那次轮询很可能打到别的副本
	h.hsPersist(st, true)
	SetAuditTarget(c, strconv.FormatInt(pid, 10))
	// ⚠️ 不能把请求作用域的 sc 带进 goroutine：HTTP 响应一返回，
	//    c.Request.Context() 就被取消，后台同步会在第一条 SQL 上直接失败。
	//    所以只取出租户号，另建一个不受请求生命周期影响的后台上下文。
	tenant := sc.TenantID()
	go func() {
		bsc, err := h.Store.Tenant(store.ForJob(context.Background(), tenant, "host_sync"))
		if err != nil {
			logx.J("host", "sync_scope_err", map[string]any{"tenant_id": int64(tenant), "err": err.Error()})
			return
		}
		// 手动触发也要进「执行记录」——之前只有定时任务写，手动点完在执行记录里
		// 什么都看不到，用户无从判断到底跑没跑。
		runID, start := startManualRunLog(bsc, "host_sync", "手动同步项目 "+projName)
		r, err := h.syncOneProject(bsc, pid, st)
		e := ""
		if err != nil {
			e = err.Error()
		}
		h.finishHostSync(st, e)
		finishManualRunLog(bsc, runID, start, err,
			projName+"："+hostSyncSummary(r.Synced, r.Gone, r.NewlyGone))
	}()
	// msg_key 给界面翻译，msg 保留给 MCP / 直接调 API 的人 ——
	// 那一侧的读者是 AI 和运维，一句中文比一个 key 有用得多。
	// ⚠️ 两个都发不是冗余：删掉 msg，AI 拿到的就只有 "hosts:sync.started"。
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "running": true,
		"msg_key": "hosts:sync.started",
		"msg":     "已在后台同步，主机多约 1-3 分钟，完成后自动刷新"})
}

// SyncAccount 后台异步同步账号下所有 project（各用各自凭据，依次跑；单个失败不影响其它）。
func (h *HostHandler) SyncAccount(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	accountID, _ := strconv.Atoi(c.Param("id"))
	rows, err := sc.Query(`SELECT id FROM cloud_account_projects WHERE tenant_id = ? AND account_id=? ORDER BY id`, accountID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var pids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			pids = append(pids, id)
		}
	}
	rows.Close()
	if len(pids) == 0 {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.accountHasNoProject", nil, nil)
		return
	}
	// 为每个项目各起一份进度。已在单独同步中的项目跳过，不因为其中一个在跑就拒掉整批。
	type job struct {
		pid  int64
		st   *hostSyncState
		name string
	}
	jobs := make([]job, 0, len(pids))
	skipped := []string{}
	for _, pid := range pids {
		_, pn, _ := h.projectMeta(pid)
		st := h.startProjectSync(accountID, pid, pn, int64(sc.TenantID()))
		if st == nil {
			skipped = append(skipped, pn)
			continue
		}
		h.hsPersist(st, true) // 同上：返回前就要让其他副本看得见
		jobs = append(jobs, job{pid, st, pn})
	}
	if len(jobs) == 0 {
		httpx.FailKey(c, httpx.CodeConflict, "error.allProjectsSyncing", nil, nil)
		return
	}
	SetAuditTarget(c, c.Param("id"))
	tenant := sc.TenantID() // 同上：goroutine 不能用请求上下文
	go func() {
		bsc, err := h.Store.Tenant(store.ForJob(context.Background(), tenant, "host_sync"))
		if err != nil {
			logx.J("host", "sync_scope_err", map[string]any{"tenant_id": int64(tenant), "err": err.Error()})
			return
		}
		// 串行跑：各项目虽用各自凭据，但共享 GCP API 配额，并发容易撞限流
		runID, start := startManualRunLog(bsc, "host_sync", "手动同步账号下全部项目")
		var errs []string
		totalSynced, totalStale, totalNewlyGone := 0, 0, 0
		for _, j := range jobs {
			r, err := h.syncOneProject(bsc, j.pid, j.st)
			e := ""
			if err != nil {
				e = err.Error()
				errs = append(errs, j.name+": "+e)
			}
			totalSynced += r.Synced
			totalStale += r.Gone
			totalNewlyGone += r.NewlyGone
			// 每个项目跑完就结束自己那份进度，前端能逐个看到完成
			h.finishHostSync(j.st, e)
		}
		var runErr error
		if len(errs) > 0 {
			runErr = fmt.Errorf("%s", strings.Join(errs, "；"))
		}
		finishManualRunLog(bsc, runID, start, runErr,
			fmt.Sprintf("同步 %d 个项目：", len(jobs))+hostSyncSummary(totalSynced, totalStale, totalNewlyGone))
	}()
	resp := gin.H{"ok": true, "running": true, "projects": len(jobs),
		"msg_key": "hosts:sync.started",
		"msg":     "已在后台同步，主机多约 1-3 分钟，完成后自动刷新"}
	if len(skipped) > 0 {
		// 跳过了什么必须说出来，否则用户以为全部都在跑
		resp["skipped"] = skipped
		resp["msg"] = fmt.Sprintf("已同步 %d 个项目；%d 个项目正在同步中已跳过：%s",
			len(jobs), len(skipped), strings.Join(skipped, "、"))
	}
	c.JSON(http.StatusAccepted, resp)
}

// SyncStatus 查某云账号后台同步进度（前端轮询）。
//
// 进度按项目存，这里汇总该账号下所有项目：顶层字段是合计（保持与旧版兼容），
// projects 数组给出每个项目各自的进度——界面上三行显示同一个数字的毛病就出在
// 以前没有这份明细，各行只能去读同一个账号级计数。
func (h *HostHandler) SyncStatus(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	// ⚠️ 读库不读进程内的 map。map 只有"接过这次 POST 的那个副本"才有，
	// 轮询打到别的副本就会得到 started=false —— 那是假的（OPSCMDB-016）
	items, ok := h.hsProgressRows(int64(sc.TenantID()), id, 0)
	if !ok {
		// 读失败要说出来，不能退化成"没在跑"：那会让界面把故障显示成正常
		httpx.FailKey(c, httpx.CodeInternal, "error.readSyncProgressFailed", err, nil)
		return
	}
	if len(items) == 0 {
		c.JSON(http.StatusOK, gin.H{"running": false, "started": false, "projects": []gin.H{}})
		return
	}
	anyRunning := false
	agg := struct{ Total, Done, Synced, Stale int }{}
	errs := []string{}
	for _, it := range items {
		if it["running"].(bool) {
			anyRunning = true
		}
		// 🔴 只累加**这一轮在跑**的项目。
		//
		//	host_sync_progress 每个项目留一行，**不管那次同步是什么时候跑的**。
		//	原来无条件累加，于是点单个项目时，分子分母里混进了另外两个项目
		//	上一次的进度 —— 出来的数既不是"这次要同步多少"，也不是"总共多少台"。
		//
		//	实测两种表现，都对不上任何真实数量：
		//	  点 public-uat（24 台）→ 显示 62/62
		//	  用户截图        → 显示 103/103（三个项目合计 127 台）
		//	而且因为历史行的 done==total，进度**一打开就是 100%**（OPSCMDB-075）。
		//
		// ⚠️ 一个数字对不上实际，比没有这个数字更糟：它看起来精确，
		//	所以人会拿它做判断（"已经跑完了"），而它其实在说别的事。
		if !it["running"].(bool) {
			continue
		}
		agg.Total += it["total"].(int)
		agg.Done += it["done"].(int)
		agg.Synced += it["synced"].(int)
		agg.Stale += it["stale"].(int)
		if e, _ := it["error"].(string); e != "" {
			errs = append(errs, it["project"].(string)+": "+e)
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"running": anyRunning, "started": true,
		"total": agg.Total, "done": agg.Done, "synced": agg.Synced, "stale": agg.Stale,
		"error": strings.Join(errs, "；"), "projects": items,
	})
}

// ProjectSyncStatus 查单个项目的同步进度。
func (h *HostHandler) ProjectSyncStatus(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	pid, _ := strconv.ParseInt(c.Param("pid"), 10, 64)
	items, ok := h.hsProgressRows(int64(sc.TenantID()), 0, pid) // 同上：以库为准
	if !ok {
		httpx.FailKey(c, httpx.CodeInternal, "error.readSyncProgressFailed", err, nil)
		return
	}
	if len(items) == 0 {
		c.JSON(http.StatusOK, gin.H{"running": false, "started": false})
		return
	}
	it := items[0]
	c.JSON(http.StatusOK, gin.H{"running": it["running"], "started": true, "project": it["project"],
		"total": it["total"], "done": it["done"], "synced": it["synced"], "stale": it["stale"], "error": it["error"]})
}

// startManualRunLog 手动触发的同步也写一条执行记录。
// 之前只有定时任务写 task_run_logs，手动点完在「执行记录」里什么都看不到。
// task_key 与定时任务一致，这样按任务名筛选时手动和定时的能一起看到，
// 靠 trigger_by 区分。
func startManualRunLog(sc *store.Scoped, taskKey, summary string) (int64, time.Time) {
	start := time.Now()
	var name string
	_ = sc.QueryRow(`SELECT name FROM scheduled_tasks WHERE tenant_id = ? AND task_key=?`, taskKey).Scan(&name)
	if name == "" {
		name = summary
	}
	res, err := sc.Insert(`INSERT INTO task_run_logs (tenant_id,task_key,name,status,trigger_by,started_at,finished_at)
		VALUES (?,?,?,?,'manual',?,?)`, taskKey, name, taskStatusRunning, start, start)
	if err != nil {
		logx.J("host", "manual_run_log_fail", map[string]any{
			"task": taskKey, "err": err.Error(), "warn": "手动同步的执行记录没写进去，界面上会看不到这次执行",
		})
		return 0, start
	}
	id, _ := res.LastInsertId()
	return id, start
}

// finishManualRunLog 收尾：把 running 记录更新成终态。
func finishManualRunLog(sc *store.Scoped, runID int64, start time.Time, err error, summary string) {
	if runID == 0 {
		return
	}
	// ⚠️ 必须和定时任务路径用同一套值。这里原来写的是 "success"/"failed"，
	// 两个都不在前端的枚举里，于是手动触发的记录在「执行记录」页
	// 筛「✅ 成功」时一条都看不到（CMDB-20260806-002）。
	status := taskStatusOK
	if err != nil {
		status, summary = taskStatusFail, err.Error()
	}
	if _, e := sc.Exec(`UPDATE task_run_logs SET status=?, summary=?, duration_ms=?, progress='', finished_at=NOW() WHERE tenant_id = ? AND id=?`,
		status, truncate(summary, 250), int(time.Since(start).Milliseconds()), runID); e != nil {
		logx.J("host", "manual_run_log_finish_fail", map[string]any{
			"run_id": runID, "err": e.Error(), "warn": "执行记录停在 running 状态，界面上会一直显示运行中",
		})
	}
}

// projectSyncResult 单个 project 的同步结果。
//
//	⚠️ 用 struct 而不是并列返回值，是**为了让新增字段不需要改调用点签名**。
//	上一版给同步结果加「本次新增已销毁」时，只有 1/3 的入口接上了——
//	另外两处是 `_, synced, stale, err :=`，接住新值要改签名，
//	比替换一行字符串麻烦，于是就被跳过了。结果同一件事在定时任务那条路径
//	说得清清楚楚，在**人最常用的手动同步**入口还是老样子（CMDB-20260806-001）。
type projectSyncResult struct {
	Name      string
	Synced    int // 云上还在的台数
	Gone      int // 累计已销毁（云上查不到）
	NewlyGone int // 其中本次新判定的
}

// syncOneProject 同步单个 project 行：解密凭据 → 列实例 → 逐台 upsert(报进度) → 标 stale → 网络资源。
// st 非空时边跑边更新进度（后台异步用）；scheduler 全量同步传 nil。
func (h *HostHandler) syncOneProject(sc *store.Scoped, pid int64, st *hostSyncState) (res projectSyncResult, err error) {
	var accountID int
	var provider, projectID, enc string
	err = sc.QueryRow(`SELECT p.name, p.account_id, p.project_id, COALESCE(p.cred_enc,''), COALESCE(a.provider,'gcp')
		FROM cloud_account_projects p LEFT JOIN cloud_accounts a ON a.id=p.account_id WHERE p.tenant_id = ? AND p.id=?`, pid).
		Scan(&res.Name, &accountID, &projectID, &enc, &provider)
	if err != nil {
		return res, fmt.Errorf("项目不存在")
	}
	if enc == "" {
		return res, fmt.Errorf("该项目还没配置 service account 凭据")
	}
	credJSON, e := h.Cipher.Decrypt(enc)
	if e != nil {
		return res, fmt.Errorf("凭据解密失败")
	}
	adapter, e := cloudsource.NewAdapter(provider, credJSON)
	if e != nil {
		return res, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute) // 上千台也够
	defer cancel()
	insts, e := adapter.ListInstances(ctx, projectID)
	if e != nil {
		_, _ = sc.Exec(`UPDATE cloud_account_projects SET last_sync_at=NOW(), last_result=? WHERE tenant_id = ? AND id=?`, truncate(e.Error(), 250), pid)
		return res, e
	}
	hsSet(st, func(s *hostSyncState) { s.Total += len(insts) })
	h.hsPersist(st, true) // total 从 0 变成真实值，是界面文案切换的依据，不能等节流
	present := map[string]bool{}
	for _, in := range insts {
		present[in.InstanceID] = true
		h.upsertHost(sc, accountID, res.Name, in)
		hsSet(st, func(s *hostSyncState) { s.Done++ })
		h.hsPersist(st, false) // 按秒节流，见 hsPersist
	}
	res.NewlyGone, res.Gone = h.markStaleHosts(sc, accountID, projectID, present)
	res.Synced = len(insts)
	// 顺带同步该 project 的网络资源（VPC/子网/防火墙/静态IP/负载均衡）
	//
	//	⚠️ 这里原来是 `if e == nil { ... }`，**没有 else 分支**——
	//	拉网络资源失败就静默跳过：不打日志、不进 last_result，
	//	而主机同步照样弹「同步完成：65 台」。
	//	用户点了同步、看到成功提示，然后发现负载均衡还是旧数据，
	//	完全不知道发生了什么。这类"一半成功也报全成功"的写法最难查。
	netErr := ""
	if nr, e := adapter.ListNetwork(ctx, projectID); e == nil {
		SyncProjectNetwork(h.DB, provider, accountID, projectID, nr)
	} else {
		netErr = e.Error()
		logx.Line("host_sync", fmt.Sprintf(
			"WARN project=%s 网络资源（VPC/防火墙/负载均衡）同步失败，本次这些数据保持不变：%v", projectID, e))
	}
	// IAM 与 Cloud DNS 是 GCP 特有的，用类型断言而非往 Adapter 接口里加方法——
	// 其它 provider 的对应概念不同，塞进同一个接口会逼它们实现空方法。
	if g, ok := adapter.(*cloudsource.GCP); ok {
		bindings, ierr := g.ListIAM(ctx, projectID)
		zones, derr := g.ListDNS(ctx, projectID)
		// 任一成功就写库；两个都失败时不动库，避免把上一次采到的好数据删成空
		if ierr == nil || derr == nil {
			SyncProjectIAMDNS(h.DB, accountID, projectID, bindings, zones)
		}
	}
	hsSet(st, func(s *hostSyncState) { s.Synced += len(insts); s.Stale += res.Gone })
	// 结果里必须带上网络资源那一步的成败——只报主机数会让人以为全同步好了
	//
	//	⚠️ 文案两处改动（同一个问题的两面）：
	//	  「失效」→「已销毁」：和主机页的口径统一。stale=1 的含义就是"云上已查不到"，
	//	    主机页已经显示成「已销毁」，同步结果这里还叫「失效」，看的人会以为是两回事，
	//	    甚至以为同步本身出了问题。
	//	  只报累计数 → 累计 + 本次新增：累计数每轮同步都一样（15、15、15…），
	//	    既吓人又没有信息量；真正代表云上发生了变化的是**本次新增**。
	result := hostSyncSummary(res.Synced, res.Gone, res.NewlyGone)
	if netErr != "" {
		result += "；⚠️ 网络资源（VPC/防火墙/负载均衡）未同步：" + truncate(netErr, 120)
	}
	_, _ = sc.Exec(`UPDATE cloud_account_projects SET last_sync_at=NOW(), last_result=? WHERE tenant_id = ? AND id=?`,
		truncate(result, 250), pid)
	return res, nil
}

// SyncAllHostProjects 供定时任务(host_sync)调用：同步所有账号所有 project，返回摘要+失败明细+是否成功。
func SyncAllHostProjects(st *store.Store, db *sql.DB, cipher *crypto.Cipher) (string, []TaskFailure, bool) {
	h := &HostHandler{Store: st, DB: db, Cipher: cipher, Mu: cluster.NewMutex(st)}
	ctx := context.Background()
	// 逐租户枚举云项目 —— 见 store.ForEachTenant。
	//
	//	⚠️ 原来这里用 Platform() 查 cloud_account_projects（租户表），
	//	查询直接被拒，主机同步从多租户改造以后一次都没成功过。
	type job struct {
		tenant store.TenantID
		pid    int64
	}
	var pids []job
	tenantCount := store.CountActiveTenants(ctx, st, "host_sync")
	failedTenants := 0
	if err := func() error {
		var err error
		failedTenants, err = store.ForEachTenant(ctx, st, "host_sync", func(sc *store.Scoped, t store.TenantID) error {
			rows, err := sc.Query(`SELECT id FROM cloud_account_projects WHERE tenant_id = ? ORDER BY id`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				j := job{tenant: t}
				if rows.Scan(&j.pid) == nil {
					pids = append(pids, j)
				}
			}
			return nil
		})
		return err
	}(); err != nil {
		return "查询云项目失败: " + err.Error(), nil, false
	}
	// 全部租户都失败 ≠ 没配云项目。后者是正常状态，前者是故障
	if store.AllFailed(failedTenants, tenantCount) {
		return fmt.Sprintf("全部 %d 个租户都取不到云项目，本轮没有同步任何主机（详见日志 tag=store）", tenantCount),
			[]TaskFailure{{Target: "所有租户", Reason: "取云项目失败"}}, false
	}
	if len(pids) == 0 {
		return "没有配置云项目，跳过", nil, true
	}
	totalSynced, totalStale, totalNewlyGone := 0, 0, 0
	var failures []TaskFailure
	for _, j := range pids {
		sc, err := st.Tenant(store.ForJob(ctx, j.tenant, "host_sync"))
		if err != nil {
			failures = append(failures, TaskFailure{Target: fmt.Sprintf("tenant=%d project=%d", j.tenant, j.pid),
				Reason: "构造租户上下文失败：" + err.Error()})
			continue
		}
		r, err := h.syncOneProject(sc, j.pid, nil)
		if err != nil {
			failures = append(failures, TaskFailure{Target: r.Name, Reason: truncate(err.Error(), 150)})
			continue
		}
		totalSynced += r.Synced
		totalStale += r.Gone
		totalNewlyGone += r.NewlyGone
	}
	ok := !(totalSynced == 0 && len(failures) == len(pids)) // 全部项目都失败才算失败
	msg := hostSyncSummary(totalSynced, totalStale, totalNewlyGone) + fmt.Sprintf("（%d 个项目）", len(pids))
	if len(failures) > 0 {
		msg += fmt.Sprintf("，%d/%d 个项目失败", len(failures), len(pids))
	}
	return msg, failures, ok
}

// gkeNodeSuffix GKE 节点名的固定尾巴：`-<8位hex>-<4位字母数字>`。
//
// GKE 建节点时名字是 `gke-<集群>-<节点池>-<8位hex>-<4位随机>`，
// 后两段由 GKE 自己生成，形状固定。
var gkeNodeSuffix = regexp.MustCompile(`-[0-9a-f]{8}-[0-9a-z]{4}$`)

// detectGKENode 判断 GCE 实例是否 GKE 节点，并取节点池名。
//
// 🔴 不能只看 `gke-` 前缀。
//
//	名字以 gke- 开头的**普通虚机**是真实存在的，实测 infra 项目里有三台：
//	    gke-infra-ansible-01
//	    gke-infra-ansible-gitlab-01
//	    gke-infra-kubernetes-master-01
//	它们是 ansible / gitlab / master 机器，不是任何集群的节点。
//
//	代价不止是"集群列空着"：按 nodeLinkState 的三态约定，
//	is_k8s_node=1 而 k8s_nodes 里查不到 → 判成 `not_ingested`，
//	界面写「该集群未接入」——把人指向一个**根本不存在的采集缺口**。
//	一个会稳定产生假警报的判定，比没有这个判定更坏。
//
//	判据用两条，任一成立即可：
//	  ① goog-gke* 标签 —— 权威，GKE 自己打的
//	  ② 名字**同时**满足 gke- 前缀和 `-<8位hex>-<4位>` 尾巴 ——
//	     那两段是 GKE 生成的，人不会这么给虚机命名
//
//	⚠️ 标签这条不能去掉：SQL 那边的注释里写过，GCP 实例不一定带
//	   goog-gke-node-pool-name，所以标签不能当唯一判据。
//	⚠️ 名字这条也不能去掉：没标签的节点只剩它。两条是互补不是二选一。
func detectGKENode(name string, labels map[string]string) (bool, string) {
	isNode := strings.HasPrefix(name, "gke-") && gkeNodeSuffix.MatchString(name)
	pool := ""
	for k, v := range labels {
		if strings.HasPrefix(k, "goog-gke") {
			isNode = true
		}
		if k == "goog-gke-node-pool-name" || k == "cloud.google.com/gke-nodepool" {
			pool = v
		}
	}
	return isNode, pool
}

func (h *HostHandler) upsertHost(sc *store.Scoped, accountID int, projName string, in cloudsource.Instance) {
	total := 0
	for _, d := range in.Disks {
		total += d.SizeGB
	}
	labelsJSON, _ := json.Marshal(in.Labels)
	var created any
	if in.CreatedAt != nil {
		created = in.CreatedAt.Format("2006-01-02 15:04:05")
	}
	// 按 (账号, instance_id) 找已有主机
	var ciID int64
	err := sc.QueryRow(`SELECT ci_id FROM hosts WHERE tenant_id = ? AND cloud_account_id=? AND instance_id=?`, accountID, in.InstanceID).Scan(&ciID)
	if err == sql.ErrNoRows {
		// ALLOW_DUP：云主机的身份是 (cloud_account_id, instance_id)，**不是 name**
		// —— 上面那行就是按这两个字段查的。主机可以改名，也可以跨账号重名，
		// 按 name 挡会让"改了名的同一台机器"被当成新机器、"两个账号下的同名机器"丢掉一台。
		res, e := sc.Insert(`INSERT INTO cis (tenant_id, type, name, status) VALUES (?, 'host', ?, 'active')`, in.Name)
		if e != nil {
			return
		}
		ciID, _ = res.LastInsertId()
		if _, e := sc.Insert(`INSERT INTO hosts (tenant_id, ci_id, instance_id, cloud_account_id, provider) VALUES (?, ?, ?, ?, ?)`,
			ciID, in.InstanceID, accountID, "gcp"); e != nil {
			return
		}
	} else if err != nil {
		return
	} else if _, e := sc.Exec(`UPDATE cis SET name=? WHERE tenant_id = ? AND id=?`, in.Name, ciID); e != nil {
		return
	}
	preempt, delProt := 0, 0
	if in.Preemptible {
		preempt = 1
	}
	if in.DeletionProtection {
		delProt = 1
	}
	isK8s, pool := detectGKENode(in.Name, in.Labels)
	logExec(h.DB, "主机同步写", `UPDATE hosts SET project=?, project_name=?, zone=?, region=?, machine_type=?, vcpu=?, mem_mb=?, disk_total_gb=?,
		internal_ip=?, external_ip=?, status=?, os=?, labels=?, self_link=?, gcp_created_at=?,
		hostname=?, vpc=?, subnet=?, network_tags=?, preemptible=?, image=?, cpu_platform=?, deletion_protection=?, service_accounts=?,
		is_k8s_node=?, k8s_pool=?, stale=0, synced_at=NOW() WHERE ci_id=?`,
		in.Project, projName, in.Zone, in.Region, in.MachineType, in.VCPU, in.MemMB, total,
		in.InternalIP, in.ExternalIP, in.Status, in.OS, string(labelsJSON), in.SelfLink, created,
		in.Hostname, in.VPC, in.Subnet, strings.Join(in.NetworkTags, ","), preempt, in.Image, in.CPUPlatform, delProt, strings.Join(in.ServiceAccounts, ","),
		boolToInt(isK8s), pool, ciID)
	// 磁盘：全删重插
	logExec(h.DB, "主机同步写", `DELETE FROM host_disks WHERE host_ci_id=?`, ciID)
	for _, d := range in.Disks {
		boot := 0
		if d.IsBoot {
			boot = 1
		}
		logExec(h.DB, "主机同步写", `INSERT INTO host_disks (host_ci_id, name, size_gb, type, is_boot) VALUES (?, ?, ?, ?, ?)`,
			ciID, d.Name, d.SizeGB, d.Type, boot)
	}
}

// hostSyncSummary 同步结果文案。四处（项目级/账号级/批量/定时任务）共用一份，
// 免得同一件事在不同入口有不同说法。
//
//	## 措辞是这条的重点，不是格式
//
//	原来写的是「同步 25 台，失效 15」，两个词都在误导：
//	  · 「失效」——stale=1 的真实含义是"这次同步时云上已经查不到它了"，
//	    主机页已经如实显示成「已销毁」。同一件事两个页面两种叫法，
//	    看的人会以为是两回事，甚至以为是同步本身出了问题（"怎么又失效了 15 个"）。
//	  · 数字是**累计**而不是本次新增。每一轮同步它都一模一样地出现 15、15、15，
//	    既吓人又没有信息量。真正值得注意的是本次新增：那才代表云上刚发生了变化。
func hostSyncSummary(live, gone, newlyGone int) string {
	s := fmt.Sprintf("同步 %d 台在用", live)
	if gone > 0 {
		s += fmt.Sprintf("；已销毁 %d 台", gone)
		if newlyGone > 0 {
			s += fmt.Sprintf("（本次新增 %d）", newlyGone)
		} else {
			s += "（本次无新增）"
		}
	}
	return s
}

// markStaleHosts 把该 (账号, project) 下 GCP 已无的实例标 stale（只影响这个 project，不碰其它 project）。
//
//	返回 (本次新判定为已销毁, 该 project 累计已销毁)。
//
//	⚠️ 这两个数必须分开。原来只返回一个数，而且是**每轮重新数一遍所有 !present 的行**，
//	于是同步结果永远写着「同步 25 台，失效 15」——看上去像"这次又坏了 15 台"，
//	实际是"库里有 15 台早就销毁了，这次仍然没看到"。它在每一轮同步里一模一样地出现，
//	既吓人又没有信息量。真正值得注意的是**本次新增**：那才代表云上刚刚发生了变化。
func (h *HostHandler) markStaleHosts(sc *store.Scoped, accountID int, projectID string, present map[string]bool) (newly, total int) {
	rows, err := h.DB.Query(`SELECT ci_id, instance_id FROM hosts WHERE cloud_account_id=? AND project=?`, accountID, projectID)
	if err != nil {
		logx.J("host_sync", "mark_stale_query_fail", map[string]any{
			"account": accountID, "project": projectID, "err": err.Error()})
		return 0, 0
	}
	type row struct {
		ciID int64
		inst string
	}
	var all []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.ciID, &r.inst) == nil {
			all = append(all, r)
		}
	}
	rows.Close()
	for _, r := range all {
		if present[r.inst] {
			continue
		}
		total++
		// 加 `AND stale=0`：受影响行数 > 0 才是**这一轮新销毁的**。
		// 不加的话每轮都会把同一批老记录重写一遍，本次新增永远等于累计。
		if n := logExec(h.DB, "主机同步写", `UPDATE hosts SET stale=1 WHERE ci_id=? AND stale=0`, r.ciID); n > 0 {
			newly++
		}
	}
	if newly > 0 {
		logx.J("host_sync", "hosts_gone", map[string]any{
			"account": accountID, "project": projectID, "newly_gone": newly, "total_gone": total,
			"note": "云上已查不到这些实例，判定为已销毁；30 天后由 stale_host_purge 清理",
		})
	}
	return newly, total
}

// 主机生命周期状态。和 status（云上原样值）是两个维度：
// 一台机器可以 lifecycle=gone 而 status=RUNNING——那正是"销毁前最后一次
// 看到它在运行"的意思，不是"它现在在运行"。
const (
	lifecyclePresent = "present" // 最近一次同步在云上见到了
	lifecycleGone    = "gone"    // 最近一次同步云上没有它（stale=1）
)

func lifecycleOf(stale bool) string {
	if stale {
		return lifecycleGone
	}
	return lifecyclePresent
}

// ---------- 主机台账（只读） ----------

type hostOut struct {
	CIID        int64  `json:"ci_id"`
	Name        string `json:"name"`
	Project     string `json:"project"`      // project id
	ProjectName string `json:"project_name"` // GCP 显示名
	Zone        string `json:"zone"`
	Region      string `json:"region"`
	MachineType string `json:"machine_type"`
	VCPU        int    `json:"vcpu"`
	MemMB       int    `json:"mem_mb"`
	DiskTotalGB int    `json:"disk_total_gb"`
	InternalIP  string `json:"internal_ip"`
	ExternalIP  string `json:"external_ip"`
	// Status 是**云上原样值**（RUNNING/TERMINATED/…），保持不动。
	//
	//	⚠️ 已销毁的机器不能靠改这个字段来表达。markStaleHosts 只置 stale=1，
	//	status 停在最后一次同步到的 RUNNING 上，于是界面同一行自相矛盾：
	//	名字划了删除线、打了「已删」标签，状态列却是绿色的「运行」（CMDB-003）。
	//	但也不能把 status 覆写成 DESTROYED——
	//	  1. TERMINATED 在 GCP 语义里是"已停机、实例还在、磁盘还计费"，
	//	     借用它会把"销毁"和"关机"混成一个值；
	//	  2. stale 是**推断**（同步时 GCP 没返回≠一定销毁，也可能同步本身坏了、
	//	     或实例被移出了这个 project）。覆写掉真值，一旦是误标就再也查不到
	//	     最后一次观测到的真实状态。
	//	所以另出一个派生字段 Lifecycle，展示层以它为准，status 留作证据。
	Status      string            `json:"status"`
	Lifecycle   string            `json:"lifecycle"` // present=云上还在 / gone=云上已查不到
	OS          string            `json:"os"`
	Labels      map[string]string `json:"labels"`
	AccountName string            `json:"account_name"`
	Provider    string            `json:"provider"`
	Stale       bool              `json:"stale"`
	IsK8sNode   bool              `json:"is_k8s_node"`
	K8sPool     string            `json:"k8s_pool"`
	// ClusterName 这台机器所属的 K8s 集群名（经 k8s_nodes 关联）。
	// ⚠️ 空串对非 K8s 节点是**正确**的值，不是"没采到"。
	ClusterName string `json:"cluster_name"`
	CreatedAt   string `json:"gcp_created_at"`
	// SyncedAt 最后一次同步到这台机器的时刻。
	//
	// 列表页必须能看到它：台账数据是**快照**，不是实时。
	// 不显示同步时间，用户会把三天前的快照当成当前状态 ——
	// 而"数据是旧的"和"数据是错的"在界面上长得一模一样。
	SyncedAt string `json:"synced_at,omitempty"`
	// GCP 只读技术字段
	Hostname           string   `json:"hostname"`
	VPC                string   `json:"vpc"`
	Subnet             string   `json:"subnet"`
	NetworkTags        []string `json:"network_tags"`
	Preemptible        bool     `json:"preemptible"`
	Image              string   `json:"image"`
	CPUPlatform        string   `json:"cpu_platform"`
	DeletionProtection bool     `json:"deletion_protection"`
	ServiceAccounts    []string `json:"service_accounts"`
	// Disks 磁盘逐块明细。
	//
	// 刻意不做成主机级的一个总用量：一台 boot 盘 96%、数据盘 71% 的机器，
	// 加权算下来才 79%，看着很健康，但系统盘马上要写满 ——
	// 而 kubelet 的 DiskPressure 恰恰是按单块盘判的。加总会把它藏起来。
	Disks []hostDiskOut `json:"disks"`

	// 成本估算（USD）
	CostDaily  float64 `json:"cost_daily"`
	CostMonth  float64 `json:"cost_month"`
	CostTotal  float64 `json:"cost_total"`
	CostSource string  `json:"cost_source"` // estimate / bigquery
}

// 前端语义的状态分桶。
//
// 云上的 status 是原样值（RUNNING/TERMINATED/…），而界面上人要选的是
// 「运行中 / 已停止 / 已销毁」。销毁与停机是两件事，绝不能合并：
// TERMINATED 在 GCP 语义里是"已停机、实例还在、磁盘还在计费"。
func hostBucket(stale bool, status string) string {
	if stale {
		return "destroyed"
	}
	if strings.EqualFold(status, "RUNNING") {
		return "running"
	}
	return "stopped"
}

// 排序白名单：前端字段名 → 比较函数。
//
// 不在表里的排序参数一律退回默认排序，绝不把参数拼进 SQL 或反射取字段。
var hostSorters = map[string]func(a, b hostOut) int{
	"name":    func(a, b hostOut) int { return strings.Compare(a.Name, b.Name) },
	"project": func(a, b hostOut) int { return strings.Compare(a.Project, b.Project) },
	"status":  func(a, b hostOut) int { return strings.Compare(a.Status, b.Status) },
	"vcpu":    func(a, b hostOut) int { return cmp.Compare(a.VCPU, b.VCPU) },
	"mem":     func(a, b hostOut) int { return cmp.Compare(a.MemMB, b.MemMB) },
	"disk":    func(a, b hostOut) int { return cmp.Compare(a.DiskTotalGB, b.DiskTotalGB) },
	"cost":    func(a, b hostOut) int { return cmp.Compare(a.CostMonth, b.CostMonth) },
	"created": func(a, b hostOut) int { return strings.Compare(a.CreatedAt, b.CreatedAt) },
}

// ListHosts 主机列表。
//
//	@Summary		主机列表
//	@Description	支持分页、关键词搜索、状态/项目/云厂商筛选与排序。
//	@Description	不传 page/size 时返回裸数组（兼容尚未迁移的旧前端）。
//	@Tags			hosts
//	@Produce		json
//	@Param			page		query		int		false	"页码，从 1 开始"
//	@Param			size		query		int		false	"每页条数，上限 200"	default(50)
//	@Param			q			query		string	false	"搜索主机名或内网 IP"
//	@Param			status		query		string	false	"状态"	Enums(running, stopped, destroyed)
//	@Param			project		query		string	false	"云项目 ID"
//	@Param			provider	query		string	false	"云厂商"
//	@Param			sort		query		string	false	"排序字段，前缀 - 为降序"	Enums(name, -name, project, -project, status, -status, vcpu, -vcpu, mem, -mem, disk, -disk, cost, -cost, created, -created)
//
//	降序形式必须逐个列出来：swag 的 Enums 没法表达"可选前缀"，
//	只列不带 - 的话，生成出的 TS 类型会比实际契约更严格 ——
//	前端传 -cost 编译不过，而后端明明支持。
//	@Success		200			{object}	httpx.ListResponse[handlers.hostOut]
//	@Failure		400			{object}	httpx.APIError
//	@Failure		500			{object}	httpx.APIError
//	@Router			/hosts [get]
//
// ⚠️ 实现说明：本接口**先取全量再在内存里分页**，因为成本是在应用层估算的
// （rateCache 要用区域+机型+磁盘现算），SQL 排不了序，facets 也要全量才准。
// 主机规模（数百台）下这完全可接受。
// **Pod / 事件这类万级数据的列表不要照抄这里**，那些必须走 SQL LIMIT。
func (h *HostHandler) ListHosts(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		httpx.Fail(c, httpx.CodeBadRequest, err, nil)
		return
	}
	q := httpx.BindPage(c, "status", "project", "provider")
	// 没传分页参数 = 旧前端。过渡期返回裸数组，等 enterprise/ 下线后删掉这个分支。
	legacy := c.Query("page") == "" && c.Query("size") == ""

	rc := newRateCache(sc)

	// 磁盘：一次全量取出按主机分组，避免 N+1。
	// costDisks 只喂给成本估算（它只认 type/size），outDisks 是对外的完整明细。
	costDisks := map[int64][]diskRow{}
	outDisks := map[int64][]hostDiskOut{}
	drows, _ := h.DB.Query(`SELECT host_ci_id, name, type, size_gb, is_boot, used_percent, used_at, mount_point
		FROM host_disks ORDER BY is_boot DESC, id`)
	if drows != nil {
		for drows.Next() {
			var ci int64
			var name, typ, mount string
			var sz, isBoot int
			var used sql.NullFloat64
			var usedAt sql.NullTime
			if drows.Scan(&ci, &name, &typ, &sz, &isBoot, &used, &usedAt, &mount) == nil {
				costDisks[ci] = append(costDisks[ci], diskRow{Type: typ, SizeGB: sz})
				d := hostDiskOut{Name: name, SizeGB: sz, Type: typ, IsBoot: isBoot == 1, MountPoint: mount}
				// NULL 保持 nil，不要退化成 0 —— 前端靠 null 区分「没采到」和「空盘」
				if used.Valid {
					v := used.Float64
					d.UsedPercent = &v
				}
				if usedAt.Valid {
					t := usedAt.Time
					d.UsedAt = &t
				}
				outDisks[ci] = append(outDisks[ci], d)
			}
		}
		drows.Close()
	}

	// 关键词下推到 SQL（能少扫就少扫）；维度筛选留到内存做，
	// 这样 facets 可以统计「不含维度筛选」的计数 —— 用户才知道切过去能看到多少台。
	w := &httpx.WhereBuilder{}
	w.Add("c.type='host'")
	if q.Keyword != "" {
		// 两个占位符各要一份参数，显式写出来，不靠 Like 帮忙复制
		kw := httpx.EscapeLike(q.Keyword)
		w.Add("(c.name LIKE ? OR h.internal_ip LIKE ?)", kw, kw)
	}
	args := w.Args()

	rows, err := h.DB.Query(`SELECT c.id, c.name, h.project, h.project_name, h.zone, h.region, h.machine_type, h.vcpu, h.mem_mb, h.disk_total_gb,
		h.internal_ip, h.external_ip, h.status, h.os, h.labels, h.stale, h.gcp_created_at, h.preemptible, h.provider, COALESCE(ca.name,''),
		h.is_k8s_node,
		-- 节点池：优先用云侧实例标签（h.k8s_pool），标签缺失时回退到 K8s 侧采到的值。
		-- 🔴 GCP 实例不一定带 goog-gke-node-pool-name 标签，于是 detectGKENode 取不到，
		-- 主机页「集群/节点池」列就全是 —— 而 k8s_nodes.pool 里明明有值
		-- （节点页的节点池筛选就是用它做的）。同一份信息在一张表里有、另一张表里没有，
		-- 界面却只读了没有的那张。
		COALESCE(NULLIF(h.k8s_pool,''), NULLIF(kn.pool,''), '') AS k8s_pool,
		-- 🔴 真正的集群名。
		--
		--	此前 /api/hosts 压根没有这个字段，于是前端把 GCP **项目**映射进了
		--	cluster 字段（routes/hosts/queries.ts 里写的是 cluster: raw.project），
		--	界面上「集群 / 节点池」列的左半边显示的是项目名。
		--	⚠️ 这段注释在 Go 的**反引号原始字符串**里，不能出现反引号 ——
		--	写一个反引号就会把 SQL 字符串提前截断（已经栽过一次）。
		--	项目和集群是两个维度：一个项目里可以有多个集群，一个集群也可能跨项目。
		--	这一列不报错、不为空，只是在回答另一个问题（OPSCMDB-037）。
		--
		--	⚠️ 非 K8s 节点这里是空串，那是**正确**的空：它确实不属于任何集群。
		--	前端必须把"不属于集群"和"没采到"分开显示。
		COALESCE(kc.name,'') AS cluster_name,
		h.synced_at
		FROM cis c JOIN hosts h ON h.ci_id=c.id
		LEFT JOIN cloud_accounts ca ON ca.id=h.cloud_account_id
		-- 先按内网 IP 关联，再按名字兜底：k3s 这类改过 --node-name 的集群只能靠 IP，
		-- 而 GKE 节点的机器名与节点名一致，两条都留着覆盖面最大
		LEFT JOIN k8s_nodes kn ON (kn.internal_ip = h.internal_ip AND h.internal_ip <> '')
			OR (kn.name = c.name)
		LEFT JOIN k8s_clusters kc ON kc.id = kn.cluster_id`+
		w.SQL()+` ORDER BY h.project, c.name`, args...)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, err, nil)
		return
	}
	defer rows.Close()

	all := []hostOut{}
	now := time.Now()
	for rows.Next() {
		var o hostOut
		var labels sql.NullString
		var created sql.NullTime
		var stale, preempt, isK8s int
		var synced sql.NullTime
		if err := rows.Scan(&o.CIID, &o.Name, &o.Project, &o.ProjectName, &o.Zone, &o.Region, &o.MachineType, &o.VCPU, &o.MemMB, &o.DiskTotalGB,
			&o.InternalIP, &o.ExternalIP, &o.Status, &o.OS, &labels, &stale, &created, &preempt, &o.Provider, &o.AccountName, &isK8s, &o.K8sPool, &o.ClusterName, &synced); err != nil {
			httpx.Fail(c, httpx.CodeInternal, err, nil)
			return
		}
		o.Stale = stale == 1
		o.Lifecycle = lifecycleOf(o.Stale)
		if synced.Valid {
			o.SyncedAt = synced.Time.Format(time.RFC3339)
		}
		o.IsK8sNode = isK8s == 1
		o.Preemptible = preempt == 1
		if labels.Valid && labels.String != "" {
			_ = json.Unmarshal([]byte(labels.String), &o.Labels)
		}
		o.Disks = outDisks[o.CIID]
		if o.Disks == nil {
			// nil 切片会序列化成 null，前端 .map 直接炸；空数组表示「确认没有盘」
			o.Disks = []hostDiskOut{}
		}
		hourly, _, _, _ := rc.hostHourly(o.Region, familyOf(o.MachineType), o.VCPU, o.MemMB, o.Status, costDisks[o.CIID])
		o.CostDaily = round2(hourly * 24)
		o.CostMonth = round2(hourly * 730)
		o.CostSource = "estimate"
		if created.Valid {
			o.CreatedAt = created.Time.Format("2006-01-02")
			o.CostTotal = round2(hourly * now.Sub(created.Time).Hours())
		}
		all = append(all, o)
	}

	if legacy {
		// 🔴 这个分支返回的是**裸数组**，与分页分支的 {items,total,facets} 形状不同。
		//
		// 分叉本身是有意的（见上面 legacy 的定义），危险的是它**不可见**：
		// 新前端只要漏传一次 page，拿到数组去读 .items 就是整页崩，
		// 而后端日志里什么都不会有——排查的人只能从前端崩溃栈倒推。
		// 所以这里留两个记号：响应头给抓包的人看，WARN 给看日志的人看。
		// enterprise/ 下线后连同 legacy 判据一起删。
		c.Header("X-Legacy-Shape", "array")
		log.Printf("WARN ListHosts 走了 legacy 裸数组分支（未传 page/size）: ua=%q referer=%q req_id=%s",
			c.Request.UserAgent(), c.Request.Referer(), c.GetHeader("X-Request-Id"))
		c.JSON(http.StatusOK, all)
		return
	}

	items, total, facets := hostPage(all, q)
	// 过期判据来自 host_sync 任务自己的 cron —— 前端不写死阈值。
	// 见 httpx.Freshness 的说明（OPSCMDB-031 P1-5）。
	win, known := scheduleStaleWindow(h.DB, "host_sync")
	fresh := &httpx.Freshness{
		TaskKey:           "host_sync",
		StaleAfterSeconds: int64(win.Seconds()),
		Known:             known,
	}
	c.JSON(http.StatusOK, httpx.NewList(items, q, total).WithFacets(facets).WithFreshness(fresh))
}

// hostPage 对已取回的全量主机做 facets 统计、筛选、排序、切片。
//
// 抽成纯函数是为了能脱离数据库测试。分页边界（越界页码、最后一页不足一页）
// 和 facets 的统计口径恰恰是最容易写错、又最难在集成环境里发现的部分 ——
// 它们出错时不报错，只是数字不对，而没人会去手工核对一个看起来正常的数字。
func hostPage(all []hostOut, q httpx.PageQuery) ([]hostOut, int64, map[string]map[string]int64) {
	// facets 按「不含维度筛选」的结果集统计。
	// 若按当前筛选后的结果统计，选了 status=running 之后其他状态全是 0，
	// 用户就看不出"切到已销毁能看到 53 台"——而那正是下拉里数字的意义。
	facets := map[string]map[string]int64{
		"status":   {"running": 0, "stopped": 0, "destroyed": 0},
		"project":  {},
		"provider": {},
	}
	for _, o := range all {
		facets["status"][hostBucket(o.Stale, o.Status)]++
		if o.Project != "" {
			facets["project"][o.Project]++
		}
		if o.Provider != "" {
			facets["provider"][o.Provider]++
		}
	}

	filtered := make([]hostOut, 0, len(all))
	for _, o := range all {
		if v, ok := q.Filters["status"]; ok && hostBucket(o.Stale, o.Status) != v {
			continue
		}
		if v, ok := q.Filters["project"]; ok && o.Project != v {
			continue
		}
		if v, ok := q.Filters["provider"]; ok && o.Provider != v {
			continue
		}
		filtered = append(filtered, o)
	}

	if less, ok := hostSorters[q.SortBy]; ok {
		slices.SortStableFunc(filtered, func(a, b hostOut) int {
			if q.SortDesc {
				return less(b, a)
			}
			return less(a, b)
		})
	}

	total := int64(len(filtered))
	// 越界的页码返回空列表，而不是静默退回最后一页：
	// 退回最后一页会让前端的"下一页"按钮看起来永远可点。
	lo := min(q.Offset(), len(filtered))
	hi := min(lo+q.Size, len(filtered))
	return filtered[lo:hi], total, facets
}

// HostDetail 主机详情。
//
//	@Summary		主机详情
//	@Description	磁盘逐块（含用量与挂载点）、关联业务域名、成本明细。
//	@Tags			hosts
//	@Produce		json
//	@Param			ciid	path		int		true	"CI ID"
//	@Param			as_of	query		string	false	"累计成本算到哪一天，YYYY-MM-DD"
//	@Success		200		{object}	handlers.hostDetailOut
//	@Failure		404		{object}	httpx.APIError
//	@Router			/hosts/{ciid} [get]
func (h *HostHandler) HostDetail(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ciid := c.Param("ciid")
	var o hostOut
	var labels sql.NullString
	var created sql.NullTime
	var stale, preempt, delProt int
	var tags, sas string
	var syncedAt sql.NullTime
	var isK8sNode int
	err = sc.QueryRow(`SELECT c.id, c.name, h.project, h.project_name, h.zone, h.region, h.machine_type, h.vcpu, h.mem_mb, h.disk_total_gb,
		h.internal_ip, h.external_ip, h.status, h.os, h.labels, h.stale, h.gcp_created_at, COALESCE(ca.name,''),
		h.hostname, h.vpc, h.subnet, h.network_tags, h.preemptible, h.image, h.cpu_platform, h.deletion_protection, h.service_accounts,
		h.synced_at, h.is_k8s_node
		FROM cis c JOIN hosts h ON h.ci_id=c.id LEFT JOIN cloud_accounts ca ON ca.id=h.cloud_account_id
		WHERE c.tenant_id = ? AND c.id=? AND c.type='host'`, ciid).
		Scan(&o.CIID, &o.Name, &o.Project, &o.ProjectName, &o.Zone, &o.Region, &o.MachineType, &o.VCPU, &o.MemMB, &o.DiskTotalGB,
			&o.InternalIP, &o.ExternalIP, &o.Status, &o.OS, &labels, &stale, &created, &o.AccountName,
			&o.Hostname, &o.VPC, &o.Subnet, &tags, &preempt, &o.Image, &o.CPUPlatform, &delProt, &sas, &syncedAt, &isK8sNode)
	if err != nil {
		httpx.NotFound(c, "host")
		return
	}
	o.Stale = stale == 1
	o.Lifecycle = lifecycleOf(o.Stale)
	// 详情页同样要显示数据有多旧 —— 列表有而详情没有，
	// 会让人以为详情是实时查的，而它同样是快照
	if syncedAt.Valid {
		o.SyncedAt = syncedAt.Time.Format(time.RFC3339)
	}
	o.Preemptible = preempt == 1
	o.DeletionProtection = delProt == 1
	o.NetworkTags = splitNonEmpty(tags)
	o.ServiceAccounts = splitNonEmpty(sas)
	if labels.Valid && labels.String != "" {
		_ = json.Unmarshal([]byte(labels.String), &o.Labels)
	}
	// 磁盘。
	//
	// ⚠️ 复用列表页的 hostDiskOut，不要在这里另定义一个。
	// 原先这里有个自己的 disk 结构，少了 used_percent 和 mount_point ——
	// 结果**详情页比列表页信息还少**，而详情页恰恰是用来看细节的。
	// 两个结构分开维护，加字段时必然只改一处。
	disks := []hostDiskOut{}
	drows, _ := h.DB.Query(`SELECT name, size_gb, type, is_boot, used_percent, used_at, mount_point
		FROM host_disks WHERE host_ci_id=? ORDER BY is_boot DESC, id`, ciid)
	if drows != nil {
		for drows.Next() {
			var d hostDiskOut
			var boot int
			var used sql.NullFloat64
			var usedAt sql.NullTime
			if drows.Scan(&d.Name, &d.SizeGB, &d.Type, &boot, &used, &usedAt, &d.MountPoint) == nil {
				d.IsBoot = boot == 1
				// NULL 保持 nil —— 前端靠它区分「没采到」和「空盘」
				if used.Valid {
					v := used.Float64
					d.UsedPercent = &v
				}
				if usedAt.Valid {
					t := usedAt.Time
					d.UsedAt = &t
				}
				disks = append(disks, d)
			}
		}
		drows.Close()
	}
	// 关联业务域名（源站IP 命中 内网/外网 IP）
	related := []relatedDomain{}
	ips := []string{}
	if o.InternalIP != "" {
		ips = append(ips, o.InternalIP)
	}
	if o.ExternalIP != "" {
		ips = append(ips, o.ExternalIP)
	}
	for _, ip := range ips {
		rrows, _ := h.DB.Query(`SELECT r.host, c.name FROM domain_records r JOIN cis c ON c.id=r.domain_ci_id
			WHERE FIND_IN_SET(?, REPLACE(r.origin_ip,' ','')) > 0`, ip)
		if rrows != nil {
			for rrows.Next() {
				var host, domain string
				if rrows.Scan(&host, &domain) == nil {
					related = append(related, relatedDomain{FQDN: recordFQDN(host, domain), IP: ip})
				}
			}
			rrows.Close()
		}
	}
	// 成本（按 区域×机型族 + 区域×磁盘类型 分档）
	rc := newRateCache(sc)
	var drs []diskRow
	for _, d := range disks {
		drs = append(drs, diskRow{Type: d.Type, SizeGB: d.SizeGB})
	}
	family := familyOf(o.MachineType)
	hourly, vcpuHour, ramGbHour, matched := rc.hostHourly(o.Region, family, o.VCPU, o.MemMB, o.Status, drs)
	o.CostDaily = round2(hourly * 24)
	o.CostMonth = round2(hourly * 730)
	o.CostSource = "estimate"
	asOf := time.Now()
	if v := c.Query("as_of"); v != "" {
		if t, e := time.Parse("2006-01-02", v); e == nil {
			asOf = t.Add(24 * time.Hour)
		}
	}
	if created.Valid {
		o.CreatedAt = created.Time.Format("2006-01-02")
		if asOf.After(created.Time) {
			o.CostTotal = round2(hourly * asOf.Sub(created.Time).Hours())
		}
	}
	// 这台机器上跑着什么。
	//
	// 先按内网 IP 找节点，名字兜底。IP 优先是因为**节点名不一定等于实例名**：
	// k3s 默认用 hostname，也有人改 --node-name；而内网 IP 是同一台机器的唯一事实。
	// 同 IP 命中多行（不同 VPC 复用网段）时按 cluster_id 取最小的，
	// 至少保证同一台机器每次打开看到的是同一个集群，而不是随机换。
	var node *hostNodeOut
	pods := []hostPodOut{}

	var n hostNodeOut
	const nodeSel = `SELECT n.cluster_id, COALESCE(cl.name,''), n.name, n.pool, n.ready_status, n.pod_count
		FROM k8s_nodes n LEFT JOIN k8s_clusters cl ON cl.id=n.cluster_id
		WHERE %s ORDER BY n.cluster_id LIMIT 1`
	scanNode := func(where string, arg string) bool {
		if arg == "" {
			return false
		}
		return h.DB.QueryRow(fmt.Sprintf(nodeSel, where), arg).
			Scan(&n.ClusterID, &n.ClusterName, &n.Name, &n.Pool, &n.ReadyStatus, &n.PodCount) == nil
	}
	found := scanNode("n.internal_ip=?", o.InternalIP) || scanNode("n.name=?", o.Name)
	nodeLink := nodeLinkState(found, isK8sNode == 1, o.Stale)
	if found {
		node = &n
		// 按严重度排，不按字母序：排障时人是来找"哪个不对劲"的，
		// 字母序会把唯一起不来的那个埋在第 40 行。
		//
		//	0  Failed / 以及任何不认识的 phase —— 不认识不等于正常
		//	1  Pending      起不来，最需要人管
		//	2  Running 但重启过  在跑，但跑得不安稳
		//	3  Running / Succeeded  正常（Succeeded 是 Job 的正常终态，
		//	                        排进异常组会让每个有定时任务的节点都像出了事）
		prows, _ := h.DB.Query(`SELECT namespace, name, COALESCE(workload,''), phase, restarts
			FROM k8s_pods WHERE cluster_id=? AND node_name=?
			ORDER BY CASE phase
				WHEN 'Running'   THEN (CASE WHEN restarts > 0 THEN 2 ELSE 3 END)
				WHEN 'Succeeded' THEN 3
				WHEN 'Pending'   THEN 1
				ELSE 0
			END, namespace, name`, n.ClusterID, n.Name)
		if prows != nil {
			for prows.Next() {
				var p hostPodOut
				if prows.Scan(&p.Namespace, &p.Name, &p.Workload, &p.Phase, &p.Restarts) == nil {
					pods = append(pods, p)
				}
			}
			prows.Close()
		}
	}

	c.JSON(http.StatusOK, hostDetailOut{
		Host: o, Disks: disks, RelatedDomains: related,
		CostHourly: round4(hourly), AsOf: asOf.Format("2006-01-02"),
		RateMatched: matched, RateVCPUHour: round4(vcpuHour), RateRAMGbHour: round4(ramGbHour),
		RateFamily: family,
		NodeLink:   nodeLink, Node: node, Pods: pods,
	})
}

// splitNonEmpty 逗号分隔转 []string，去空；空串返回空切片（前端渲染友好）。
func splitNonEmpty(s string) []string {
	out := []string{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }
func round4(f float64) float64 { return float64(int64(f*10000+0.5)) / 10000 }

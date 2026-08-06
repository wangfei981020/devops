package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/cloudsource"
	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/logx"
)

// HostHandler 云主机（一期只读）：云账号管理 + GCP 同步 + 主机台账 + 成本估算 + 域名关联。
type HostHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewHostHandler(db *sql.DB, cipher *crypto.Cipher) *HostHandler {
	return &HostHandler{DB: db, Cipher: cipher}
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

func (h *HostHandler) loadRates() *rateCache { return newRateCache(h.DB) }

// newRateCache 从 DB 加载全部费率（供主机模块与 K8s 成本模块复用）。
func newRateCache(db *sql.DB) *rateCache {
	rc := &rateCache{compute: map[string][2]float64{}, disk: map[string]float64{}}
	if rows, _ := db.Query(`SELECT region, machine_family, vcpu_hour_usd, ram_gb_hour_usd FROM cloud_compute_rates WHERE provider='gcp'`); rows != nil {
		for rows.Next() {
			var region, family string
			var v, r float64
			if rows.Scan(&region, &family, &v, &r) == nil {
				rc.compute[region+"|"+family] = [2]float64{v, r}
			}
		}
		rows.Close()
	}
	if rows, _ := db.Query(`SELECT region, disk_type, gb_month_usd FROM cloud_disk_rates WHERE provider='gcp'`); rows != nil {
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
	rows, err := h.DB.Query(`SELECT id, region, machine_family, vcpu_hour_usd, ram_gb_hour_usd, note FROM cloud_compute_rates WHERE provider='gcp' ORDER BY region, machine_family`)
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
	var in struct {
		Region    string  `json:"region"`
		Family    string  `json:"machine_family"`
		VcpuHour  float64 `json:"vcpu_hour_usd"`
		RamGbHour float64 `json:"ram_gb_hour_usd"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Region == "" || in.Family == "" {
		c.JSON(400, gin.H{"error": "区域和机型族必填"})
		return
	}
	if _, err := h.DB.Exec(`INSERT INTO cloud_compute_rates (provider,region,machine_family,vcpu_hour_usd,ram_gb_hour_usd,note) VALUES ('gcp',?,?,?,?,'manual')`,
		in.Region, in.Family, in.VcpuHour, in.RamGbHour); err != nil {
		c.JSON(500, gin.H{"error": "该区域+机型族已存在或写入失败"})
		return
	}
	c.JSON(201, gin.H{"ok": true})
}

func (h *HostHandler) UpdateComputeRate(c *gin.Context) {
	var in struct {
		VcpuHour  float64 `json:"vcpu_hour_usd"`
		RamGbHour float64 `json:"ram_gb_hour_usd"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 人工改过的标 confirmed（不再是 estimate 待核对）
	if _, err := h.DB.Exec(`UPDATE cloud_compute_rates SET vcpu_hour_usd=?, ram_gb_hour_usd=?, note='confirmed' WHERE id=?`,
		in.VcpuHour, in.RamGbHour, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *HostHandler) DeleteComputeRate(c *gin.Context) {
	logExec(h.DB, "主机同步写", `DELETE FROM cloud_compute_rates WHERE id=?`, c.Param("id"))
	c.JSON(200, gin.H{"ok": true})
}

// ---- 磁盘费率 CRUD ----

func (h *HostHandler) ListDiskRates(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, region, disk_type, gb_month_usd, note FROM cloud_disk_rates WHERE provider='gcp' ORDER BY region, disk_type`)
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
	var in struct {
		Region   string  `json:"region"`
		DiskType string  `json:"disk_type"`
		GbMonth  float64 `json:"gb_month_usd"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Region == "" || in.DiskType == "" {
		c.JSON(400, gin.H{"error": "区域和磁盘类型必填"})
		return
	}
	if _, err := h.DB.Exec(`INSERT INTO cloud_disk_rates (provider,region,disk_type,gb_month_usd,note) VALUES ('gcp',?,?,?,'manual')`,
		in.Region, in.DiskType, in.GbMonth); err != nil {
		c.JSON(500, gin.H{"error": "该区域+磁盘类型已存在或写入失败"})
		return
	}
	c.JSON(201, gin.H{"ok": true})
}

func (h *HostHandler) UpdateDiskRate(c *gin.Context) {
	var in struct {
		GbMonth float64 `json:"gb_month_usd"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE cloud_disk_rates SET gb_month_usd=?, note='confirmed' WHERE id=?`, in.GbMonth, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *HostHandler) DeleteDiskRate(c *gin.Context) {
	logExec(h.DB, "主机同步写", `DELETE FROM cloud_disk_rates WHERE id=?`, c.Param("id"))
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
		HostCount  int    `json:"host_count"`
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
	var in struct {
		Name      string `json:"name"`
		Provider  string `json:"provider"`
		BillingDS string `json:"billing_export_dataset"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		c.JSON(400, gin.H{"error": "name 必填"})
		return
	}
	if in.Provider == "" {
		in.Provider = "gcp"
	}
	if _, err := h.DB.Exec(`INSERT INTO cloud_accounts (name, provider, billing_export_dataset) VALUES (?, ?, ?)`,
		in.Name, in.Provider, in.BillingDS); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	SetAuditTarget(c, in.Name)
	c.JSON(201, gin.H{"ok": true})
}

func (h *HostHandler) UpdateAccount(c *gin.Context) {
	var in struct {
		Name      string `json:"name"`
		BillingDS string `json:"billing_export_dataset"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE cloud_accounts SET name=?, billing_export_dataset=? WHERE id=?`,
		in.Name, in.BillingDS, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *HostHandler) DeleteAccount(c *gin.Context) {
	// 删账号：其下 project + 主机一并清（只读同步来的，无业务数据）
	tx, _ := h.DB.Begin()
	id := c.Param("id")
	_, _ = tx.Exec(`DELETE hd FROM host_disks hd JOIN hosts h ON h.ci_id=hd.host_ci_id WHERE h.cloud_account_id=?`, id)
	_, _ = tx.Exec(`DELETE c FROM cis c JOIN hosts h ON h.ci_id=c.id WHERE h.cloud_account_id=?`, id)
	_, _ = tx.Exec(`DELETE FROM hosts WHERE cloud_account_id=?`, id)
	_, _ = tx.Exec(`DELETE FROM cloud_account_projects WHERE account_id=?`, id)
	_, _ = tx.Exec(`DELETE FROM cloud_accounts WHERE id=?`, id)
	if err := tx.Commit(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---------- 云项目（凭据在这一层：name/project_id/cred） ----------

func (h *HostHandler) CreateProject(c *gin.Context) {
	var in struct {
		Name      string `json:"name"`
		ProjectID string `json:"project_id"`
		CredJSON  string `json:"cred_json"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.ProjectID == "" {
		c.JSON(400, gin.H{"error": "project_id 必填"})
		return
	}
	if in.Name == "" {
		in.Name = in.ProjectID
	}
	enc := ""
	if in.CredJSON != "" {
		e, err := h.Cipher.Encrypt(in.CredJSON)
		if err != nil {
			c.JSON(500, gin.H{"error": "凭据加密失败"})
			return
		}
		enc = e
	}
	if _, err := h.DB.Exec(`INSERT INTO cloud_account_projects (account_id, name, project_id, cred_enc) VALUES (?, ?, ?, ?)`,
		c.Param("id"), in.Name, in.ProjectID, enc); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	SetAuditTarget(c, in.ProjectID)
	c.JSON(201, gin.H{"ok": true})
}

func (h *HostHandler) UpdateProject(c *gin.Context) {
	var in struct {
		Name      string `json:"name"`
		ProjectID string `json:"project_id"`
		CredJSON  string `json:"cred_json"` // 空=不改凭据
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.ProjectID == "" {
		c.JSON(400, gin.H{"error": "project_id 必填"})
		return
	}
	if in.Name == "" {
		in.Name = in.ProjectID
	}
	if _, err := h.DB.Exec(`UPDATE cloud_account_projects SET name=?, project_id=? WHERE id=?`,
		in.Name, in.ProjectID, c.Param("pid")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if in.CredJSON != "" {
		if e, err := h.Cipher.Encrypt(in.CredJSON); err == nil {
			logExec(h.DB, "主机同步写", `UPDATE cloud_account_projects SET cred_enc=? WHERE id=?`, e, c.Param("pid"))
		}
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *HostHandler) DeleteProject(c *gin.Context) {
	pid := c.Param("pid")
	var accountID int
	var projectID string
	if h.DB.QueryRow(`SELECT account_id, project_id FROM cloud_account_projects WHERE id=?`, pid).Scan(&accountID, &projectID) != nil {
		c.JSON(404, gin.H{"error": "项目不存在"})
		return
	}
	tx, _ := h.DB.Begin()
	_, _ = tx.Exec(`DELETE hd FROM host_disks hd JOIN hosts h ON h.ci_id=hd.host_ci_id WHERE h.cloud_account_id=? AND h.project=?`, accountID, projectID)
	_, _ = tx.Exec(`DELETE c FROM cis c JOIN hosts h ON h.ci_id=c.id WHERE h.cloud_account_id=? AND h.project=?`, accountID, projectID)
	_, _ = tx.Exec(`DELETE FROM hosts WHERE cloud_account_id=? AND project=?`, accountID, projectID)
	_, _ = tx.Exec(`DELETE FROM cloud_account_projects WHERE id=?`, pid)
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
func (h *HostHandler) startProjectSync(accountID int, pid int64, projName string) *hostSyncState {
	hostSyncMu.Lock()
	defer hostSyncMu.Unlock()
	if st := hostSyncStore[pid]; st != nil && st.Running {
		return nil
	}
	st := &hostSyncState{Running: true, StartedAt: time.Now(),
		AccountID: accountID, ProjectID: pid, ProjectName: projName}
	hostSyncStore[pid] = st
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
	hostSyncMu.Unlock()
}

// SyncProject 同步指定 project（后台异步 + 进度；主机多约 1-3 分钟，避免 HTTP 超时误报失败）。
func (h *HostHandler) SyncProject(c *gin.Context) {
	pid, _ := strconv.ParseInt(c.Param("pid"), 10, 64)
	var accountID int
	if h.DB.QueryRow(`SELECT account_id FROM cloud_account_projects WHERE id=?`, pid).Scan(&accountID) != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "项目不存在"})
		return
	}
	_, projName, _ := h.projectMeta(pid)
	st := h.startProjectSync(accountID, pid, projName)
	if st == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "该项目正在同步中，请稍候"})
		return
	}
	SetAuditTarget(c, strconv.FormatInt(pid, 10))
	go func() {
		// 手动触发也要进「执行记录」——之前只有定时任务写，手动点完在执行记录里
		// 什么都看不到，用户无从判断到底跑没跑。
		runID, start := startManualRunLog(h.DB, "host_sync", "手动同步项目 "+projName)
		r, err := h.syncOneProject(pid, st)
		e := ""
		if err != nil {
			e = err.Error()
		}
		h.finishHostSync(st, e)
		finishManualRunLog(h.DB, runID, start, err,
			projName+"："+hostSyncSummary(r.Synced, r.Gone, r.NewlyGone))
	}()
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "running": true, "msg": "已在后台同步，主机多约 1-3 分钟，完成后自动刷新"})
}

// SyncAccount 后台异步同步账号下所有 project（各用各自凭据，依次跑；单个失败不影响其它）。
func (h *HostHandler) SyncAccount(c *gin.Context) {
	accountID, _ := strconv.Atoi(c.Param("id"))
	rows, err := h.DB.Query(`SELECT id FROM cloud_account_projects WHERE account_id=? ORDER BY id`, accountID)
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
		c.JSON(400, gin.H{"error": "该账号下还没有项目，请先添加项目并配置凭据"})
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
		st := h.startProjectSync(accountID, pid, pn)
		if st == nil {
			skipped = append(skipped, pn)
			continue
		}
		jobs = append(jobs, job{pid, st, pn})
	}
	if len(jobs) == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "该账号下的项目都正在同步中，请稍候"})
		return
	}
	SetAuditTarget(c, c.Param("id"))
	go func() {
		// 串行跑：各项目虽用各自凭据，但共享 GCP API 配额，并发容易撞限流
		runID, start := startManualRunLog(h.DB, "host_sync", "手动同步账号下全部项目")
		var errs []string
		totalSynced, totalStale, totalNewlyGone := 0, 0, 0
		for _, j := range jobs {
			r, err := h.syncOneProject(j.pid, j.st)
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
		finishManualRunLog(h.DB, runID, start, runErr,
			fmt.Sprintf("同步 %d 个项目：", len(jobs))+hostSyncSummary(totalSynced, totalStale, totalNewlyGone))
	}()
	resp := gin.H{"ok": true, "running": true, "projects": len(jobs),
		"msg": "已在后台同步，主机多约 1-3 分钟，完成后自动刷新"}
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
	id, _ := strconv.Atoi(c.Param("id"))
	hostSyncMu.Lock()
	items := []gin.H{}
	agg := hostSyncState{}
	anyRunning, started := false, false
	errs := []string{}
	for _, st := range hostSyncStore {
		if st.AccountID != id {
			continue
		}
		started = true
		if st.Running {
			anyRunning = true
		}
		agg.Total += st.Total
		agg.Done += st.Done
		agg.Synced += st.Synced
		agg.Stale += st.Stale
		if st.Err != "" {
			errs = append(errs, st.ProjectName+": "+st.Err)
		}
		items = append(items, gin.H{
			"project_id": st.ProjectID, "project": st.ProjectName, "running": st.Running,
			"total": st.Total, "done": st.Done, "synced": st.Synced, "stale": st.Stale,
			"error": st.Err,
		})
	}
	hostSyncMu.Unlock()
	if !started {
		c.JSON(http.StatusOK, gin.H{"running": false, "started": false, "projects": []gin.H{}})
		return
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i]["project_id"].(int64) < items[j]["project_id"].(int64)
	})
	c.JSON(http.StatusOK, gin.H{
		"running": anyRunning, "started": true,
		"total": agg.Total, "done": agg.Done, "synced": agg.Synced, "stale": agg.Stale,
		"error": strings.Join(errs, "；"), "projects": items,
	})
}

// ProjectSyncStatus 查单个项目的同步进度。
func (h *HostHandler) ProjectSyncStatus(c *gin.Context) {
	pid, _ := strconv.ParseInt(c.Param("pid"), 10, 64)
	hostSyncMu.Lock()
	st := hostSyncStore[pid]
	if st == nil {
		hostSyncMu.Unlock()
		c.JSON(http.StatusOK, gin.H{"running": false, "started": false})
		return
	}
	out := gin.H{"running": st.Running, "started": true, "project": st.ProjectName,
		"total": st.Total, "done": st.Done, "synced": st.Synced, "stale": st.Stale, "error": st.Err}
	hostSyncMu.Unlock()
	c.JSON(http.StatusOK, out)
}

// startManualRunLog 手动触发的同步也写一条执行记录。
// 之前只有定时任务写 task_run_logs，手动点完在「执行记录」里什么都看不到。
// task_key 与定时任务一致，这样按任务名筛选时手动和定时的能一起看到，
// 靠 trigger_by 区分。
func startManualRunLog(db *sql.DB, taskKey, summary string) (int64, time.Time) {
	start := time.Now()
	var name string
	_ = db.QueryRow(`SELECT name FROM scheduled_tasks WHERE task_key=?`, taskKey).Scan(&name)
	if name == "" {
		name = summary
	}
	res, err := db.Exec(`INSERT INTO task_run_logs (task_key,name,status,trigger_by,started_at,finished_at)
		VALUES (?,?,?,'manual',?,?)`, taskKey, name, taskStatusRunning, start, start)
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
func finishManualRunLog(db *sql.DB, runID int64, start time.Time, err error, summary string) {
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
	if _, e := db.Exec(`UPDATE task_run_logs SET status=?, summary=?, duration_ms=?, progress='', finished_at=NOW() WHERE id=?`,
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
func (h *HostHandler) syncOneProject(pid int64, st *hostSyncState) (res projectSyncResult, err error) {
	var accountID int
	var provider, projectID, enc string
	err = h.DB.QueryRow(`SELECT p.name, p.account_id, p.project_id, COALESCE(p.cred_enc,''), COALESCE(a.provider,'gcp')
		FROM cloud_account_projects p LEFT JOIN cloud_accounts a ON a.id=p.account_id WHERE p.id=?`, pid).
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
		logExec(h.DB, "主机同步写", `UPDATE cloud_account_projects SET last_sync_at=NOW(), last_result=? WHERE id=?`, truncate(e.Error(), 250), pid)
		return res, e
	}
	hsSet(st, func(s *hostSyncState) { s.Total += len(insts) })
	present := map[string]bool{}
	for _, in := range insts {
		present[in.InstanceID] = true
		h.upsertHost(accountID, res.Name, in)
		hsSet(st, func(s *hostSyncState) { s.Done++ })
	}
	res.NewlyGone, res.Gone = h.markStaleHosts(accountID, projectID, present)
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
	logExec(h.DB, "主机同步写", `UPDATE cloud_account_projects SET last_sync_at=NOW(), last_result=? WHERE id=?`,
		truncate(result, 250), pid)
	return res, nil
}

// SyncAllHostProjects 供定时任务(host_sync)调用：同步所有账号所有 project，返回摘要+失败明细+是否成功。
func SyncAllHostProjects(db *sql.DB, cipher *crypto.Cipher) (string, []TaskFailure, bool) {
	h := &HostHandler{DB: db, Cipher: cipher}
	rows, err := db.Query(`SELECT id FROM cloud_account_projects ORDER BY id`)
	if err != nil {
		return "查询云项目失败: " + err.Error(), nil, false
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
		return "没有配置云项目，跳过", nil, true
	}
	totalSynced, totalStale, totalNewlyGone := 0, 0, 0
	var failures []TaskFailure
	for _, pid := range pids {
		r, err := h.syncOneProject(pid, nil)
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

// detectGKENode 判断 GCE 实例是否 GKE 节点(名字 gke- 前缀 或 goog-gke label)，并取节点池名。
func detectGKENode(name string, labels map[string]string) (bool, string) {
	isNode := strings.HasPrefix(name, "gke-")
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

func (h *HostHandler) upsertHost(accountID int, projName string, in cloudsource.Instance) {
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
	err := h.DB.QueryRow(`SELECT ci_id FROM hosts WHERE cloud_account_id=? AND instance_id=?`, accountID, in.InstanceID).Scan(&ciID)
	if err == sql.ErrNoRows {
		res, e := h.DB.Exec(`INSERT INTO cis (type, name, status) VALUES ('host', ?, 'active')`, in.Name)
		if e != nil {
			return
		}
		ciID, _ = res.LastInsertId()
		logExec(h.DB, "主机同步写", `INSERT INTO hosts (ci_id, instance_id, cloud_account_id, provider) VALUES (?, ?, ?, ?)`,
			ciID, in.InstanceID, accountID, "gcp")
	} else if err != nil {
		return
	} else {
		logExec(h.DB, "主机同步写", `UPDATE cis SET name=? WHERE id=?`, in.Name, ciID)
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
func (h *HostHandler) markStaleHosts(accountID int, projectID string, present map[string]bool) (newly, total int) {
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
	CIID        int64             `json:"ci_id"`
	Name        string            `json:"name"`
	Project     string            `json:"project"`      // project id
	ProjectName string            `json:"project_name"` // GCP 显示名
	Zone        string            `json:"zone"`
	Region      string            `json:"region"`
	MachineType string            `json:"machine_type"`
	VCPU        int               `json:"vcpu"`
	MemMB       int               `json:"mem_mb"`
	DiskTotalGB int               `json:"disk_total_gb"`
	InternalIP  string            `json:"internal_ip"`
	ExternalIP  string            `json:"external_ip"`
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
	CreatedAt   string            `json:"gcp_created_at"`
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
	// 成本估算（USD）
	CostDaily  float64 `json:"cost_daily"`
	CostMonth  float64 `json:"cost_month"`
	CostTotal  float64 `json:"cost_total"`
	CostSource string  `json:"cost_source"` // estimate / bigquery
}

func (h *HostHandler) ListHosts(c *gin.Context) {
	rc := h.loadRates()
	// 各主机磁盘明细（类型+容量，用于按区域×磁盘类型算成本）
	hostDisks := map[int64][]diskRow{}
	drows, _ := h.DB.Query(`SELECT host_ci_id, type, size_gb FROM host_disks`)
	if drows != nil {
		for drows.Next() {
			var ci int64
			var typ string
			var sz int
			if drows.Scan(&ci, &typ, &sz) == nil {
				hostDisks[ci] = append(hostDisks[ci], diskRow{Type: typ, SizeGB: sz})
			}
		}
		drows.Close()
	}
	rows, err := h.DB.Query(`SELECT c.id, c.name, h.project, h.project_name, h.zone, h.region, h.machine_type, h.vcpu, h.mem_mb, h.disk_total_gb,
		h.internal_ip, h.external_ip, h.status, h.os, h.labels, h.stale, h.gcp_created_at, h.preemptible, h.provider, COALESCE(ca.name,''),
		h.is_k8s_node, COALESCE(h.k8s_pool,'')
		FROM cis c JOIN hosts h ON h.ci_id=c.id LEFT JOIN cloud_accounts ca ON ca.id=h.cloud_account_id
		WHERE c.type='host' ORDER BY h.project, c.name`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []hostOut{}
	now := time.Now()
	for rows.Next() {
		var o hostOut
		var labels sql.NullString
		var created sql.NullTime
		var stale, preempt, isK8s int
		if rows.Scan(&o.CIID, &o.Name, &o.Project, &o.ProjectName, &o.Zone, &o.Region, &o.MachineType, &o.VCPU, &o.MemMB, &o.DiskTotalGB,
			&o.InternalIP, &o.ExternalIP, &o.Status, &o.OS, &labels, &stale, &created, &preempt, &o.Provider, &o.AccountName, &isK8s, &o.K8sPool); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		o.Stale = stale == 1
		o.Lifecycle = lifecycleOf(o.Stale)
		o.IsK8sNode = isK8s == 1
		o.Preemptible = preempt == 1
		if labels.Valid && labels.String != "" {
			_ = json.Unmarshal([]byte(labels.String), &o.Labels)
		}
		hourly, _, _, _ := rc.hostHourly(o.Region, familyOf(o.MachineType), o.VCPU, o.MemMB, o.Status, hostDisks[o.CIID])
		o.CostDaily = round2(hourly * 24)
		o.CostMonth = round2(hourly * 730)
		o.CostSource = "estimate"
		if created.Valid {
			o.CreatedAt = created.Time.Format("2006-01-02")
			o.CostTotal = round2(hourly * now.Sub(created.Time).Hours())
		}
		out = append(out, o)
	}
	c.JSON(http.StatusOK, out)
}

// HostDetail 主机详情：磁盘逐块 + 关联业务域名 + 成本（可选 ?as_of=YYYY-MM-DD 算累计到指定日期）。
func (h *HostHandler) HostDetail(c *gin.Context) {
	ciid := c.Param("ciid")
	var o hostOut
	var labels sql.NullString
	var created sql.NullTime
	var stale, preempt, delProt int
	var tags, sas string
	err := h.DB.QueryRow(`SELECT c.id, c.name, h.project, h.project_name, h.zone, h.region, h.machine_type, h.vcpu, h.mem_mb, h.disk_total_gb,
		h.internal_ip, h.external_ip, h.status, h.os, h.labels, h.stale, h.gcp_created_at, COALESCE(ca.name,''),
		h.hostname, h.vpc, h.subnet, h.network_tags, h.preemptible, h.image, h.cpu_platform, h.deletion_protection, h.service_accounts
		FROM cis c JOIN hosts h ON h.ci_id=c.id LEFT JOIN cloud_accounts ca ON ca.id=h.cloud_account_id
		WHERE c.id=? AND c.type='host'`, ciid).
		Scan(&o.CIID, &o.Name, &o.Project, &o.ProjectName, &o.Zone, &o.Region, &o.MachineType, &o.VCPU, &o.MemMB, &o.DiskTotalGB,
			&o.InternalIP, &o.ExternalIP, &o.Status, &o.OS, &labels, &stale, &created, &o.AccountName,
			&o.Hostname, &o.VPC, &o.Subnet, &tags, &preempt, &o.Image, &o.CPUPlatform, &delProt, &sas)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "主机不存在"})
		return
	}
	o.Stale = stale == 1
	o.Lifecycle = lifecycleOf(o.Stale)
	o.Preemptible = preempt == 1
	o.DeletionProtection = delProt == 1
	o.NetworkTags = splitNonEmpty(tags)
	o.ServiceAccounts = splitNonEmpty(sas)
	if labels.Valid && labels.String != "" {
		_ = json.Unmarshal([]byte(labels.String), &o.Labels)
	}
	// 磁盘
	type disk struct {
		Name   string `json:"name"`
		SizeGB int    `json:"size_gb"`
		Type   string `json:"type"`
		IsBoot bool   `json:"is_boot"`
	}
	disks := []disk{}
	drows, _ := h.DB.Query(`SELECT name, size_gb, type, is_boot FROM host_disks WHERE host_ci_id=? ORDER BY is_boot DESC, id`, ciid)
	if drows != nil {
		for drows.Next() {
			var d disk
			var boot int
			if drows.Scan(&d.Name, &d.SizeGB, &d.Type, &boot) == nil {
				d.IsBoot = boot == 1
				disks = append(disks, d)
			}
		}
		drows.Close()
	}
	// 关联业务域名（源站IP 命中 内网/外网 IP）
	related := []gin.H{}
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
					related = append(related, gin.H{"fqdn": recordFQDN(host, domain), "ip": ip})
				}
			}
			rrows.Close()
		}
	}
	// 成本（按 区域×机型族 + 区域×磁盘类型 分档）
	rc := h.loadRates()
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
	c.JSON(http.StatusOK, gin.H{
		"host": o, "disks": disks, "related_domains": related,
		"cost_hourly": round4(hourly), "as_of": asOf.Format("2006-01-02"),
		"rate_matched": matched, "rate_vcpu_hour": round4(vcpuHour), "rate_ram_gb_hour": round4(ramGbHour), "rate_family": family,
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

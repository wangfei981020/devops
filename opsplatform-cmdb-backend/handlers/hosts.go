package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/cloudsource"
	"opsplatform-cmdb-backend/crypto"
)

// HostHandler 云主机（一期只读）：云账号管理 + GCP 同步 + 主机台账 + 成本估算 + 域名关联。
type HostHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewHostHandler(db *sql.DB, cipher *crypto.Cipher) *HostHandler { return &HostHandler{DB: db, Cipher: cipher} }

func (h *HostHandler) Register(r *gin.RouterGroup) {
	r.GET("/cloud-accounts", h.ListAccounts)
	r.POST("/cloud-accounts", h.CreateAccount)
	r.PUT("/cloud-accounts/:id", h.UpdateAccount)
	r.DELETE("/cloud-accounts/:id", h.DeleteAccount)
	r.POST("/cloud-accounts/:id/sync", h.SyncAccount)      // 同步账号下所有 project
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

func (h *HostHandler) loadRates() *rateCache {
	rc := &rateCache{compute: map[string][2]float64{}, disk: map[string]float64{}}
	if rows, _ := h.DB.Query(`SELECT region, machine_family, vcpu_hour_usd, ram_gb_hour_usd FROM cloud_compute_rates WHERE provider='gcp'`); rows != nil {
		for rows.Next() {
			var region, family string
			var v, r float64
			if rows.Scan(&region, &family, &v, &r) == nil {
				rc.compute[region+"|"+family] = [2]float64{v, r}
			}
		}
		rows.Close()
	}
	if rows, _ := h.DB.Query(`SELECT region, disk_type, gb_month_usd FROM cloud_disk_rates WHERE provider='gcp'`); rows != nil {
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
	_, _ = h.DB.Exec(`DELETE FROM cloud_compute_rates WHERE id=?`, c.Param("id"))
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
	_, _ = h.DB.Exec(`DELETE FROM cloud_disk_rates WHERE id=?`, c.Param("id"))
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
	WriteAudit(h.DB, c, "add_cloud_account", in.Name)
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
	WriteAudit(h.DB, c, "add_cloud_project", in.ProjectID)
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
			_, _ = h.DB.Exec(`UPDATE cloud_account_projects SET cred_enc=? WHERE id=?`, e, c.Param("pid"))
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

// SyncProject 同步指定 project（只读拉取，upsert；该 project 下 GCP 已无的标 stale）。
func (h *HostHandler) SyncProject(c *gin.Context) {
	pid, _ := strconv.ParseInt(c.Param("pid"), 10, 64)
	name, synced, stale, err := h.syncOneProject(pid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "sync_cloud_project", name)
	c.JSON(http.StatusOK, gin.H{"project": name, "synced": synced, "stale": stale})
}

// SyncAccount 同步账号下所有 project（各用各自凭据，依次跑；单个失败不影响其它）。
func (h *HostHandler) SyncAccount(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id FROM cloud_account_projects WHERE account_id=? ORDER BY id`, c.Param("id"))
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
	totalSynced, totalStale := 0, 0
	var fails []string
	for _, pid := range pids {
		name, synced, stale, err := h.syncOneProject(pid)
		if err != nil {
			fails = append(fails, name+"："+err.Error())
			continue
		}
		totalSynced += synced
		totalStale += stale
	}
	WriteAudit(h.DB, c, "sync_cloud_account", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"synced": totalSynced, "stale": totalStale, "failures": fails})
}

// syncOneProject 同步单个 project 行：解密该 project 凭据 → 列实例 → upsert → 标 stale → 记结果。
func (h *HostHandler) syncOneProject(pid int64) (name string, synced, stale int, err error) {
	var accountID int
	var provider, projectID, enc string
	err = h.DB.QueryRow(`SELECT p.name, p.account_id, p.project_id, COALESCE(p.cred_enc,''), COALESCE(a.provider,'gcp')
		FROM cloud_account_projects p LEFT JOIN cloud_accounts a ON a.id=p.account_id WHERE p.id=?`, pid).
		Scan(&name, &accountID, &projectID, &enc, &provider)
	if err != nil {
		return "", 0, 0, fmt.Errorf("项目不存在")
	}
	if enc == "" {
		return name, 0, 0, fmt.Errorf("该项目还没配置 service account 凭据")
	}
	credJSON, e := h.Cipher.Decrypt(enc)
	if e != nil {
		return name, 0, 0, fmt.Errorf("凭据解密失败")
	}
	adapter, e := cloudsource.NewAdapter(provider, credJSON)
	if e != nil {
		return name, 0, 0, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	insts, e := adapter.ListInstances(ctx, projectID)
	if e != nil {
		_, _ = h.DB.Exec(`UPDATE cloud_account_projects SET last_sync_at=NOW(), last_result=? WHERE id=?`, truncate(e.Error(), 250), pid)
		return name, 0, 0, e
	}
	present := map[string]bool{}
	for _, in := range insts {
		present[in.InstanceID] = true
		h.upsertHost(accountID, name, in)
	}
	stale = h.markStaleHosts(accountID, projectID, present)
	// 顺带同步该 project 的网络资源（VPC/子网/防火墙/静态IP/负载均衡）
	if nr, e := adapter.ListNetwork(ctx, projectID); e == nil {
		SyncProjectNetwork(h.DB, provider, accountID, projectID, nr)
	}
	_, _ = h.DB.Exec(`UPDATE cloud_account_projects SET last_sync_at=NOW(), last_result=? WHERE id=?`,
		truncate(fmt.Sprintf("同步 %d 台，失效 %d", len(insts), stale), 250), pid)
	return name, len(insts), stale, nil
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
	totalSynced, totalStale := 0, 0
	var failures []TaskFailure
	for _, pid := range pids {
		name, synced, stale, err := h.syncOneProject(pid)
		if err != nil {
			failures = append(failures, TaskFailure{Target: name, Reason: truncate(err.Error(), 150)})
			continue
		}
		totalSynced += synced
		totalStale += stale
	}
	ok := !(totalSynced == 0 && len(failures) == len(pids)) // 全部项目都失败才算失败
	msg := fmt.Sprintf("同步 %d 台主机（%d 个项目），失效 %d", totalSynced, len(pids), totalStale)
	if len(failures) > 0 {
		msg += fmt.Sprintf("，%d/%d 个项目失败", len(failures), len(pids))
	}
	return msg, failures, ok
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
		_, _ = h.DB.Exec(`INSERT INTO hosts (ci_id, instance_id, cloud_account_id, provider) VALUES (?, ?, ?, ?)`,
			ciID, in.InstanceID, accountID, "gcp")
	} else if err != nil {
		return
	} else {
		_, _ = h.DB.Exec(`UPDATE cis SET name=? WHERE id=?`, in.Name, ciID)
	}
	preempt, delProt := 0, 0
	if in.Preemptible {
		preempt = 1
	}
	if in.DeletionProtection {
		delProt = 1
	}
	_, _ = h.DB.Exec(`UPDATE hosts SET project=?, project_name=?, zone=?, region=?, machine_type=?, vcpu=?, mem_mb=?, disk_total_gb=?,
		internal_ip=?, external_ip=?, status=?, os=?, labels=?, self_link=?, gcp_created_at=?,
		hostname=?, vpc=?, subnet=?, network_tags=?, preemptible=?, image=?, cpu_platform=?, deletion_protection=?, service_accounts=?,
		stale=0, synced_at=NOW() WHERE ci_id=?`,
		in.Project, projName, in.Zone, in.Region, in.MachineType, in.VCPU, in.MemMB, total,
		in.InternalIP, in.ExternalIP, in.Status, in.OS, string(labelsJSON), in.SelfLink, created,
		in.Hostname, in.VPC, in.Subnet, strings.Join(in.NetworkTags, ","), preempt, in.Image, in.CPUPlatform, delProt, strings.Join(in.ServiceAccounts, ","),
		ciID)
	// 磁盘：全删重插
	_, _ = h.DB.Exec(`DELETE FROM host_disks WHERE host_ci_id=?`, ciID)
	for _, d := range in.Disks {
		boot := 0
		if d.IsBoot {
			boot = 1
		}
		_, _ = h.DB.Exec(`INSERT INTO host_disks (host_ci_id, name, size_gb, type, is_boot) VALUES (?, ?, ?, ?, ?)`,
			ciID, d.Name, d.SizeGB, d.Type, boot)
	}
}

// markStaleHosts 把该 (账号, project) 下 GCP 已无的实例标 stale（只影响这个 project，不碰其它 project）。
func (h *HostHandler) markStaleHosts(accountID int, projectID string, present map[string]bool) int {
	rows, err := h.DB.Query(`SELECT ci_id, instance_id FROM hosts WHERE cloud_account_id=? AND project=?`, accountID, projectID)
	if err != nil {
		return 0
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
	n := 0
	for _, r := range all {
		if !present[r.inst] {
			_, _ = h.DB.Exec(`UPDATE hosts SET stale=1 WHERE ci_id=?`, r.ciID)
			n++
		}
	}
	return n
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
	Status      string            `json:"status"`
	OS          string            `json:"os"`
	Labels      map[string]string `json:"labels"`
	AccountName string            `json:"account_name"`
	Provider    string            `json:"provider"`
	Stale       bool              `json:"stale"`
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
		h.internal_ip, h.external_ip, h.status, h.os, h.labels, h.stale, h.gcp_created_at, h.preemptible, h.provider, COALESCE(ca.name,'')
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
		var stale, preempt int
		if rows.Scan(&o.CIID, &o.Name, &o.Project, &o.ProjectName, &o.Zone, &o.Region, &o.MachineType, &o.VCPU, &o.MemMB, &o.DiskTotalGB,
			&o.InternalIP, &o.ExternalIP, &o.Status, &o.OS, &labels, &stale, &created, &preempt, &o.Provider, &o.AccountName); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		o.Stale = stale == 1
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

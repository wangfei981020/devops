package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
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
	r.POST("/cloud-accounts/:id/sync", h.Sync)
	r.GET("/cloud-price-rates", h.ListRates)
	r.PUT("/cloud-price-rates/:id", h.UpdateRate)
	r.GET("/hosts", h.ListHosts)         // 只读
	r.GET("/hosts/:ciid", h.HostDetail)  // 只读
}

// ---------- 费率 ----------

type priceRate struct {
	vcpuHour, ramGbHour, ssdMonth, stdMonth float64
}

func (h *HostHandler) rate() priceRate {
	var r priceRate
	_ = h.DB.QueryRow(`SELECT vcpu_hour_usd, ram_gb_hour_usd, disk_ssd_gb_month, disk_std_gb_month
		FROM cloud_price_rates WHERE provider='gcp' AND region='default' AND machine_family='default' LIMIT 1`).
		Scan(&r.vcpuHour, &r.ramGbHour, &r.ssdMonth, &r.stdMonth)
	return r
}

// estHourly 估算每小时成本(USD)。停机(非 RUNNING)只算磁盘；运行算 vCPU+内存+磁盘。
func estHourly(vcpu, memMB int, ssdGB, stdGB int, status string, r priceRate) float64 {
	compute := 0.0
	if status == "RUNNING" {
		compute = float64(vcpu)*r.vcpuHour + (float64(memMB)/1024.0)*r.ramGbHour
	}
	disk := float64(ssdGB)*r.ssdMonth/730.0 + float64(stdGB)*r.stdMonth/730.0
	return compute + disk
}

func (h *HostHandler) ListRates(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, provider, region, machine_family, vcpu_hour_usd, ram_gb_hour_usd, disk_ssd_gb_month, disk_std_gb_month FROM cloud_price_rates ORDER BY id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type rt struct {
		ID          int     `json:"id"`
		Provider    string  `json:"provider"`
		Region      string  `json:"region"`
		Family      string  `json:"machine_family"`
		VcpuHour    float64 `json:"vcpu_hour_usd"`
		RamGbHour   float64 `json:"ram_gb_hour_usd"`
		SsdMonth    float64 `json:"disk_ssd_gb_month"`
		StdMonth    float64 `json:"disk_std_gb_month"`
	}
	out := []rt{}
	for rows.Next() {
		var x rt
		if rows.Scan(&x.ID, &x.Provider, &x.Region, &x.Family, &x.VcpuHour, &x.RamGbHour, &x.SsdMonth, &x.StdMonth) == nil {
			out = append(out, x)
		}
	}
	c.JSON(http.StatusOK, out)
}

func (h *HostHandler) UpdateRate(c *gin.Context) {
	var in struct {
		VcpuHour  float64 `json:"vcpu_hour_usd"`
		RamGbHour float64 `json:"ram_gb_hour_usd"`
		SsdMonth  float64 `json:"disk_ssd_gb_month"`
		StdMonth  float64 `json:"disk_std_gb_month"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE cloud_price_rates SET vcpu_hour_usd=?, ram_gb_hour_usd=?, disk_ssd_gb_month=?, disk_std_gb_month=? WHERE id=?`,
		in.VcpuHour, in.RamGbHour, in.SsdMonth, in.StdMonth, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ---------- 云账号 ----------

func (h *HostHandler) ListAccounts(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, name, provider, projects, billing_export_dataset, last_sync_at, last_result FROM cloud_accounts ORDER BY id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type acct struct {
		ID          int    `json:"id"`
		Name        string `json:"name"`
		Provider    string `json:"provider"`
		Projects    string `json:"projects"`
		BillingDS   string `json:"billing_export_dataset"`
		LastSyncAt  string `json:"last_sync_at"`
		LastResult  string `json:"last_result"`
	}
	out := []acct{}
	for rows.Next() {
		var a acct
		var ls sql.NullTime
		if rows.Scan(&a.ID, &a.Name, &a.Provider, &a.Projects, &a.BillingDS, &ls, &a.LastResult) == nil {
			if ls.Valid {
				a.LastSyncAt = ls.Time.Format("2006-01-02 15:04")
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
		Projects  string `json:"projects"`
		CredJSON  string `json:"cred_json"`
		BillingDS string `json:"billing_export_dataset"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		c.JSON(400, gin.H{"error": "name 必填"})
		return
	}
	if in.Provider == "" {
		in.Provider = "gcp"
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
	if _, err := h.DB.Exec(`INSERT INTO cloud_accounts (name, provider, projects, cred_enc, billing_export_dataset) VALUES (?, ?, ?, ?, ?)`,
		in.Name, in.Provider, in.Projects, enc, in.BillingDS); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "add_cloud_account", in.Name)
	c.JSON(201, gin.H{"ok": true})
}

func (h *HostHandler) UpdateAccount(c *gin.Context) {
	var in struct {
		Name      string `json:"name"`
		Projects  string `json:"projects"`
		CredJSON  string `json:"cred_json"` // 空=不改凭据
		BillingDS string `json:"billing_export_dataset"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.DB.Exec(`UPDATE cloud_accounts SET name=?, projects=?, billing_export_dataset=? WHERE id=?`,
		in.Name, in.Projects, in.BillingDS, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if in.CredJSON != "" {
		if e, err := h.Cipher.Encrypt(in.CredJSON); err == nil {
			_, _ = h.DB.Exec(`UPDATE cloud_accounts SET cred_enc=? WHERE id=?`, e, c.Param("id"))
		}
	}
	c.JSON(200, gin.H{"ok": true})
}

func (h *HostHandler) DeleteAccount(c *gin.Context) {
	// 删账号：其下主机一并清（只读同步来的，无业务数据）
	tx, _ := h.DB.Begin()
	id := c.Param("id")
	_, _ = tx.Exec(`DELETE hd FROM host_disks hd JOIN hosts h ON h.ci_id=hd.host_ci_id WHERE h.cloud_account_id=?`, id)
	_, _ = tx.Exec(`DELETE c FROM cis c JOIN hosts h ON h.ci_id=c.id WHERE h.cloud_account_id=?`, id)
	_, _ = tx.Exec(`DELETE FROM hosts WHERE cloud_account_id=?`, id)
	_, _ = tx.Exec(`DELETE FROM cloud_accounts WHERE id=?`, id)
	if err := tx.Commit(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// loadAccountCred 取账号 provider + 解密后的 SA JSON + projects 列表
func (h *HostHandler) loadAccountCred(id string) (provider, credJSON string, projects []string, err error) {
	var enc, projStr string
	err = h.DB.QueryRow(`SELECT provider, COALESCE(cred_enc,''), projects FROM cloud_accounts WHERE id=?`, id).Scan(&provider, &enc, &projStr)
	if err != nil {
		return
	}
	if enc != "" {
		credJSON, err = h.Cipher.Decrypt(enc)
		if err != nil {
			return
		}
	}
	for _, p := range strings.Split(projStr, ",") {
		if p = strings.TrimSpace(p); p != "" {
			projects = append(projects, p)
		}
	}
	return
}

// Sync 从云账号同步主机（只读拉取，upsert；该账号下 GCP 已无的标 stale）。
func (h *HostHandler) Sync(c *gin.Context) {
	id := c.Param("id")
	provider, credJSON, projects, err := h.loadAccountCred(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "云账号不存在或凭据读取失败"})
		return
	}
	if credJSON == "" || len(projects) == 0 {
		c.JSON(400, gin.H{"error": "请先在云账号里填 service account JSON 和要同步的 project"})
		return
	}
	adapter, err := cloudsource.NewAdapter(provider, credJSON)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	insts, err := adapter.ListInstances(ctx, projects)
	if err != nil {
		_, _ = h.DB.Exec(`UPDATE cloud_accounts SET last_sync_at=NOW(), last_result=? WHERE id=?`, truncate(err.Error(), 250), id)
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	present := map[string]bool{}
	for _, in := range insts {
		present[in.InstanceID] = true
		h.upsertHost(id, in)
	}
	// 标记 GCP 已无的为 stale
	stale := h.markStaleHosts(id, present)
	_, _ = h.DB.Exec(`UPDATE cloud_accounts SET last_sync_at=NOW(), last_result=? WHERE id=?`,
		truncate("同步 "+strconv.Itoa(len(insts))+" 台，失效 "+strconv.Itoa(stale), 250), id)
	WriteAudit(h.DB, c, "sync_cloud_account", id)
	c.JSON(http.StatusOK, gin.H{"synced": len(insts), "stale": stale})
}

func (h *HostHandler) upsertHost(accountID string, in cloudsource.Instance) {
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
	_, _ = h.DB.Exec(`UPDATE hosts SET project=?, project_name=?, zone=?, region=?, machine_type=?, vcpu=?, mem_mb=?, disk_total_gb=?,
		internal_ip=?, external_ip=?, status=?, os=?, labels=?, self_link=?, gcp_created_at=?, stale=0, synced_at=NOW() WHERE ci_id=?`,
		in.Project, in.ProjectName, in.Zone, in.Region, in.MachineType, in.VCPU, in.MemMB, total,
		in.InternalIP, in.ExternalIP, in.Status, in.OS, string(labelsJSON), in.SelfLink, created, ciID)
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

func (h *HostHandler) markStaleHosts(accountID string, present map[string]bool) int {
	rows, err := h.DB.Query(`SELECT ci_id, instance_id FROM hosts WHERE cloud_account_id=?`, accountID)
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
	Stale       bool              `json:"stale"`
	CreatedAt   string            `json:"gcp_created_at"`
	// 成本估算（USD）
	CostDaily  float64 `json:"cost_daily"`
	CostMonth  float64 `json:"cost_month"`
	CostTotal  float64 `json:"cost_total"`
	CostSource string  `json:"cost_source"` // estimate / bigquery
}

func (h *HostHandler) ListHosts(c *gin.Context) {
	rate := h.rate()
	// 各主机 SSD/标准盘容量（用于成本）
	ssd := map[int64]int{}
	std := map[int64]int{}
	drows, _ := h.DB.Query(`SELECT host_ci_id, type, size_gb FROM host_disks`)
	if drows != nil {
		for drows.Next() {
			var ci int64
			var typ string
			var sz int
			if drows.Scan(&ci, &typ, &sz) == nil {
				if strings.Contains(typ, "ssd") {
					ssd[ci] += sz
				} else {
					std[ci] += sz
				}
			}
		}
		drows.Close()
	}
	rows, err := h.DB.Query(`SELECT c.id, c.name, h.project, h.project_name, h.zone, h.region, h.machine_type, h.vcpu, h.mem_mb, h.disk_total_gb,
		h.internal_ip, h.external_ip, h.status, h.os, h.labels, h.stale, h.gcp_created_at, COALESCE(ca.name,'')
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
		var stale int
		if rows.Scan(&o.CIID, &o.Name, &o.Project, &o.ProjectName, &o.Zone, &o.Region, &o.MachineType, &o.VCPU, &o.MemMB, &o.DiskTotalGB,
			&o.InternalIP, &o.ExternalIP, &o.Status, &o.OS, &labels, &stale, &created, &o.AccountName); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		o.Stale = stale == 1
		if labels.Valid && labels.String != "" {
			_ = json.Unmarshal([]byte(labels.String), &o.Labels)
		}
		hourly := estHourly(o.VCPU, o.MemMB, ssd[o.CIID], std[o.CIID], o.Status, rate)
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
	var stale int
	err := h.DB.QueryRow(`SELECT c.id, c.name, h.project, h.project_name, h.zone, h.region, h.machine_type, h.vcpu, h.mem_mb, h.disk_total_gb,
		h.internal_ip, h.external_ip, h.status, h.os, h.labels, h.stale, h.gcp_created_at, COALESCE(ca.name,'')
		FROM cis c JOIN hosts h ON h.ci_id=c.id LEFT JOIN cloud_accounts ca ON ca.id=h.cloud_account_id
		WHERE c.id=? AND c.type='host'`, ciid).
		Scan(&o.CIID, &o.Name, &o.Project, &o.ProjectName, &o.Zone, &o.Region, &o.MachineType, &o.VCPU, &o.MemMB, &o.DiskTotalGB,
			&o.InternalIP, &o.ExternalIP, &o.Status, &o.OS, &labels, &stale, &created, &o.AccountName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "主机不存在"})
		return
	}
	o.Stale = stale == 1
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
	ssdGB, stdGB := 0, 0
	drows, _ := h.DB.Query(`SELECT name, size_gb, type, is_boot FROM host_disks WHERE host_ci_id=? ORDER BY is_boot DESC, id`, ciid)
	if drows != nil {
		for drows.Next() {
			var d disk
			var boot int
			if drows.Scan(&d.Name, &d.SizeGB, &d.Type, &boot) == nil {
				d.IsBoot = boot == 1
				if strings.Contains(d.Type, "ssd") {
					ssdGB += d.SizeGB
				} else {
					stdGB += d.SizeGB
				}
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
	// 成本
	rate := h.rate()
	hourly := estHourly(o.VCPU, o.MemMB, ssdGB, stdGB, o.Status, rate)
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
	})
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }
func round4(f float64) float64 { return float64(int64(f*10000+0.5)) / 10000 }

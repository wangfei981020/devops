package handlers

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/dnsource"
)

// dnsMigratedFromGoDaddy 查域名权威 NS：若能查到且都不是 GoDaddy(domaincontrol.com)，
// 说明域名 DNS 解析已迁到别处（GoDaddy 拉不到记录）。查不到 NS 时保守返回 false（不武断判迁移）。
func dnsMigratedFromGoDaddy(domain string) bool {
	nss, err := net.LookupNS(domain)
	if err != nil || len(nss) == 0 {
		return false
	}
	for _, ns := range nss {
		if strings.Contains(strings.ToLower(ns.Host), "domaincontrol.com") {
			return false // 仍托管在 GoDaddy
		}
	}
	return true
}

// SyncHandler 域名数据源同步（厂商 → DB）+ DNS 记录缓存读取 + API 用量。
type SyncHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewSyncHandler(db *sql.DB, cipher *crypto.Cipher) *SyncHandler {
	return &SyncHandler{DB: db, Cipher: cipher}
}

func (h *SyncHandler) Register(r *gin.RouterGroup) {
	r.POST("/sources/:id/sync", h.Sync)
	r.GET("/sources/:id/sync-status", h.SyncStatus)
	r.GET("/sources/:id/usage", h.Usage)
	r.GET("/domains/:ciid/dns-records", h.DNSRecords)
	r.POST("/domains/:ciid/sync-records", h.SyncDomainRecords)
}

// ---- 全量同步的后台状态（进程内，按数据源 id）----
type syncState struct {
	Running                            bool
	Total, Done, Synced, Records, Imp  int
	Stale                              int
	Err                                string
	StartedAt, FinishedAt              time.Time
}

var (
	syncMu    sync.Mutex
	syncStore = map[int]*syncState{}
)

// SyncDomainRecords 单个域名从其绑定数据源拉 A/CNAME，刷 DNS 记录缓存 + 导入/更新业务台账。
func (h *SyncHandler) SyncDomainRecords(c *gin.Context) {
	ciid := c.Param("ciid")
	var name string
	var sourceID sql.NullInt64
	if err := h.DB.QueryRow(`SELECT c.name, d.registrar_id FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.id=? AND c.type='domain'`, ciid).Scan(&name, &sourceID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "域名不存在"})
		return
	}
	if !sourceID.Valid {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该域名未绑定数据源，请先在「编辑」里选择数据源/注册商"})
		return
	}
	id := int(sourceID.Int64)
	provider, cred, err := LoadCredential(h.DB, h.Cipher, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "数据源凭据读取失败"})
		return
	}
	adapter, err := dnsource.NewAdapter(provider, cred, dnsource.LimiterFor(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	recs, err := adapter.ListRecords(ctx, name)
	if rle := asRateLimit(err); rle != nil {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": rle.Error(), "rate_limit": rle.Info})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	ciIDInt, _ := parseID(ciid)
	h.refreshDNSRecords(ciIDInt, id, recs)
	imported := h.importBusinessRecords(ciIDInt, recs)
	migrated := 0
	if len(recs) == 0 && dnsMigratedFromGoDaddy(name) {
		migrated = 1
	}
	_, _ = h.DB.Exec(`UPDATE domains SET last_synced_at=NOW(), dns_migrated=? WHERE ci_id=?`, migrated, ciIDInt)
	WriteAudit(h.DB, c, "sync_domain_records", name)
	c.JSON(http.StatusOK, gin.H{"ok": true, "synced_records": len(recs), "imported_records": imported})
}

// Usage 某数据源的 API 客户端限流用量。
func (h *SyncHandler) Usage(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	c.JSON(http.StatusOK, dnsource.LimiterFor(id).Stats())
}

// Sync 同步某数据源：拉域名 + 每个域名的 DNS 记录，写入/更新 DB（受客户端限流）。
// Sync 全量同步一个数据源：改为**后台异步**（域名多、限流节流下需 1-2 分钟，避免 HTTP 超时/429）。
// 立即返回 202，前端轮询 sync-status 看进度。
func (h *SyncHandler) Sync(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	syncMu.Lock()
	if st := syncStore[id]; st != nil && st.Running {
		syncMu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "该数据源正在同步中，请稍候"})
		return
	}
	syncMu.Unlock()

	provider, cred, err := LoadCredential(h.DB, h.Cipher, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "数据源不存在或凭据读取失败"})
		return
	}
	adapter, err := dnsource.NewAdapter(provider, cred, dnsource.LimiterFor(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	st := &syncState{Running: true, StartedAt: time.Now()}
	syncMu.Lock()
	syncStore[id] = st
	syncMu.Unlock()
	WriteAudit(h.DB, c, "sync_source", c.Param("id"))
	go h.runSync(id, adapter, st)
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "running": true, "msg": "已在后台同步，域名较多约 1-2 分钟，完成后自动刷新"})
}

// runSync 后台跑全量同步：限流节流(Wait)下完整拉全部域名+记录，最后 markStale。
func (h *SyncHandler) runSync(id int, adapter dnsource.Adapter, st *syncState) {
	defer func() {
		syncMu.Lock()
		st.Running = false
		st.FinishedAt = time.Now()
		syncMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	domains, err := adapter.ListDomains(ctx)
	if err != nil {
		syncMu.Lock()
		st.Err = err.Error()
		syncMu.Unlock()
		return
	}
	syncMu.Lock()
	st.Total = len(domains)
	syncMu.Unlock()

	ignoredSet := h.ignoredDomainSet(id) // 已忽略的域名同步时跳过
	present := map[string]bool{}
	for _, d := range domains {
		if ignoredSet[d.Name] {
			syncMu.Lock()
			st.Done++
			syncMu.Unlock()
			continue
		}
		ciID, err := h.upsertDomainCI(d.Name, id, d.ExpiresAt)
		if err != nil {
			syncMu.Lock()
			st.Done++
			syncMu.Unlock()
			continue
		}
		present[d.Name] = true
		recs, err := adapter.ListRecords(ctx, d.Name)
		if err == nil {
			h.refreshDNSRecords(ciID, id, recs)
			imp := h.importBusinessRecords(ciID, recs)
			// 无论有无记录都记同步时刻（0 记录也算已同步，避免误报"未同步"）
			// 记录 0 条时查权威 NS：若已不指向 GoDaddy，说明域名还在账户但 DNS 迁走了。
			migrated := 0
			if len(recs) == 0 && dnsMigratedFromGoDaddy(d.Name) {
				migrated = 1
			}
			_, _ = h.DB.Exec(`UPDATE domains SET last_synced_at=NOW(), dns_migrated=? WHERE ci_id=?`, migrated, ciID)
			syncMu.Lock()
			st.Synced++
			st.Records += len(recs)
			st.Imp += imp
			syncMu.Unlock()
		}
		syncMu.Lock()
		st.Done++
		syncMu.Unlock()
	}
	// 完整拉全后才标记：该数据源下 GoDaddy 已不存在的主域名标失效（保留业务信息，人工确认删）
	stale := h.markStaleDomains(id, present)
	syncMu.Lock()
	st.Stale = stale
	syncMu.Unlock()
}

// SyncStatus 查某数据源后台同步进度（前端轮询）。
func (h *SyncHandler) SyncStatus(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	syncMu.Lock()
	st := syncStore[id]
	if st == nil {
		syncMu.Unlock()
		c.JSON(http.StatusOK, gin.H{"running": false, "started": false})
		return
	}
	out := gin.H{
		"running": st.Running, "started": true,
		"total": st.Total, "done": st.Done,
		"synced_domains": st.Synced, "synced_records": st.Records, "imported_records": st.Imp,
		"stale_domains": st.Stale, "error": st.Err,
	}
	if !st.FinishedAt.IsZero() {
		out["finished_at"] = st.FinishedAt.Format("2006-01-02 15:04:05")
	}
	syncMu.Unlock()
	c.JSON(http.StatusOK, out)
}

// DNSRecords 读某域名的厂商原始 DNS 记录（来自同步缓存 dns_records）。
func (h *SyncHandler) DNSRecords(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT id, type, name, data, ttl, priority, protected, synced_at
		FROM dns_records WHERE domain_ci_id=? ORDER BY type, name`, c.Param("ciid"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type rec struct {
		ID        int64  `json:"id"`
		Type      string `json:"type"`
		Name      string `json:"name"`
		Data      string `json:"data"`
		TTL       int    `json:"ttl"`
		Priority  *int   `json:"priority"`
		Protected bool   `json:"protected"`
		SyncedAt  string `json:"synced_at"`
	}
	out := []rec{}
	for rows.Next() {
		var r rec
		var prio sql.NullInt64
		var prot int
		var synced time.Time
		if rows.Scan(&r.ID, &r.Type, &r.Name, &r.Data, &r.TTL, &prio, &prot, &synced) == nil {
			if prio.Valid {
				v := int(prio.Int64)
				r.Priority = &v
			}
			r.Protected = prot == 1
			r.SyncedAt = synced.Format("2006-01-02 15:04")
			out = append(out, r)
		}
	}
	c.JSON(http.StatusOK, out)
}

// ---- helpers ----

func (h *SyncHandler) upsertDomainCI(name string, sourceID int, expires *time.Time) (int64, error) {
	var ciID int64
	err := h.DB.QueryRow(`SELECT id FROM cis WHERE type='domain' AND name=?`, name).Scan(&ciID)
	if err == sql.ErrNoRows {
		res, e := h.DB.Exec(`INSERT INTO cis (type, name, status) VALUES ('domain', ?, 'active')`, name)
		if e != nil {
			return 0, e
		}
		ciID, _ = res.LastInsertId()
	} else if err != nil {
		return 0, err
	}
	var exp any
	if expires != nil {
		exp = *expires
	}
	_, _ = h.DB.Exec(`INSERT INTO domains (ci_id, registrar_id, expiry_at, origin) VALUES (?, ?, ?, 'sync')
		ON DUPLICATE KEY UPDATE registrar_id=VALUES(registrar_id), expiry_at=VALUES(expiry_at), stale=0, origin='sync'`, ciID, sourceID, exp)
	return ciID, nil
}

// ignoredDomainSet 取某数据源下被忽略的主域名名集合（同步时跳过）。
func (h *SyncHandler) ignoredDomainSet(sourceID int) map[string]bool {
	set := map[string]bool{}
	rows, err := h.DB.Query(`SELECT c.name FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.type='domain' AND d.registrar_id=? AND d.ignored=1`, sourceID)
	if err != nil {
		return set
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			set[n] = true
		}
	}
	return set
}

// markStaleDomains 把某数据源下、本次同步未出现(GoDaddy 已无)的主域名标为失效；返回标记条数。
func (h *SyncHandler) markStaleDomains(sourceID int, present map[string]bool) int {
	rows, err := h.DB.Query(`SELECT c.id, c.name FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.type='domain' AND d.registrar_id=? AND d.ignored=0`, sourceID)
	if err != nil {
		return 0
	}
	defer rows.Close()
	var stale []int64
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) == nil && !present[name] {
			stale = append(stale, id)
		}
	}
	for _, id := range stale {
		_, _ = h.DB.Exec(`UPDATE domains SET stale=1 WHERE ci_id=?`, id)
	}
	return len(stale)
}

// importBusinessRecords 把厂商的 A/CNAME 自动导入业务台账(domain_records)。
// 按 (host, record_type) 聚合：A 多 IP 逗号拼接、CNAME 取值。已存在的只刷厂商字段(源站IP/回源CNAME)，
// 保留人工填的项目/环境/模块/CDN/证书；不存在的新建。受保护(_acme-challenge/NS)与非 A/CNAME 跳过。
// 返回新建条数。
func (h *SyncHandler) importBusinessRecords(ciID int64, recs []dnsource.DNSRecord) int {
	type biz struct{ host, rtype, originIP, cname string }
	agg := map[string]*biz{}
	order := []string{}
	for _, r := range recs {
		if (r.Type != "A" && r.Type != "CNAME") || isProtectedRecord(r) {
			continue
		}
		key := r.Name + "|" + r.Type
		b := agg[key]
		if b == nil {
			b = &biz{host: r.Name, rtype: r.Type}
			agg[key] = b
			order = append(order, key)
		}
		if r.Type == "A" {
			if b.originIP == "" {
				b.originIP = r.Data
			} else {
				b.originIP += "," + r.Data
			}
		} else {
			b.cname = r.Data
		}
	}
	created := 0
	present := map[string]bool{}
	for _, key := range order {
		b := agg[key]
		present[b.host+"|"+b.rtype] = true
		var id int64
		err := h.DB.QueryRow(`SELECT id FROM domain_records WHERE domain_ci_id=? AND host=? AND record_type=?`,
			ciID, b.host, b.rtype).Scan(&id)
		if err == sql.ErrNoRows {
			_, _ = h.DB.Exec(`INSERT INTO domain_records (domain_ci_id, host, record_type, origin_ip, cname, operator)
				VALUES (?, ?, ?, ?, ?, 'godaddy同步')`, ciID, b.host, b.rtype, b.originIP, b.cname)
			created++
		} else if err == nil {
			// 只刷厂商字段，业务字段(项目/环境/模块/CDN/证书)原样保留；重新出现则取消失效标记。
			// origin_ip：同步值为空(CNAME 记录)时保留人工手填的源站IP，非空(A 记录)才用厂商值覆盖。
			_, _ = h.DB.Exec(`UPDATE domain_records SET origin_ip=IF(?='', origin_ip, ?), cname=?, stale=0 WHERE id=?`,
				b.originIP, b.originIP, b.cname, id)
		}
	}
	// 厂商已删除：本次未出现的 A/CNAME 标为失效（保留业务字段，由人工确认后删）
	rows, err := h.DB.Query(`SELECT id, host, record_type FROM domain_records WHERE domain_ci_id=? AND record_type IN ('A','CNAME')`, ciID)
	if err == nil {
		defer rows.Close()
		var stale []int64
		for rows.Next() {
			var id int64
			var host, rtype string
			if rows.Scan(&id, &host, &rtype) == nil && !present[host+"|"+rtype] {
				stale = append(stale, id)
			}
		}
		for _, id := range stale {
			_, _ = h.DB.Exec(`UPDATE domain_records SET stale=1 WHERE id=?`, id)
		}
	}
	return created
}

func (h *SyncHandler) refreshDNSRecords(ciID int64, sourceID int, recs []dnsource.DNSRecord) {
	_, _ = h.DB.Exec(`DELETE FROM dns_records WHERE domain_ci_id=?`, ciID)
	for _, r := range recs {
		var prio any
		if r.Priority != nil {
			prio = *r.Priority
		}
		_, _ = h.DB.Exec(`INSERT INTO dns_records (domain_ci_id, type, name, data, ttl, priority, protected, source_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			ciID, r.Type, r.Name, r.Data, r.TTL, prio, boolToInt(isProtectedRecord(r)), sourceID)
	}
}

// isProtectedRecord 标记不可在 CMDB 误改的记录：CF 的 _acme-challenge 委托、NS 等。
func isProtectedRecord(r dnsource.DNSRecord) bool {
	if r.Type == "NS" || r.Type == "SOA" {
		return true
	}
	if strings.HasPrefix(r.Name, "_acme-challenge") {
		return true
	}
	return false
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func asRateLimit(err error) *dnsource.RateLimitError {
	var rle *dnsource.RateLimitError
	if errors.As(err, &rle) {
		return rle
	}
	return nil
}

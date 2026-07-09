package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log"
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
		if isDomainGone(d.Status) {
			// 已转出/取消/过户：不计 present、不重置 stale，直接标已移出账号并记状态，交由灰显+人工移除
			h.markDomainGone(d.Name, id, d.Status)
			log.Printf("[domain-sync] 域名 %s 判为已移出账号（GoDaddy status=%s）", d.Name, d.Status)
			syncMu.Lock()
			st.Done++
			syncMu.Unlock()
			continue
		}
		// 未识别状态（既非活跃/待激活，也不在移出清单）——暂按活跃保留，但打 WARN，便于补分类
		if !isDomainActive(d.Status) && !isDomainPending(d.Status) {
			log.Printf("[domain-sync] WARN 域名 %s 状态未识别（GoDaddy status=%s），暂按活跃处理，请确认是否应判移出", d.Name, d.Status)
		}
		ciID, err := h.upsertDomainCI(d.Name, id, d.ExpiresAt, d.Status)
		if err != nil {
			log.Printf("[domain-sync] WARN 域名 %s upsert 失败: %v", d.Name, err)
			syncMu.Lock()
			st.Done++
			syncMu.Unlock()
			continue
		}
		present[d.Name] = true
		recs, err := adapter.ListRecords(ctx, d.Name)
		if err != nil {
			log.Printf("[domain-sync] WARN 域名 %s 拉解析记录失败（可能转出中/DNS已迁走）: %v", d.Name, err)
		} else {
			h.refreshDNSRecords(ciID, id, recs)
			imp := h.importBusinessRecords(ciID, recs)
			// 记录 0 条时查权威 NS：若已不指向 GoDaddy，说明域名还在账户但 DNS 迁走了。
			migrated := 0
			if len(recs) == 0 && dnsMigratedFromGoDaddy(d.Name) {
				migrated = 1
			}
			_, _ = h.DB.Exec(`UPDATE domains SET dns_migrated=? WHERE ci_id=?`, migrated, ciID)
			syncMu.Lock()
			st.Synced++
			st.Records += len(recs)
			st.Imp += imp
			syncMu.Unlock()
		}
		// 只要这次扫到了就更新同步时刻（不管 records 成败），消除假"24h未同步"
		_, _ = h.DB.Exec(`UPDATE domains SET last_synced_at=NOW() WHERE ci_id=?`, ciID)
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

// goneDomainStatuses GoDaddy 返回但已不再属于本账户(转出/取消/过户/没收等)的状态。
// 这些状态的域名 API 仍会返回一段时间，需当作"已移出账号"处理，而不是正常活跃域名。
// 实测(csc5002)真实值：TRANSFERRED_OUT / CANCELLED / UPDATED_OWNERSHIP。
var goneDomainStatuses = map[string]bool{
	"TRANSFERRED_OUT":           true, // 已转出（实测真实值）
	"TRANSFERRED":               true,
	"USER_TRANSFER_OUT":         true,
	"CANCELLED":                 true,
	"CANCELLED_REDEEMABLE":      true,
	"UPDATED_OWNERSHIP":         true, // 已过户（所有权变更走了）
	"CONFISCATED":               true,
	"EXCLUDED":                  true,
	"FAILED":                    true,
	"NAME_CANNOT_BE_REGISTERED": true,
}

// isDomainGone 判断数据源状态是否表示"已移出账号"(转出/取消/过户/没收)。
func isDomainGone(status string) bool {
	s := strings.ToUpper(strings.TrimSpace(status))
	if s == "" {
		return false
	}
	if goneDomainStatuses[s] {
		return true
	}
	return strings.Contains(s, "TRANSFER") && strings.Contains(s, "OUT") // 兜底：任何"转出"变体(TRANSFER_OUT / TRANSFERRED_OUT)
}

// isDomainPending 待激活/在途状态(新买待验证、DNS 未激活等)——仍算在管，单独归类，不判移出。
func isDomainPending(status string) bool {
	s := strings.ToUpper(strings.TrimSpace(status))
	return strings.HasPrefix(s, "PENDING") || strings.HasPrefix(s, "AWAITING")
}

// isDomainActive 明确的活跃状态。空(手动录入)也按活跃。
func isDomainActive(status string) bool {
	s := strings.ToUpper(strings.TrimSpace(status))
	return s == "" || s == "ACTIVE"
}

// domainCategory 综合各字段算出展示分类，前端据此上色/筛选。
func domainCategory(sourceStatus string, stale, dnsMigrated, ignored, expiryPast bool) string {
	if ignored {
		return "ignored"
	}
	s := strings.ToUpper(strings.TrimSpace(sourceStatus))
	if stale {
		switch {
		case strings.Contains(s, "TRANSFER") && strings.Contains(s, "OUT"), s == "TRANSFERRED":
			return "transferred_out"
		case s == "CANCELLED" || s == "CANCELLED_REDEEMABLE":
			return "cancelled"
		case s == "UPDATED_OWNERSHIP":
			return "ownership"
		default:
			return "removed" // 从 GoDaddy 列表彻底消失 / 其它移出
		}
	}
	if dnsMigrated {
		return "dns_migrated"
	}
	if expiryPast {
		return "expired"
	}
	if isDomainPending(s) {
		return "pending"
	}
	if isDomainActive(s) {
		return "active"
	}
	return "unknown"
}

// markDomainGone 把已存在的域名标记为移出账号(stale=1)并记下数据源状态；不重置 stale、不碰业务信息。
// 域名首次出现即为消亡态(库里还没有)时无需处理——本就没纳管，不显示。
func (h *SyncHandler) markDomainGone(name string, sourceID int, status string) {
	_, _ = h.DB.Exec(`UPDATE domains d JOIN cis c ON c.id=d.ci_id
		SET d.stale=1, d.source_status=?
		WHERE c.type='domain' AND c.name=? AND d.registrar_id=? AND d.ignored=0`, status, name, sourceID)
}

func (h *SyncHandler) upsertDomainCI(name string, sourceID int, expires *time.Time, status string) (int64, error) {
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
	_, _ = h.DB.Exec(`INSERT INTO domains (ci_id, registrar_id, expiry_at, origin, source_status) VALUES (?, ?, ?, 'sync', ?)
		ON DUPLICATE KEY UPDATE registrar_id=VALUES(registrar_id), expiry_at=VALUES(expiry_at), stale=0, origin='sync', source_status=VALUES(source_status)`, ciID, sourceID, exp, status)
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

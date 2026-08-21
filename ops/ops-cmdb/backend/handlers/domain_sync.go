package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/dnsource"
	"ops-cmdb-backend/internal/cluster"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
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
	Store *store.Store
	// Mu 跨副本互斥。续费是非幂等写，进程内的锁在多副本下形同虚设。
	Mu     *cluster.Mutex
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewSyncHandler(st *store.Store, db *sql.DB, cipher *crypto.Cipher) *SyncHandler {
	return &SyncHandler{Store: st, Mu: cluster.NewMutex(st), DB: db, Cipher: cipher}
}

func (h *SyncHandler) Register(r *gin.RouterGroup) {
	r.POST("/sources/:id/sync", h.Sync)
	r.GET("/sources/:id/sync-status", h.SyncStatus)
	r.GET("/sources/:id/usage", h.Usage)
	r.GET("/domains/:ciid/dns-records", h.DNSRecords)
	r.POST("/domains/:ciid/sync-records", h.SyncDomainRecords)
	// DNS 解析写回厂商（增/改/删）
	r.POST("/domains/:ciid/dns-records", h.CreateDNSRecord)
	r.POST("/domains/:ciid/dns-records/batch", h.BatchCreateDNSRecord)
	r.POST("/domains/:ciid/dns-records/batch-delete", h.BatchDeleteDNSRecords)
	r.POST("/domains/:ciid/dns-records/batch-update", h.BatchUpdateDNSRecords)
	r.PUT("/dns-records/:id", h.UpdateDNSRecord)
	r.DELETE("/dns-records/:id", h.DeleteDNSRecord)
	// 域名续费 / 自动续费（写回厂商）
	r.GET("/domains/:ciid/godaddy-detail", h.GodaddyDetail)
	r.POST("/domains/:ciid/renew", h.RenewDomain)
	// 批量续费：先 preview 看清楚哪些能续，再执行（真金白银，不做一步到位）
	r.POST("/domains/renew-batch/preview", h.PreviewBatchRenew)
	r.POST("/domains/renew-batch", h.BatchRenewDomains)
	r.GET("/domains/renew-batch/:id", h.BatchRenewStatus) // 轮询后台任务进度
	r.POST("/domains/:ciid/auto-renew", h.SetAutoRenew)
	r.GET("/renewals", h.ListRenewals) // 续费记录历史
}

// 同步进度存在库里（sync_progress 表），见 handlers/sync_progress.go。
//
//	⚠️ 这里原本是一个进程内的 map。多副本下前端轮询会打到
//	**没跑这次同步的那个 Pod**，界面显示"没在同步"而实际正跑着，
//	用户以为失败又点一次。互斥判据同样只在单 Pod 内有效。
//	两个症状都只在扩容后出现 —— 本地怎么点都是对的。

// SyncDomainRecords 单个域名从其绑定数据源拉 A/CNAME，刷 DNS 记录缓存 + 导入/更新业务台账。
func (h *SyncHandler) SyncDomainRecords(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ciid := c.Param("ciid")
	var name string
	var sourceID sql.NullInt64
	if err := sc.QueryRow(`SELECT c.name, d.registrar_id FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.tenant_id = ? AND c.id=? AND c.type='domain'`, ciid).Scan(&name, &sourceID); err != nil {
		httpx.NotFound(c, "domain")
		return
	}
	if !sourceID.Valid {
		httpx.FailKey(c, httpx.CodeBadRequest, "error.domainNoSource", nil, nil)
		return
	}
	id := int(sourceID.Int64)
	provider, cred, err := LoadCredential(h.DB, h.Cipher, id)
	if err != nil {
		httpx.FailKey(c, httpx.CodeInternal, "error.sourceCredReadFailed", nil, nil)
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
	imported := h.importBusinessRecords(sc, ciIDInt, name, recs)
	migrated := 0
	if len(recs) == 0 && dnsMigratedFromGoDaddy(name) {
		migrated = 1
	}
	logExec(h.DB, "DNS同步写", `UPDATE domains SET last_synced_at=NOW(), dns_migrated=? WHERE ci_id=?`, migrated, ciIDInt)
	SetAuditTarget(c, name)
	c.JSON(http.StatusOK, gin.H{"ok": true, "synced_records": len(recs), "imported_records": len(imported), "new_records": imported})
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
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))

	provider, cred, err := LoadCredential(h.DB, h.Cipher, id)
	if err != nil {
		httpx.FailKey(c, httpx.CodeInternal, "error.sourceMissingOrCredFailed", nil, nil)
		return
	}
	adapter, err := dnsource.NewAdapter(provider, cred, dnsource.LimiterFor(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// 占位与互斥都在库里完成：跨副本可见，且能接管"跑到一半被杀"的记录。
	ok, err := beginSync(sc, id, replicaOwner())
	if err != nil {
		httpx.FailKey(c, httpx.CodeInternal, "error.syncRegisterFailed", err, nil)
		return
	}
	if !ok {
		httpx.FailKey(c, httpx.CodeConflict, "error.sourceSyncInProgress", nil, nil)
		return
	}
	SetAuditTarget(c, c.Param("id"))
	go h.runSync(sc.TenantID(), id, adapter)
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "running": true, "msg_key": "domains:sync.started",
		// msg 保留给 MCP / 直接调 API 的人
		"msg": "已在后台同步，域名较多约 1-2 分钟，完成后自动刷新"})
}

// runSync 后台跑全量同步：限流节流(Wait)下完整拉全部域名+记录，最后 markStale。
// 同时写「执行记录」(task_run_logs, key=dns_sync, trigger=manual)：含新增解析列表、真失败明细、DNS已迁走计数。
// runSync 后台同步。tenant 由调用方从请求里取出传入 —— goroutine 里
// 请求上下文已取消，不能在这里现取。
func (h *SyncHandler) runSync(tenant store.TenantID, id int, adapter dnsource.Adapter) {
	sc, serr := h.Store.Tenant(store.ForJob(context.Background(), tenant, "dns_sync"))
	if serr != nil {
		logx.J("dns_sync", "scope_fail", map[string]any{"tenant_id": int64(tenant), "err": serr.Error()})
		return
	}
	start := time.Now()
	var srcName string
	_ = sc.QueryRow(`SELECT name FROM registrars WHERE tenant_id = ? AND id=?`, id).Scan(&srcName)
	var runID int64
	if sched != nil {
		runID = sched.startRunLog("dns_sync", "manual", start)
	}
	var failures []TaskFailure
	var newRecList []string
	migratedCnt := 0
	defer func() {
		p, _, _ := loadSyncProgress(sc, id)
		errStr := p.Err
		finishSync(sc, id, errStr)
		// 写执行记录终态
		if sched != nil && runID > 0 {
			status := taskStatusOK
			if errStr != "" {
				status = taskStatusFail
			} else if len(failures) > 0 {
				status = taskStatusPartial
			}
			summary := fmt.Sprintf("手动同步「%s」：%d 域名 / %d 条解析 / 新增 %d 条", srcName, p.Synced, p.Records, p.Imported)
			if migratedCnt > 0 {
				summary += fmt.Sprintf(" / %d 个DNS已迁走(Cloudflare等,正常)", migratedCnt)
			}
			if errStr != "" {
				summary += " / 出错：" + truncate(errStr, 120)
			}
			if len(newRecList) > 0 {
				summary += "\n新增业务解析："
				for i, r := range newRecList {
					if i >= 10 {
						summary += fmt.Sprintf("\n…另 %d 条", len(newRecList)-10)
						break
					}
					summary += "\n· " + r
				}
			}
			sched.finishRunLog(runID, status, summary, failures, nil, start, "", "", "")
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	domains, err := adapter.ListDomains(ctx)
	if err != nil {
		finishSync(sc, id, err.Error())
		return
	}
	bumpSync(sc, id, func(d *syncProgress) { d.Total = len(domains) })

	ignoredSet := h.ignoredDomainSet(id) // 已忽略的域名同步时跳过
	present := map[string]bool{}
	for _, d := range domains {
		if ignoredSet[d.Name] {
			bumpSync(sc, id, func(d *syncProgress) { d.Done = 1 })
			continue
		}
		if isDomainGone(d.Status) {
			// 已转出/取消/过户：不计 present、不重置 stale，直接标已移出账号并记状态，交由灰显+人工移除
			h.markDomainGone(d.Name, id, d.Status)
			logx.Line("domain-sync", fmt.Sprintf("[domain-sync] 域名 %s 判为已移出账号（GoDaddy status=%s）", d.Name, d.Status))
			bumpSync(sc, id, func(d *syncProgress) { d.Done = 1 })
			continue
		}
		// 未识别状态（既非活跃/待激活，也不在移出清单）——暂按活跃保留，但打 WARN，便于补分类
		if !isDomainActive(d.Status) && !isDomainPending(d.Status) {
			logx.Line("domain-sync", fmt.Sprintf("[domain-sync] WARN 域名 %s 状态未识别（GoDaddy status=%s），暂按活跃处理，请确认是否应判移出", d.Name, d.Status))
		}
		ciID, err := h.upsertDomainCI(d.Name, id, d.ExpiresAt, d.Status)
		if err != nil {
			logx.Line("domain-sync", fmt.Sprintf("[domain-sync] WARN 域名 %s upsert 失败: %v", d.Name, err))
			bumpSync(sc, id, func(d *syncProgress) { d.Done = 1 })
			continue
		}
		present[d.Name] = true
		recs, err := adapter.ListRecords(ctx, d.Name)
		if err != nil {
			// 区分 DNS 已迁走(Cloudflare 等，正常) 与真失败：迁走计数，真失败进执行记录 failures
			if mig, reason := classifyRecordFetchErr(d.Name, err); mig {
				migratedCnt++
				logExec(h.DB, "DNS同步写", `UPDATE domains SET dns_migrated=1 WHERE ci_id=?`, ciID)
			} else {
				failures = append(failures, TaskFailure{Target: d.Name, Reason: "拉解析失败：" + reason})
			}
		} else {
			h.refreshDNSRecords(ciID, id, recs)
			imp := h.importBusinessRecords(sc, ciID, d.Name, recs)
			newRecList = append(newRecList, imp...)
			// 记录 0 条时查权威 NS：若已不指向 GoDaddy，说明域名还在账户但 DNS 迁走了。
			migrated := 0
			if len(recs) == 0 && dnsMigratedFromGoDaddy(d.Name) {
				migrated = 1
				migratedCnt++
			}
			logExec(h.DB, "DNS同步写", `UPDATE domains SET dns_migrated=? WHERE ci_id=?`, migrated, ciID)
			bumpSync(sc, id, func(d *syncProgress) {
				d.Synced, d.Records, d.Imported = 1, len(recs), len(imp)
			})
		}
		// 只要这次扫到了就更新同步时刻（不管 records 成败），消除假"24h未同步"
		logExec(h.DB, "DNS同步写", `UPDATE domains SET last_synced_at=NOW() WHERE ci_id=?`, ciID)
		bumpSync(sc, id, func(d *syncProgress) { d.Done = 1 })
	}
	// 完整拉全后才标记：该数据源下 GoDaddy 已不存在的主域名标失效（保留业务信息，人工确认删）
	setSyncStale(sc, id, h.markStaleDomains(id, present))
}

// SyncStatus 查某数据源后台同步进度（前端轮询）。
func (h *SyncHandler) SyncStatus(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	id, _ := strconv.Atoi(c.Param("id"))
	p, found, err := loadSyncProgress(sc, id)
	if err != nil {
		// ⚠️ 查失败不能返回 running:false —— 那和"没在同步"长得一样，
		//    用户会以为同步没跑起来而重复点。必须报错让前端显示失败态。
		httpx.FailKey(c, httpx.CodeInternal, "error.readSyncProgressFailed", err, nil)
		return
	}
	if !found {
		c.JSON(http.StatusOK, gin.H{"running": false, "started": false})
		return
	}
	live := p.live(time.Now())
	out := gin.H{
		"running": live, "started": true,
		"total": p.Total, "done": p.Done,
		"synced_domains": p.Synced, "synced_records": p.Records, "imported_records": p.Imported,
		"stale_domains": p.Stale, "error": p.Err,
		"replica": p.Owner, // 排障时能直接对上 Pod 日志
	}
	// running=1 但心跳早停了：跑它的副本已经死了。
	// 如实说出来，而不是显示成"同步中"（那会让这个数据源再也点不动）
	// 也不是显示成"没在同步"（那会掩盖掉这次同步其实中断了）。
	if p.Running && !live {
		out["interrupted"] = true
		if p.Err == "" {
			out["error"] = "同步中断（执行它的副本已退出）"
		}
	}
	if p.FinishedAt.Valid {
		out["finished_at"] = p.FinishedAt.Time.Format("2006-01-02 15:04:05")
	}
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
	logExec(h.DB, "DNS同步写", `UPDATE domains d JOIN cis c ON c.id=d.ci_id
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
	logExec(h.DB, "DNS同步写", `INSERT INTO domains (ci_id, registrar_id, expiry_at, origin, source_status) VALUES (?, ?, ?, 'sync', ?)
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
		logExec(h.DB, "DNS同步写", `UPDATE domains SET stale=1 WHERE ci_id=?`, id)
	}
	return len(stale)
}

// importBusinessRecords 把厂商的 A/CNAME 自动导入业务台账(domain_records)。
// 按 (host, record_type) 聚合：A 多 IP 逗号拼接、CNAME 取值。已存在的只刷厂商字段(源站IP/回源CNAME)，
// 保留人工填的项目/环境/模块/CDN/证书；不存在的新建。受保护(_acme-challenge/NS)与非 A/CNAME 跳过。
// 返回新建条数。
// importBusinessRecords 返回本次新增的业务解析展示串（FQDN + 类型 + 值），供同步摘要列出。
func (h *SyncHandler) importBusinessRecords(sc *store.Scoped, ciID int64, domainName string, recs []dnsource.DNSRecord) []string {
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
	var created []string
	present := map[string]bool{}
	for _, key := range order {
		b := agg[key]
		present[b.host+"|"+b.rtype] = true
		var id int64
		err := sc.QueryRow(`SELECT id FROM domain_records WHERE tenant_id = ? AND domain_ci_id=? AND host=? AND record_type=?`,
			ciID, b.host, b.rtype).Scan(&id)
		if err == sql.ErrNoRows {
			// 下划线服务记录（如 _domainconnect 的 Domain Connect CNAME）非业务主机，新导入默认忽略；仅 INSERT 时设，不覆盖人工取消。
			autoIgnore := strings.HasPrefix(b.host, "_")
			ig, igReason := 0, ""
			if autoIgnore {
				ig, igReason = 1, "下划线服务记录，自动忽略"
			}
			if _, e := sc.Insert(`INSERT INTO domain_records (tenant_id, domain_ci_id, host, record_type, origin_ip, cname, operator, ignored, ignore_reason)
				VALUES (?, ?, ?, ?, ?, ?, 'godaddy同步', ?, ?)`, ciID, b.host, b.rtype, b.originIP, b.cname, ig, igReason); e != nil {
				logx.J("dns_sync", "insert_record_fail", map[string]any{"ci_id": ciID, "host": b.host, "err": e.Error()})
			}
			if !autoIgnore { // 自动忽略的不算"新增业务解析"，不进同步摘要
				val := b.originIP
				if b.rtype == "CNAME" {
					val = b.cname
				}
				created = append(created, fmt.Sprintf("%s (%s → %s)", recordFQDN(b.host, domainName), b.rtype, val))
			}
		} else if err == nil {
			// 只刷厂商字段，业务字段(项目/环境/模块/CDN/证书)原样保留；重新出现则取消失效标记。
			// origin_ip：同步值为空(CNAME 记录)时保留人工手填的源站IP，非空(A 记录)才用厂商值覆盖。
			if _, e := sc.Exec(`UPDATE domain_records SET origin_ip=IF(?='', origin_ip, ?), cname=?, stale=0 WHERE tenant_id = ? AND id=?`,
				b.originIP, b.originIP, b.cname, id); e != nil {
				logx.J("dns_sync", "update_record_fail", map[string]any{"id": id, "err": e.Error()})
			}
		}
	}
	// 厂商已删除：本次未出现的 A/CNAME 标为失效（保留业务字段，由人工确认后删）
	rows, err := sc.Query(`SELECT id, host, record_type FROM domain_records WHERE tenant_id = ? AND domain_ci_id=? AND record_type IN ('A','CNAME')`, ciID)
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
			if _, e := sc.Exec(`UPDATE domain_records SET stale=1 WHERE tenant_id = ? AND id=?`, id); e != nil {
				logx.J("dns_sync", "mark_stale_fail", map[string]any{"id": id, "err": e.Error()})
			}
		}
	}
	return created
}

// ← importBusinessRecords 返回值改为新增解析展示串列表

func (h *SyncHandler) refreshDNSRecords(ciID int64, sourceID int, recs []dnsource.DNSRecord) {
	logExec(h.DB, "DNS同步写", `DELETE FROM dns_records WHERE domain_ci_id=?`, ciID)
	for _, r := range recs {
		var prio any
		if r.Priority != nil {
			prio = *r.Priority
		}
		logExec(h.DB, "DNS同步写", `INSERT INTO dns_records (domain_ci_id, type, name, data, ttl, priority, protected, source_id)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			ciID, r.Type, r.Name, r.Data, r.TTL, prio, boolToInt(isProtectedRecord(r)), sourceID)
	}
}

// classifyRecordFetchErr 区分「拉解析失败」是 DNS 已迁走(正常) 还是真失败。
// GoDaddy 对 DNS 迁到 Cloudflare 等的域名返回 UNKNOWN_DOMAIN(404)：若权威 NS 已不指向 GoDaddy，
// 判为已迁移(正常，不算失败)；否则算真失败。返回 (migrated, failReason)。
func classifyRecordFetchErr(domain string, err error) (migrated bool, failReason string) {
	msg := err.Error()
	is404 := strings.Contains(msg, "UNKNOWN_DOMAIN") || strings.Contains(msg, "404") ||
		strings.Contains(strings.ToLower(msg), "not registered") || strings.Contains(strings.ToLower(msg), "zone file")
	if is404 && dnsMigratedFromGoDaddy(domain) {
		return true, ""
	}
	return false, truncate(msg, 120)
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

package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/dnsource"
	"opsplatform-cmdb-backend/logx"
)

// DNS 解析写回 GoDaddy：在 CMDB 增/改/删解析，直接同步回厂商（以后不用登 GoDaddy 后台）。
// 语义对齐 GoDaddy——按「类型+主机名」整组读改写，不是单条。护栏：受保护记录禁改 + 可选 dry-run + 写审计 + 全链路日志。

// writableTypes 当前允许在 CMDB 写回的记录类型（SRV/CAA/NS/SOA 字段复杂或受保护，暂用厂商后台）。
var writableTypes = map[string]bool{"A": true, "AAAA": true, "CNAME": true, "TXT": true, "MX": true}

// batchTimeout 按操作组数放大超时：每组 2 次 GoDaddy 调用(GetGroup+写)，
// 撞客户端限流(50/分钟)时会阻塞等窗口，固定 90s 会中途超时导致部分写入。
// 基础 60s + 每组 5s，上限 10 分钟。
func batchTimeout(groups int) time.Duration {
	d := 60*time.Second + time.Duration(groups)*5*time.Second
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}

// isProtectedTypeName 判断某 (type,name) 是否受保护、禁止 CMDB 写回。
// 与只读同步的 isProtectedRecord 一致：NS/SOA/_acme-challenge 不许动。
func isProtectedTypeName(rtype, name string) bool {
	rtype = strings.ToUpper(strings.TrimSpace(rtype))
	if rtype == "NS" || rtype == "SOA" {
		return true
	}
	return strings.HasPrefix(name, "_acme-challenge")
}

// writeAdapterForDomain 取某域名的写回适配器 + 域名名 + 数据源id。
// 校验：域名存在、已绑数据源、该 provider 支持写回。
func (h *SyncHandler) writeAdapterForDomain(ciid string) (wa dnsource.WriteAdapter, ad dnsource.Adapter, domain string, sourceID int, err error) {
	var name string
	var srcID sql.NullInt64
	if e := h.DB.QueryRow(`SELECT c.name, d.registrar_id FROM cis c JOIN domains d ON d.ci_id=c.id
		WHERE c.id=? AND c.type='domain'`, ciid).Scan(&name, &srcID); e != nil {
		return nil, nil, "", 0, fmt.Errorf("域名不存在")
	}
	if !srcID.Valid {
		return nil, nil, "", 0, fmt.Errorf("该域名未绑定数据源，无法写回")
	}
	id := int(srcID.Int64)
	provider, cred, e := LoadCredential(h.DB, h.Cipher, id)
	if e != nil {
		return nil, nil, "", 0, fmt.Errorf("数据源凭据读取失败")
	}
	adapter, e := dnsource.NewAdapter(provider, cred, dnsource.LimiterFor(id))
	if e != nil {
		return nil, nil, "", 0, e
	}
	w, ok := adapter.(dnsource.WriteAdapter)
	if !ok {
		return nil, nil, "", 0, fmt.Errorf("数据源 %s 暂不支持写回", provider)
	}
	return w, adapter, name, id, nil
}

// normalizeRecordInput 归一化并校验写回入参；返回错误消息（空为通过）。
func normalizeRecordInput(rtype *string, name *string, data string, ttl *int, priority **int) string {
	*rtype = strings.ToUpper(strings.TrimSpace(*rtype))
	*name = strings.TrimSpace(*name)
	if *name == "" {
		*name = "@" // 根记录
	}
	if !writableTypes[*rtype] {
		return "该类型暂不支持在 CMDB 写回（当前支持 A/AAAA/CNAME/TXT/MX，其余请用厂商后台）"
	}
	if isProtectedTypeName(*rtype, *name) {
		return "该记录受保护（NS/SOA/_acme-challenge），禁止写回"
	}
	if strings.TrimSpace(data) == "" {
		return "记录值不能为空"
	}
	if *ttl < 600 {
		*ttl = 600 // GoDaddy 最小 TTL 600
	}
	if *rtype == "MX" && *priority == nil {
		p := 10
		*priority = &p // MX 需优先级，缺省 10
	}
	return ""
}

// refreshDomainDNSCache 写回成功后回拉整域名记录，刷新 dns_records 缓存，让 UI 立即看到真实结果。
func (h *SyncHandler) refreshDomainDNSCache(ctx context.Context, ad dnsource.Adapter, ciID int64, sourceID int, domain string) {
	recs, err := ad.ListRecords(ctx, domain)
	if err != nil {
		// 回拉失败不阻断（写回已成功），仅记日志；下次同步会对齐
		logx.JCtx(ctx, "dns_write", "refresh_cache_fail", map[string]any{"domain": domain, "error": err.Error()})
		return
	}
	h.refreshDNSRecords(ciID, sourceID, recs)
}

// CreateDNSRecord 新增一条解析并写回厂商（POST /domains/:ciid/dns-records）。
func (h *SyncHandler) CreateDNSRecord(c *gin.Context) {
	ciid := c.Param("ciid")
	var in struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		Data     string `json:"data"`
		TTL      int    `json:"ttl"`
		Priority *int   `json:"priority"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if msg := normalizeRecordInput(&in.Type, &in.Name, in.Data, &in.TTL, &in.Priority); msg != "" {
		c.JSON(400, gin.H{"error": msg})
		return
	}
	wa, ad, domain, sourceID, err := h.writeAdapterForDomain(ciid)
	if err != nil {
		logx.JCtx(c.Request.Context(), "dns_write", "create_precheck_fail", map[string]any{"ciid": ciid, "error": err.Error()})
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	logx.JCtx(c.Request.Context(), "dns_write", "create_start", map[string]any{"domain": domain, "type": in.Type, "name": in.Name, "data": in.Data, "env": wa.EnvLabel(), "dry_run": wa.DryRun(), "operator": currentUser(c)})
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	// 查重（best-effort）后用 PATCH 原子追加单条——不再整组 ReplaceGroup，
	// 避免两人同时给同 (type,name) 加记录时后写覆盖先写导致丢记录（TOCTOU）。
	group, err := wa.GetGroup(ctx, domain, in.Type, in.Name)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "读取现有记录失败：" + err.Error()})
		return
	}
	for _, r := range group {
		if r.Data == strings.TrimSpace(in.Data) {
			c.JSON(409, gin.H{"error": "该 主机名+类型 下已存在相同值的记录"})
			return
		}
	}
	newRec := dnsource.DNSRecord{Type: in.Type, Name: in.Name, Data: strings.TrimSpace(in.Data), TTL: in.TTL, Priority: in.Priority}
	if err := wa.AddRecords(ctx, domain, []dnsource.DNSRecord{newRec}); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "写回 GoDaddy 失败：" + err.Error()})
		return
	}
	ciID, _ := parseID(ciid)
	if !wa.DryRun() {
		h.refreshDomainDNSCache(ctx, ad, ciID, sourceID, domain)
	}
	logx.JCtx(c.Request.Context(), "dns_write", "create_done", map[string]any{"domain": domain, "type": in.Type, "name": in.Name, "data": in.Data, "dry_run": wa.DryRun()})
	SetAuditTarget(c, fmt.Sprintf("%s %s.%s → %s", in.Type, in.Name, domain, in.Data))
	c.JSON(200, gin.H{"ok": true, "dry_run": wa.DryRun(), "env": wa.EnvLabel(),
		"msg": writeResultMsg(wa, "已新增并写回")})
}

// BatchCreateDNSRecord 批量新增解析并写回（POST /domains/:ciid/dns-records/batch）。
// GoDaddy PATCH 一次追加多条；逐行校验，非法行跳过并回报原因，合法行一把写回。
func (h *SyncHandler) BatchCreateDNSRecord(c *gin.Context) {
	ciid := c.Param("ciid")
	var in struct {
		Records []struct {
			Type     string `json:"type"`
			Name     string `json:"name"`
			Data     string `json:"data"`
			TTL      int    `json:"ttl"`
			Priority *int   `json:"priority"`
		} `json:"records"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if len(in.Records) == 0 {
		c.JSON(400, gin.H{"error": "没有要新增的记录"})
		return
	}
	if len(in.Records) > 200 {
		c.JSON(400, gin.H{"error": "一次最多 200 条"})
		return
	}
	// 逐行校验：合法进 valid，非法进 errs（带行号+原因），不阻断其它行
	type rowErr struct {
		Row    int    `json:"row"`
		Detail string `json:"detail"`
		Msg    string `json:"msg"`
	}
	var valid []dnsource.DNSRecord
	var errs []rowErr
	for i, r := range in.Records {
		rtype, name, ttl, prio := r.Type, r.Name, r.TTL, r.Priority
		if msg := normalizeRecordInput(&rtype, &name, r.Data, &ttl, &prio); msg != "" {
			errs = append(errs, rowErr{Row: i + 1, Detail: fmt.Sprintf("%s %s", r.Type, r.Name), Msg: msg})
			continue
		}
		valid = append(valid, dnsource.DNSRecord{Type: rtype, Name: name, Data: strings.TrimSpace(r.Data), TTL: ttl, Priority: prio})
	}
	if len(valid) == 0 {
		c.JSON(400, gin.H{"error": "全部行校验不通过", "errors": errs})
		return
	}
	wa, ad, domain, sourceID, err := h.writeAdapterForDomain(ciid)
	if err != nil {
		logx.JCtx(c.Request.Context(), "dns_write", "batch_create_precheck_fail", map[string]any{"ciid": ciid, "error": err.Error()})
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	logx.JCtx(c.Request.Context(), "dns_write", "batch_create_start", map[string]any{"domain": domain, "valid": len(valid), "skipped": len(errs), "env": wa.EnvLabel(), "dry_run": wa.DryRun(), "operator": currentUser(c)})
	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()
	if err := wa.AddRecords(ctx, domain, valid); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "批量写回 GoDaddy 失败：" + err.Error()})
		return
	}
	ciID, _ := parseID(ciid)
	if !wa.DryRun() {
		h.refreshDomainDNSCache(ctx, ad, ciID, sourceID, domain)
	}
	logx.JCtx(c.Request.Context(), "dns_write", "batch_create_done", map[string]any{"domain": domain, "added": len(valid), "skipped": len(errs), "dry_run": wa.DryRun()})
	SetAuditTarget(c, fmt.Sprintf("%s 新增%d条", domain, len(valid)))
	c.JSON(200, gin.H{"ok": true, "dry_run": wa.DryRun(), "env": wa.EnvLabel(),
		"added": len(valid), "skipped": len(errs), "errors": errs,
		"msg": writeResultMsg(wa, fmt.Sprintf("已批量新增 %d 条（跳过 %d 条）", len(valid), len(errs)))})
}

// cachedRec 缓存里的一条解析（批量改/删按 id 反查 type/name/data）。
type cachedRec struct {
	ID   int64
	Type string
	Name string
	Data string
}

// loadCachedRecs 按 id 列表 + 域名反查 dns_records（限定本域名，防越权改别的域名记录）。
func (h *SyncHandler) loadCachedRecs(ciID int64, ids []int64) map[int64]cachedRec {
	out := map[int64]cachedRec{}
	if len(ids) == 0 {
		return out
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{ciID}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := h.DB.Query(`SELECT id, type, name, data FROM dns_records WHERE domain_ci_id=? AND id IN (`+ph+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r cachedRec
		if rows.Scan(&r.ID, &r.Type, &r.Name, &r.Data) == nil {
			out[r.ID] = r
		}
	}
	return out
}

type batchRowErr struct {
	ID     int64  `json:"id"`
	Detail string `json:"detail"`
	Msg    string `json:"msg"`
}

// BatchDeleteDNSRecords 批量删除解析并写回（POST /domains/:ciid/dns-records/batch-delete）。
// 按「类型+主机名」分组：整组只剩要删的→DeleteGroup；还有其它→ReplaceGroup 留剩余。受保护跳过。
func (h *SyncHandler) BatchDeleteDNSRecords(c *gin.Context) {
	ciid := c.Param("ciid")
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if len(in.IDs) == 0 {
		c.JSON(400, gin.H{"error": "没有选中要删除的记录"})
		return
	}
	if len(in.IDs) > 200 {
		c.JSON(400, gin.H{"error": "一次最多删除 200 条"})
		return
	}
	ciID, _ := parseID(ciid)
	recs := h.loadCachedRecs(ciID, in.IDs)
	// 按 (type,name) 分组收集要删的 data；受保护跳过
	type grp struct{ rtype, name string }
	toDel := map[grp]map[string]int64{} // group → data→id
	var errs []batchRowErr
	for _, id := range in.IDs {
		r, ok := recs[id]
		if !ok {
			continue
		}
		if isProtectedTypeName(r.Type, r.Name) {
			errs = append(errs, batchRowErr{ID: id, Detail: r.Type + " " + r.Name, Msg: "受保护，禁止删除"})
			continue
		}
		g := grp{r.Type, r.Name}
		if toDel[g] == nil {
			toDel[g] = map[string]int64{}
		}
		toDel[g][r.Data] = id
	}
	if len(toDel) == 0 {
		c.JSON(400, gin.H{"error": "没有可删除的记录（可能都受保护）", "errors": errs})
		return
	}
	wa, ad, domain, sourceID, err := h.writeAdapterForDomain(ciid)
	if err != nil {
		logx.JCtx(c.Request.Context(), "dns_write", "batch_delete_precheck_fail", map[string]any{"ciid": ciid, "error": err.Error()})
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), batchTimeout(len(toDel)))
	defer cancel()
	logx.JCtx(c.Request.Context(), "dns_write", "batch_delete_start", map[string]any{"domain": domain, "groups": len(toDel), "env": wa.EnvLabel(), "dry_run": wa.DryRun(), "operator": currentUser(c)})
	deleted := 0
	for g, dataSet := range toDel {
		group, e := wa.GetGroup(ctx, domain, g.rtype, g.name)
		if e != nil {
			for _, id := range dataSet {
				errs = append(errs, batchRowErr{ID: id, Detail: g.rtype + " " + g.name, Msg: "读取现有记录失败：" + e.Error()})
			}
			continue
		}
		remaining := make([]dnsource.DNSRecord, 0, len(group))
		for _, r := range group {
			if _, del := dataSet[r.Data]; !del {
				remaining = append(remaining, r)
			}
		}
		if len(remaining) == 0 {
			e = wa.DeleteGroup(ctx, domain, g.rtype, g.name)
		} else {
			e = wa.ReplaceGroup(ctx, domain, g.rtype, g.name, remaining)
		}
		if e != nil {
			for _, id := range dataSet {
				errs = append(errs, batchRowErr{ID: id, Detail: g.rtype + " " + g.name, Msg: "写回失败：" + e.Error()})
			}
			continue
		}
		deleted += len(dataSet)
	}
	if !wa.DryRun() {
		h.refreshDomainDNSCache(ctx, ad, ciID, sourceID, domain)
	}
	logx.JCtx(c.Request.Context(), "dns_write", "batch_delete_done", map[string]any{"domain": domain, "deleted": deleted, "skipped_failed": len(errs), "dry_run": wa.DryRun()})
	for _, r := range recs {
		auditDNSChange(c, "DELETE", domain, fmt.Sprint(r.ID),
			map[string]interface{}{"domain": domain, "type": r.Type, "name": r.Name, "data": r.Data}, nil)
	}
	SetAuditTarget(c, fmt.Sprintf("%s 删除%d条", domain, deleted))
	c.JSON(200, gin.H{"ok": true, "dry_run": wa.DryRun(), "env": wa.EnvLabel(),
		"deleted": deleted, "skipped": len(errs), "errors": errs,
		"msg": writeResultMsg(wa, fmt.Sprintf("已批量删除 %d 条（跳过/失败 %d 条）", deleted, len(errs)))})
}

// BatchUpdateDNSRecords 批量编辑解析（改值/TTL/优先级）并写回（POST /domains/:ciid/dns-records/batch-update）。
// 按 id 反查旧 (type,name,data)，按 (type,name) 分组，一次把该组内所有改动套进 ReplaceGroup。
func (h *SyncHandler) BatchUpdateDNSRecords(c *gin.Context) {
	ciid := c.Param("ciid")
	var in struct {
		Records []struct {
			ID       int64  `json:"id"`
			Data     string `json:"data"`
			TTL      int    `json:"ttl"`
			Priority *int   `json:"priority"`
		} `json:"records"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if len(in.Records) == 0 {
		c.JSON(400, gin.H{"error": "没有要编辑的记录"})
		return
	}
	if len(in.Records) > 200 {
		c.JSON(400, gin.H{"error": "一次最多编辑 200 条"})
		return
	}
	ciID, _ := parseID(ciid)
	ids := make([]int64, 0, len(in.Records))
	for _, r := range in.Records {
		ids = append(ids, r.ID)
	}
	cache := h.loadCachedRecs(ciID, ids)
	// 按 (type,name) 分组：旧data → 新记录
	type grp struct{ rtype, name string }
	edits := map[grp]map[string]dnsource.DNSRecord{} // group → oldData→newRec
	var errs []batchRowErr
	for _, r := range in.Records {
		old, ok := cache[r.ID]
		if !ok {
			continue
		}
		rtype, name, ttl, prio := old.Type, old.Name, r.TTL, r.Priority
		if msg := normalizeRecordInput(&rtype, &name, r.Data, &ttl, &prio); msg != "" {
			errs = append(errs, batchRowErr{ID: r.ID, Detail: old.Type + " " + old.Name, Msg: msg})
			continue
		}
		g := grp{old.Type, old.Name}
		if edits[g] == nil {
			edits[g] = map[string]dnsource.DNSRecord{}
		}
		edits[g][old.Data] = dnsource.DNSRecord{Type: rtype, Name: name, Data: strings.TrimSpace(r.Data), TTL: ttl, Priority: prio}
	}
	if len(edits) == 0 {
		c.JSON(400, gin.H{"error": "没有可编辑的记录", "errors": errs})
		return
	}
	wa, ad, domain, sourceID, err := h.writeAdapterForDomain(ciid)
	if err != nil {
		logx.JCtx(c.Request.Context(), "dns_write", "batch_update_precheck_fail", map[string]any{"ciid": ciid, "error": err.Error()})
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), batchTimeout(len(edits)))
	defer cancel()
	logx.JCtx(c.Request.Context(), "dns_write", "batch_update_start", map[string]any{"domain": domain, "groups": len(edits), "env": wa.EnvLabel(), "dry_run": wa.DryRun(), "operator": currentUser(c)})
	updated := 0
	for g, m := range edits {
		group, e := wa.GetGroup(ctx, domain, g.rtype, g.name)
		if e != nil {
			for range m {
				errs = append(errs, batchRowErr{Detail: g.rtype + " " + g.name, Msg: "读取现有记录失败：" + e.Error()})
			}
			continue
		}
		cnt := 0
		for i := range group {
			if nr, ok := m[group[i].Data]; ok {
				group[i] = nr
				cnt++
			}
		}
		if cnt == 0 {
			for range m {
				errs = append(errs, batchRowErr{Detail: g.rtype + " " + g.name, Msg: "记录已在厂商侧变化，请先同步"})
			}
			continue
		}
		if e = wa.ReplaceGroup(ctx, domain, g.rtype, g.name, group); e != nil {
			for range m {
				errs = append(errs, batchRowErr{Detail: g.rtype + " " + g.name, Msg: "写回失败：" + e.Error()})
			}
			continue
		}
		updated += cnt
	}
	if !wa.DryRun() {
		h.refreshDomainDNSCache(ctx, ad, ciID, sourceID, domain)
	}
	logx.JCtx(c.Request.Context(), "dns_write", "batch_update_done", map[string]any{"domain": domain, "updated": updated, "failed": len(errs), "dry_run": wa.DryRun()})
	SetAuditTarget(c, fmt.Sprintf("%s 编辑%d条", domain, updated))
	c.JSON(200, gin.H{"ok": true, "dry_run": wa.DryRun(), "env": wa.EnvLabel(),
		"updated": updated, "failed": len(errs), "errors": errs,
		"msg": writeResultMsg(wa, fmt.Sprintf("已批量编辑 %d 条（失败 %d 条）", updated, len(errs)))})
}

// UpdateDNSRecord 编辑一条解析（改值/TTL/优先级；类型+主机名不变）并写回（PUT /dns-records/:id）。
func (h *SyncHandler) UpdateDNSRecord(c *gin.Context) {
	id := c.Param("id")
	var rtype, name, oldData string
	var domCiID int64
	if h.DB.QueryRow(`SELECT domain_ci_id, type, name, data FROM dns_records WHERE id=?`, id).
		Scan(&domCiID, &rtype, &name, &oldData) != nil {
		c.JSON(404, gin.H{"error": "记录不存在"})
		return
	}
	var in struct {
		Data     string `json:"data"`
		TTL      int    `json:"ttl"`
		Priority *int   `json:"priority"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if msg := normalizeRecordInput(&rtype, &name, in.Data, &in.TTL, &in.Priority); msg != "" {
		c.JSON(400, gin.H{"error": msg})
		return
	}
	wa, ad, domain, sourceID, err := h.writeAdapterForDomain(fmt.Sprint(domCiID))
	if err != nil {
		logx.JCtx(c.Request.Context(), "dns_write", "update_precheck_fail", map[string]any{"id": id, "error": err.Error()})
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	logx.JCtx(c.Request.Context(), "dns_write", "update_start", map[string]any{"domain": domain, "type": rtype, "name": name, "old_data": oldData, "new_data": in.Data, "env": wa.EnvLabel(), "dry_run": wa.DryRun(), "operator": currentUser(c)})
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	group, err := wa.GetGroup(ctx, domain, rtype, name)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "读取现有记录失败：" + err.Error()})
		return
	}
	replaced := false
	for i := range group {
		if group[i].Data == oldData {
			group[i] = dnsource.DNSRecord{Type: rtype, Name: name, Data: strings.TrimSpace(in.Data), TTL: in.TTL, Priority: in.Priority}
			replaced = true
			break
		}
	}
	if !replaced {
		c.JSON(409, gin.H{"error": "记录已在厂商侧变化，请先同步再编辑"})
		return
	}
	if err := wa.ReplaceGroup(ctx, domain, rtype, name, group); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "写回 GoDaddy 失败：" + err.Error()})
		return
	}
	if !wa.DryRun() {
		h.refreshDomainDNSCache(ctx, ad, domCiID, sourceID, domain)
	}
	logx.JCtx(c.Request.Context(), "dns_write", "update_done", map[string]any{"domain": domain, "type": rtype, "name": name, "old_data": oldData, "new_data": in.Data, "dry_run": wa.DryRun()})
	// 记业务字段而不是本地行——回滚时是拿这些字段调 DNS API 写回去，
	// 本地 id 在缓存刷新后已经变了，留着也没用。
	auditDNSChange(c, "UPDATE", domain, id,
		map[string]interface{}{"domain": domain, "type": rtype, "name": name, "data": oldData},
		map[string]interface{}{"domain": domain, "type": rtype, "name": name, "data": strings.TrimSpace(in.Data), "ttl": in.TTL})
	SetAuditTarget(c, fmt.Sprintf("%s %s.%s: %s → %s", rtype, name, domain, oldData, in.Data))
	c.JSON(200, gin.H{"ok": true, "dry_run": wa.DryRun(), "env": wa.EnvLabel(),
		"msg": writeResultMsg(wa, "已编辑并写回")})
}

// DeleteDNSRecord 删除一条解析并写回（DELETE /dns-records/:id）。
// 组内只此一条→删整组；组内还有其它→整组替换为剩余。
func (h *SyncHandler) DeleteDNSRecord(c *gin.Context) {
	id := c.Param("id")
	var rtype, name, oldData string
	var domCiID int64
	if h.DB.QueryRow(`SELECT domain_ci_id, type, name, data FROM dns_records WHERE id=?`, id).
		Scan(&domCiID, &rtype, &name, &oldData) != nil {
		c.JSON(404, gin.H{"error": "记录不存在"})
		return
	}
	if isProtectedTypeName(rtype, name) {
		c.JSON(400, gin.H{"error": "该记录受保护（NS/SOA/_acme-challenge），禁止删除"})
		return
	}
	wa, ad, domain, sourceID, err := h.writeAdapterForDomain(fmt.Sprint(domCiID))
	if err != nil {
		logx.JCtx(c.Request.Context(), "dns_write", "delete_precheck_fail", map[string]any{"id": id, "error": err.Error()})
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	logx.JCtx(c.Request.Context(), "dns_write", "delete_start", map[string]any{"domain": domain, "type": rtype, "name": name, "data": oldData, "env": wa.EnvLabel(), "dry_run": wa.DryRun(), "operator": currentUser(c)})
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	group, err := wa.GetGroup(ctx, domain, rtype, name)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "读取现有记录失败：" + err.Error()})
		return
	}
	remaining := make([]dnsource.DNSRecord, 0, len(group))
	for _, r := range group {
		if r.Data != oldData {
			remaining = append(remaining, r)
		}
	}
	if len(remaining) == 0 {
		err = wa.DeleteGroup(ctx, domain, rtype, name)
	} else {
		err = wa.ReplaceGroup(ctx, domain, rtype, name, remaining)
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "写回 GoDaddy 失败：" + err.Error()})
		return
	}
	if !wa.DryRun() {
		h.refreshDomainDNSCache(ctx, ad, domCiID, sourceID, domain)
	}
	logx.JCtx(c.Request.Context(), "dns_write", "delete_done", map[string]any{"domain": domain, "type": rtype, "name": name, "data": oldData, "dry_run": wa.DryRun()})
	auditDNSChange(c, "DELETE", domain, id,
		map[string]interface{}{"domain": domain, "type": rtype, "name": name, "data": oldData}, nil)
	SetAuditTarget(c, fmt.Sprintf("%s %s.%s (%s)", rtype, name, domain, oldData))
	c.JSON(200, gin.H{"ok": true, "dry_run": wa.DryRun(), "env": wa.EnvLabel(),
		"msg": writeResultMsg(wa, "已删除并写回")})
}

// writeResultMsg 组织回执文案：预演/环境标识让用户一眼看清是否真写、写到哪个环境。
func writeResultMsg(wa dnsource.WriteAdapter, action string) string {
	if wa.DryRun() {
		return fmt.Sprintf("【预演·未真发】%s（%s）——关掉 dry_run 才会真正写入", action, wa.EnvLabel())
	}
	return fmt.Sprintf("%s（%s）", action, wa.EnvLabel())
}

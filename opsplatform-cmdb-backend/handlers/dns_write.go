package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/dnsource"
)

// DNS 解析写回 GoDaddy：在 CMDB 增/改/删解析，直接同步回厂商（以后不用登 GoDaddy 后台）。
// 语义对齐 GoDaddy——按「类型+主机名」整组读改写，不是单条。护栏：受保护记录禁改 + 可选 dry-run + 写审计 + 全链路日志。

// writableTypes 当前允许在 CMDB 写回的记录类型（SRV/CAA/NS/SOA 字段复杂或受保护，暂用厂商后台）。
var writableTypes = map[string]bool{"A": true, "AAAA": true, "CNAME": true, "TXT": true, "MX": true}

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
		log.Printf("[dns写回] 回拉 %s 记录失败（写回已成功，缓存稍后由同步对齐）: %v", domain, err)
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
		log.Printf("[dns写回] 新增前置失败 ciid=%s: %v", ciid, err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[dns写回] 新增发起 domain=%s %s %s→%s env=%s dry_run=%v 操作人=%s", domain, in.Type, in.Name, in.Data, wa.EnvLabel(), wa.DryRun(), currentUser(c))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 读改写：取现有组 → 追加（同值查重）→ 整组 PUT
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
	group = append(group, dnsource.DNSRecord{Type: in.Type, Name: in.Name, Data: strings.TrimSpace(in.Data), TTL: in.TTL, Priority: in.Priority})
	if err := wa.ReplaceGroup(ctx, domain, in.Type, in.Name, group); err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "写回 GoDaddy 失败：" + err.Error()})
		return
	}
	ciID, _ := parseID(ciid)
	if !wa.DryRun() {
		h.refreshDomainDNSCache(ctx, ad, ciID, sourceID, domain)
	}
	log.Printf("[dns写回] 新增完成 domain=%s %s %s→%s dry_run=%v", domain, in.Type, in.Name, in.Data, wa.DryRun())
	WriteAudit(h.DB, c, "dns_create", fmt.Sprintf("%s %s.%s → %s", in.Type, in.Name, domain, in.Data))
	c.JSON(200, gin.H{"ok": true, "dry_run": wa.DryRun(), "env": wa.EnvLabel(),
		"msg": writeResultMsg(wa, "已新增并写回")})
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
		log.Printf("[dns写回] 编辑前置失败 id=%s: %v", id, err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[dns写回] 编辑发起 domain=%s %s %s: %s→%s env=%s dry_run=%v 操作人=%s", domain, rtype, name, oldData, in.Data, wa.EnvLabel(), wa.DryRun(), currentUser(c))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	log.Printf("[dns写回] 编辑完成 domain=%s %s %s: %s→%s dry_run=%v", domain, rtype, name, oldData, in.Data, wa.DryRun())
	WriteAudit(h.DB, c, "dns_update", fmt.Sprintf("%s %s.%s: %s → %s", rtype, name, domain, oldData, in.Data))
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
		log.Printf("[dns写回] 删除前置失败 id=%s: %v", id, err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	log.Printf("[dns写回] 删除发起 domain=%s %s %s (%s) env=%s dry_run=%v 操作人=%s", domain, rtype, name, oldData, wa.EnvLabel(), wa.DryRun(), currentUser(c))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	log.Printf("[dns写回] 删除完成 domain=%s %s %s (%s) dry_run=%v", domain, rtype, name, oldData, wa.DryRun())
	WriteAudit(h.DB, c, "dns_delete", fmt.Sprintf("%s %s.%s (%s)", rtype, name, domain, oldData))
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

package handlers

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// RecordHandler 解析层（domain_records）：一个域名下挂多条解析。
type RecordHandler struct {
	DB *sql.DB
}

func NewRecordHandler(db *sql.DB) *RecordHandler { return &RecordHandler{DB: db} }

func (h *RecordHandler) Register(r *gin.RouterGroup) {
	r.GET("/records", h.ListAll) // 拉平：跨所有域名的主机头台账
	r.GET("/domains/:ciid/records", h.List)
	r.POST("/domains/:ciid/records", h.Create)
	r.PUT("/records/:id", h.Update)
	r.POST("/records/bulk-update", h.BulkUpdate) // 批量设项目/环境/模块
	r.POST("/records/bulk-ignore", h.BulkIgnore) // 批量忽略/取消忽略
	r.DELETE("/records/:id", h.Delete)
	r.POST("/records/:id/check-cert", h.CheckCert)
	r.POST("/domains/:ciid/check-all-certs", h.CheckAllCerts)
	// 源站映射规则（回源CNAME → 源站IP）
	r.GET("/origin-rules", h.ListOriginRules)
	r.POST("/origin-rules", h.UpsertOriginRule)
	r.DELETE("/origin-rules/:id", h.DeleteOriginRule)
}

// ListOriginRules 列出源站映射规则 + 每条"用到 N 条"（有多少业务解析回源到这个 CNAME）。
func (h *RecordHandler) ListOriginRules(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT r.id, r.cname, r.origin_ip,
			(SELECT COUNT(*) FROM domain_records dr WHERE LOWER(dr.cname)=LOWER(r.cname)) AS used,
			DATE_FORMAT(r.updated_at,'%Y-%m-%d %H:%i')
		FROM origin_ip_rules r ORDER BY used DESC, r.cname`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	type ruleOut struct {
		ID        int64  `json:"id"`
		Cname     string `json:"cname"`
		OriginIP  string `json:"origin_ip"`
		Used      int    `json:"used"`
		UpdatedAt string `json:"updated_at"`
	}
	out := []ruleOut{}
	for rows.Next() {
		var o ruleOut
		if rows.Scan(&o.ID, &o.Cname, &o.OriginIP, &o.Used, &o.UpdatedAt) == nil {
			out = append(out, o)
		}
	}
	c.JSON(http.StatusOK, out)
}

// UpsertOriginRule 新增/更新一条规则（按 cname 唯一，upsert）。
func (h *RecordHandler) UpsertOriginRule(c *gin.Context) {
	var in struct {
		Cname    string `json:"cname"`
		OriginIP string `json:"origin_ip"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || strings.TrimSpace(in.Cname) == "" {
		c.JSON(400, gin.H{"error": "回源 CNAME 必填"})
		return
	}
	if _, err := h.DB.Exec(`INSERT INTO origin_ip_rules (cname, origin_ip) VALUES (?, ?)
		ON DUPLICATE KEY UPDATE origin_ip=VALUES(origin_ip)`, strings.TrimSpace(in.Cname), strings.TrimSpace(in.OriginIP)); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "origin_rule_upsert", in.Cname)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// DeleteOriginRule 删除一条规则。
func (h *RecordHandler) DeleteOriginRule(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM origin_ip_rules WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type flatRecordOut struct {
	ID            int64  `json:"id"`
	DomainCIID    int64  `json:"domain_ci_id"`
	Domain        string `json:"domain"`
	FQDN          string `json:"fqdn"`
	Host          string `json:"host"`
	RecordType    string `json:"record_type"`
	CdnID         *int   `json:"cdn_id"`
	CdnName       string `json:"cdn_name"`
	Cname         string `json:"cname"`
	OriginIP      string `json:"origin_ip"`
	AutoOriginIP  string `json:"auto_origin_ip"`  // 手填 origin_ip 为空时的推测源站IP（不落库，仅展示）：DNS 解析优先，查不到用映射规则兜底
	AutoOriginSrc string `json:"auto_origin_src"` // 推测来源：解析(DNS查A记录) / 规则(源站映射) / 空
	CertExpiryAt  string `json:"cert_expiry_at"`
	CertCheckMsg  string `json:"cert_check_msg"`
	Project       string `json:"project"`
	Env           string `json:"env"`
	Module        string `json:"module"`
	ModuleSource  string `json:"module_source"` // auto=K8s自动关联 / manual=手动
	LifeStatus    string `json:"life_status"`   // 该记录使用状态(使用中等)
	StatusSource  string `json:"status_source"` // auto/manual
	Operator      string `json:"operator"`
	Origin        string `json:"origin"`        // 所属主域名来源：manual/sync
	SourceName    string `json:"source_name"`   // 数据源/注册商名（GoDaddy 等）
	DomainStatus  string `json:"domain_status"` // 所属主域名的生命周期状态
	DomainStale   bool   `json:"domain_stale"`  // 所属主域名是否已移出账号/过户（stale）
	DomainGone    string `json:"domain_gone"`   // 主域名失效标签：已过户/已移出账号/已取消/已失效（stale 时）
	Ignored       bool   `json:"ignored"`
	IgnoreReason  string `json:"ignore_reason"`
	Stale         bool   `json:"stale"`
	UpdatedAt     string `json:"updated_at"`
}

// ListAll 拉平所有域名下的解析记录：一行一个「主机头.域名」，供主机头台账页展示。
// status: 默认(空/normal)只返回未忽略；ignored=只返回已忽略；all=全部。
func (h *RecordHandler) ListAll(c *gin.Context) {
	// status: normal(默认)=未忽略且主域名未忽略未移出 / ignored=记录已忽略 / stale=主域名已移出账号(过户/转出) / all=全部
	cond := "AND COALESCE(d.ignored,0)=0 AND COALESCE(d.stale,0)=0 AND r.ignored=0"
	switch c.Query("status") {
	case "ignored":
		cond = "AND COALESCE(d.ignored,0)=0 AND r.ignored=1"
	case "stale":
		cond = "AND COALESCE(d.ignored,0)=0 AND COALESCE(d.stale,0)=1"
	case "all":
		cond = ""
	}
	rows, err := h.DB.Query(`
		SELECT r.id, r.domain_ci_id, c.name, r.host, r.record_type, r.cdn_id, COALESCE(cd.name,''),
		       r.cname, r.origin_ip, r.cert_expiry_at, r.cert_check_msg,
		       r.project, r.env, r.module, COALESCE(r.module_source,''), COALESCE(r.life_status,''), COALESCE(r.status_source,''), r.operator,
		       COALESCE(d.origin,''), COALESCE(reg.name,''), r.ignored, r.ignore_reason, r.stale, r.updated_at,
		       COALESCE(d.status,''), COALESCE(d.stale,0), COALESCE(d.source_status,'')
		FROM domain_records r
		JOIN cis c ON c.id=r.domain_ci_id
		LEFT JOIN domains d ON d.ci_id=r.domain_ci_id
		LEFT JOIN cdns cd ON cd.id=r.cdn_id
		LEFT JOIN registrars reg ON reg.id=d.registrar_id
		WHERE c.type='domain' ` + cond + `
		ORDER BY r.project, c.name, r.host`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	resolveOrigin := buildCnameOriginResolver(h.DB) // 回源 CNAME → 已同步 DNS 里的 A 记录，推测源站IP
	out := []flatRecordOut{}
	for rows.Next() {
		var o flatRecordOut
		var cdnID sql.NullInt64
		var certExp, updated sql.NullTime
		var stale, ignored, domStale int
		var srcStatus string
		if err := rows.Scan(&o.ID, &o.DomainCIID, &o.Domain, &o.Host, &o.RecordType, &cdnID, &o.CdnName,
			&o.Cname, &o.OriginIP, &certExp, &o.CertCheckMsg,
			&o.Project, &o.Env, &o.Module, &o.ModuleSource, &o.LifeStatus, &o.StatusSource, &o.Operator, &o.Origin, &o.SourceName, &ignored, &o.IgnoreReason, &stale, &updated,
			&o.DomainStatus, &domStale, &srcStatus); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		o.Stale = stale == 1
		o.Ignored = ignored == 1
		o.DomainStale = domStale == 1
		if o.DomainStale {
			o.DomainGone = domainGoneLabel(srcStatus)
		}
		o.FQDN = recordFQDN(o.Host, o.Domain)
		// 手填源站IP为空且有回源CNAME时，推测源站IP（不落库，仅展示，手填优先）：DNS解析优先，查不到用规则兜底
		if o.OriginIP == "" && o.Cname != "" {
			o.AutoOriginIP, o.AutoOriginSrc = resolveOrigin(o.Cname)
		}
		if cdnID.Valid {
			v := int(cdnID.Int64)
			o.CdnID = &v
		}
		if certExp.Valid {
			o.CertExpiryAt = certExp.Time.Format("2006-01-02")
		}
		if updated.Valid {
			o.UpdatedAt = updated.Time.Format("2006-01-02 15:04")
		}
		out = append(out, o)
	}
	c.JSON(http.StatusOK, out)
}

// CheckAllCerts 一键检测某域名下所有解析的证书到期（并发连 443，限并发 8）。
func (h *RecordHandler) CheckAllCerts(c *gin.Context) {
	ciid := c.Param("ciid")
	var domain string
	if err := h.DB.QueryRow(`SELECT name FROM cis WHERE id=? AND type='domain'`, ciid).Scan(&domain); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "域名不存在"})
		return
	}
	rows, err := h.DB.Query(`SELECT id, host FROM domain_records WHERE domain_ci_id=?`, ciid)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	type rec struct {
		id   int64
		host string
	}
	var recs []rec
	for rows.Next() {
		var r rec
		if rows.Scan(&r.id, &r.host) == nil {
			recs = append(recs, r)
		}
	}
	rows.Close()

	var ok, fail int32
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, r := range recs {
		wg.Add(1)
		go func(id int64, host string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fqdn := recordFQDN(host, domain)
			if t, cmsg := tlsCertExpiry(fqdn); t != nil {
				_, _ = h.DB.Exec(`UPDATE domain_records SET cert_expiry_at=?, cert_check_at=NOW(), cert_check_msg=? WHERE id=?`, *t, truncate(cmsg, 250), id)
				atomic.AddInt32(&ok, 1)
			} else {
				_, _ = h.DB.Exec(`UPDATE domain_records SET cert_check_at=NOW(), cert_check_msg=? WHERE id=?`, truncate(cmsg, 250), id)
				atomic.AddInt32(&fail, 1)
			}
		}(r.id, r.host)
	}
	wg.Wait()
	WriteAudit(h.DB, c, "check_all_certs", domain)
	c.JSON(http.StatusOK, gin.H{"ok": true, "checked": len(recs), "success": ok, "failed": fail})
}

type recordOut struct {
	ID           int64  `json:"id"`
	Host         string `json:"host"`
	RecordType   string `json:"record_type"`
	CdnID        *int   `json:"cdn_id"`
	CdnName      string `json:"cdn_name"`
	Cname        string `json:"cname"`
	OriginIP     string `json:"origin_ip"`
	CertExpiryAt string `json:"cert_expiry_at"`
	CertCheckMsg string `json:"cert_check_msg"`
	Project      string `json:"project"`
	Env          string `json:"env"`
	Module       string `json:"module"`
	Operator     string `json:"operator"`
	Stale        bool   `json:"stale"`
	UpdatedAt    string `json:"updated_at"`
}

func (h *RecordHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`
		SELECT r.id, r.host, r.record_type, r.cdn_id, COALESCE(d.name,''),
		       r.cname, r.origin_ip, r.cert_expiry_at, r.cert_check_msg,
		       r.project, r.env, r.module, r.operator, r.stale, r.updated_at
		FROM domain_records r LEFT JOIN cdns d ON d.id=r.cdn_id
		WHERE r.domain_ci_id=? ORDER BY r.stale, r.id`, c.Param("ciid"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []recordOut{}
	for rows.Next() {
		var o recordOut
		var cdnID sql.NullInt64
		var certExp sql.NullTime
		var updated sql.NullTime
		var stale int
		if err := rows.Scan(&o.ID, &o.Host, &o.RecordType, &cdnID, &o.CdnName,
			&o.Cname, &o.OriginIP, &certExp, &o.CertCheckMsg,
			&o.Project, &o.Env, &o.Module, &o.Operator, &stale, &updated); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		o.Stale = stale == 1
		if cdnID.Valid {
			v := int(cdnID.Int64)
			o.CdnID = &v
		}
		if certExp.Valid {
			o.CertExpiryAt = certExp.Time.Format("2006-01-02")
		}
		if updated.Valid {
			o.UpdatedAt = updated.Time.Format("2006-01-02 15:04")
		}
		out = append(out, o)
	}
	c.JSON(http.StatusOK, out)
}

type recordIn struct {
	Host       string `json:"host"`
	RecordType string `json:"record_type"`
	CdnID      *int   `json:"cdn_id"`
	Cname      string `json:"cname"`
	OriginIP   string `json:"origin_ip"`
	CertExpiry string `json:"cert_expiry_at"` // 手动填的证书到期 "2006-01-02" 或空
	Project    string `json:"project"`
	Env        string `json:"env"`
	Module     string `json:"module"`
	LifeStatus string `json:"life_status"` // 使用中/备用/未使用/待下线/已下线,手动设置
}

func (h *RecordHandler) Create(c *gin.Context) {
	var in recordIn
	if err := c.ShouldBindJSON(&in); err != nil || in.Host == "" {
		c.JSON(400, gin.H{"error": "host 必填"})
		return
	}
	if in.RecordType == "" {
		in.RecordType = "A"
	}
	res, err := h.DB.Exec(`INSERT INTO domain_records
		(domain_ci_id, host, record_type, cdn_id, cname, origin_ip, cert_expiry_at, project, env, module, operator)
		VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?)`,
		c.Param("ciid"), in.Host, in.RecordType, nullableInt(in.CdnID), in.Cname, in.OriginIP, in.CertExpiry,
		in.Project, in.Env, in.Module, currentUser(c))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	WriteAudit(h.DB, c, "create_record", in.Host)
	c.JSON(201, gin.H{"id": id})
}

func (h *RecordHandler) Update(c *gin.Context) {
	var in recordIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// 手动改:模块/使用中状态标记为 manual(区分自动关联)
	if _, err := h.DB.Exec(`UPDATE domain_records SET host=?, record_type=?, cdn_id=?, cname=?, origin_ip=?,
		cert_expiry_at=NULLIF(?, ''), project=?, env=?, module=?, module_source='manual',
		life_status=?, status_source=IF(?<>'', 'manual', status_source), operator=? WHERE id=?`,
		in.Host, in.RecordType, nullableInt(in.CdnID), in.Cname, in.OriginIP, in.CertExpiry,
		in.Project, in.Env, in.Module, in.LifeStatus, in.LifeStatus, currentUser(c), c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "update_record", in.Host)
	c.JSON(200, gin.H{"ok": true})
}

// BulkUpdate 批量设置选中解析的业务字段（项目/环境/模块）。只更新请求里显式给出的字段，未给出的不动。
func (h *RecordHandler) BulkUpdate(c *gin.Context) {
	var in struct {
		IDs         []int64 `json:"ids"`
		Project     *string `json:"project"`
		Env         *string `json:"env"`
		Module      *string `json:"module"`
		SetCdn      bool    `json:"set_cdn"`
		CdnID       *int    `json:"cdn_id"`
		SetOriginIP bool    `json:"set_origin_ip"`
		OriginIP    *string `json:"origin_ip"`
		SetCname    bool    `json:"set_cname"`
		Cname       *string `json:"cname"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || len(in.IDs) == 0 {
		c.JSON(400, gin.H{"error": "ids 必填"})
		return
	}
	sets := []string{}
	args := []any{}
	if in.Project != nil {
		sets = append(sets, "project=?")
		args = append(args, *in.Project)
	}
	if in.Env != nil {
		sets = append(sets, "env=?")
		args = append(args, *in.Env)
	}
	if in.Module != nil {
		sets = append(sets, "module=?")
		args = append(args, *in.Module)
	}
	if in.SetCdn {
		sets = append(sets, "cdn_id=?")
		args = append(args, nullableInt(in.CdnID))
	}
	if in.SetOriginIP { // 可填值=手填源站IP（盖过自动推算）；留空=清掉手填、回到自动推算
		v := ""
		if in.OriginIP != nil {
			v = *in.OriginIP
		}
		sets = append(sets, "origin_ip=?")
		args = append(args, v)
	}
	if in.SetCname {
		v := ""
		if in.Cname != nil {
			v = *in.Cname
		}
		sets = append(sets, "cname=?")
		args = append(args, v)
	}
	if len(sets) == 0 {
		c.JSON(400, gin.H{"error": "至少选择一个要设置的字段"})
		return
	}
	sets = append(sets, "operator=?")
	args = append(args, currentUser(c))
	ph := make([]string, len(in.IDs))
	for i, id := range in.IDs {
		ph[i] = "?"
		args = append(args, id)
	}
	q := "UPDATE domain_records SET " + strings.Join(sets, ", ") + " WHERE id IN (" + strings.Join(ph, ",") + ")"
	res, err := h.DB.Exec(q, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	n, _ := res.RowsAffected()
	WriteAudit(h.DB, c, "bulk_update_records", "count="+strconv.FormatInt(n, 10))
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": n})
}

// BulkIgnore 批量忽略/取消忽略主机头。ignored=true 标忽略(可带原因)，false 取消。
func (h *RecordHandler) BulkIgnore(c *gin.Context) {
	var in struct {
		IDs     []int64 `json:"ids"`
		Ignored bool    `json:"ignored"`
		Reason  string  `json:"reason"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || len(in.IDs) == 0 {
		c.JSON(400, gin.H{"error": "ids 必填"})
		return
	}
	ph := make([]string, len(in.IDs))
	args := []any{}
	ig := 0
	if in.Ignored {
		ig = 1
	}
	args = append(args, ig, in.Reason)
	for i, id := range in.IDs {
		ph[i] = "?"
		args = append(args, id)
	}
	q := "UPDATE domain_records SET ignored=?, ignore_reason=? WHERE id IN (" + strings.Join(ph, ",") + ")"
	res, err := h.DB.Exec(q, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	n, _ := res.RowsAffected()
	action := "ignore_records"
	if !in.Ignored {
		action = "unignore_records"
	}
	WriteAudit(h.DB, c, action, "count="+strconv.FormatInt(n, 10))
	c.JSON(http.StatusOK, gin.H{"ok": true, "updated": n})
}

func (h *RecordHandler) Delete(c *gin.Context) {
	if _, err := h.DB.Exec(`DELETE FROM domain_records WHERE id=?`, c.Param("id")); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "delete_record", c.Param("id"))
	c.JSON(200, gin.H{"ok": true})
}

// CheckCert 连接 该解析的完整域名:443 读线上证书到期，写回 cert_expiry_at。手动填的值会被覆盖。
func (h *RecordHandler) CheckCert(c *gin.Context) {
	id := c.Param("id")
	var host, domain string
	if err := h.DB.QueryRow(`SELECT r.host, c.name FROM domain_records r JOIN cis c ON c.id=r.domain_ci_id WHERE r.id=?`, id).
		Scan(&host, &domain); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "解析不存在"})
		return
	}
	fqdn := recordFQDN(host, domain)
	if t, cmsg := tlsCertExpiry(fqdn); t != nil {
		_, _ = h.DB.Exec(`UPDATE domain_records SET cert_expiry_at=?, cert_check_at=NOW(), cert_check_msg=? WHERE id=?`, *t, truncate(cmsg, 250), id)
		c.JSON(http.StatusOK, gin.H{"ok": true, "fqdn": fqdn, "cert_expiry_at": t.Format("2006-01-02"), "warn": cmsg})
		return
	} else {
		_, _ = h.DB.Exec(`UPDATE domain_records SET cert_check_at=NOW(), cert_check_msg=? WHERE id=?`, truncate(cmsg, 250), id)
		c.JSON(http.StatusOK, gin.H{"ok": false, "fqdn": fqdn, "msg": cmsg})
		return
	}
}

// recordFQDN 由主机头 + 主域名拼出完整域名。host 为空/@ 即主域名；已含主域名则原样返回。
func recordFQDN(host, domain string) string {
	host = strings.TrimSpace(host)
	if host == "" || host == "@" {
		return domain
	}
	if host == domain || strings.HasSuffix(host, "."+domain) {
		return host
	}
	return host + "." + domain
}

// domainGoneLabel 主域名已移出账号(stale)时，按 GoDaddy source_status 给中文标签。
func domainGoneLabel(sourceStatus string) string {
	s := strings.ToUpper(strings.TrimSpace(sourceStatus))
	switch {
	case strings.Contains(s, "OWNERSHIP"):
		return "已过户"
	case strings.Contains(s, "TRANSFER"):
		return "已移出账号"
	case strings.Contains(s, "CANCEL"):
		return "已取消"
	default:
		return "已失效"
	}
}

// normFQDN 归一化：去空白、末尾点、转小写，用于回源 CNAME 与 A 记录的 FQDN 匹配。
func normFQDN(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}

// buildCnameOriginResolver 从 dns_records 建全局「FQDN → A记录IP / CNAME目标」索引 + origin_ip_rules 映射规则，
// 返回一个推测源站IP 的函数：回源 CNAME 先跟随 CNAME 链(≤3跳)找 A 记录(命中→"解析")；
// 查不到再用映射规则兜底(命中→"规则")；都没有返回("", "")。跨所有已管域名匹配。
func buildCnameOriginResolver(db *sql.DB) func(string) (string, string) {
	aIdx := map[string][]string{} // fqdn -> A 记录 IP 列表
	cIdx := map[string]string{}   // fqdn -> CNAME 目标 fqdn
	rows, err := db.Query(`SELECT dr.type, dr.name, c.name, dr.data
		FROM dns_records dr JOIN cis c ON c.id=dr.domain_ci_id
		WHERE dr.type IN ('A','CNAME')`)
	if err == nil {
		for rows.Next() {
			var typ, name, domain, data string
			if rows.Scan(&typ, &name, &domain, &data) != nil {
				continue
			}
			fqdn := normFQDN(recordFQDN(name, domain))
			if typ == "A" {
				aIdx[fqdn] = append(aIdx[fqdn], data)
			} else {
				cIdx[fqdn] = normFQDN(data)
			}
		}
		rows.Close()
	}
	ruleIdx := map[string]string{} // 回源CNAME(归一化) -> 源站IP（映射规则兜底）
	rrows, rerr := db.Query(`SELECT cname, origin_ip FROM origin_ip_rules WHERE origin_ip<>''`)
	if rerr == nil {
		for rrows.Next() {
			var cname, ip string
			if rrows.Scan(&cname, &ip) == nil {
				ruleIdx[normFQDN(cname)] = ip
			}
		}
		rrows.Close()
	}
	// 沿 CNAME 链逐跳解析（≤6 跳）：每个节点先看 A 记录(解析优先)，没 A 就跟 CNAME 往下(可能拿到下游 A/规则)，
	// 下游拿不到再看本节点有没有规则(规则兜底)。这样规则配在链上任意一环都能被上游继承。
	var resolve func(string, int) (string, string)
	resolve = func(fqdn string, depth int) (string, string) {
		if depth < 0 {
			return "", ""
		}
		if ips := aIdx[fqdn]; len(ips) > 0 { // ① 本节点有 A 记录 → 解析（最真实）
			return strings.Join(ips, ","), "解析"
		}
		if tgt, ok := cIdx[fqdn]; ok && tgt != fqdn { // ② 跟 CNAME 往下（优先找到真实 A/下游规则）
			if ip, src := resolve(tgt, depth-1); ip != "" {
				return ip, src
			}
		}
		if ip, ok := ruleIdx[fqdn]; ok { // ③ 本节点有规则 → 规则兜底
			return ip, "规则"
		}
		return "", ""
	}
	return func(cname string) (string, string) { return resolve(normFQDN(cname), 6) }
}

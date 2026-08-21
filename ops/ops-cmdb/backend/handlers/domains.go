package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"golang.org/x/net/publicsuffix"
	"ops-cmdb-backend/internal/store"
)

type DomainHandler struct {
	// 迁移期同时持有：Store 用于有租户语义的读写，DB 供尚未迁移的内部函数。
	Store *store.Store
	DB    *sql.DB
}

func NewDomainHandler(st *store.Store, db *sql.DB) *DomainHandler {
	return &DomainHandler{Store: st, DB: db}
}

func (h *DomainHandler) Register(r *gin.RouterGroup) {
	r.GET("/domains", h.List)
	r.POST("/domains", h.Create)
	r.PUT("/domains/:ciid", h.Update)
	r.DELETE("/domains/:ciid", h.Delete)
	r.POST("/domains/sync", h.Sync)
	r.POST("/domains/refresh-all", h.RefreshAll)
	r.POST("/domains/:ciid/refresh", h.Refresh)
	r.POST("/domains/bulk-ignore", h.BulkIgnore)            // 忽略/取消忽略主域名（忽略后同步跳过、不报未同步）
	r.POST("/domains/bulk-status", h.BulkStatus)            // 批量/单个设主域名生命周期状态
	r.POST("/domains/auto-link-modules", h.AutoLinkModules) // 从 K8s 入口(VS/Ingress hosts) 自动填模块(仅补空的)
}

// BulkStatus 批量或单个设主域名生命周期状态（domains.status）。status 空=清除（回到"未设置"）。
func (h *DomainHandler) BulkStatus(c *gin.Context) {
	var in struct {
		CIIDs  []int64 `json:"ci_ids"`
		Status string  `json:"status"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || len(in.CIIDs) == 0 {
		httpx.Required(c, "ci_ids")
		return
	}
	ph := make([]string, len(in.CIIDs))
	args := []any{in.Status}
	for i, id := range in.CIIDs {
		ph[i] = "?"
		args = append(args, id)
	}
	if _, err := h.DB.Exec(`UPDATE domains SET status=? WHERE ci_id IN (`+strings.Join(ph, ",")+`)`, args...); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true, "count": len(in.CIIDs)})
}

// BulkIgnore 批量忽略/取消忽略主域名。ignored=1 忽略(可带原因)，=0 取消。
func (h *DomainHandler) BulkIgnore(c *gin.Context) {
	var in struct {
		CIIDs   []int64 `json:"ci_ids"`
		Ignored int     `json:"ignored"`
		Reason  string  `json:"reason"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || len(in.CIIDs) == 0 {
		httpx.Required(c, "ci_ids")
		return
	}
	ph := make([]string, len(in.CIIDs))
	args := []any{in.Ignored, in.Reason}
	for i, id := range in.CIIDs {
		ph[i] = "?"
		args = append(args, id)
	}
	if _, err := h.DB.Exec(`UPDATE domains SET ignored=?, ignore_reason=? WHERE ci_id IN (`+strings.Join(ph, ",")+`)`, args...); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	act := "ignore_domain"
	if in.Ignored == 0 {
		act = "unignore_domain"
	}
	SetAuditTarget(c, fmt.Sprintf("%s %d 个", act, len(in.CIIDs)))
	c.JSON(200, gin.H{"ok": true, "count": len(in.CIIDs)})
}

type domainOut struct {
	CIID          int64  `json:"ci_id"`
	Name          string `json:"name"`
	Project       string `json:"project"`
	Env           string `json:"env"`
	Module        string `json:"module"`
	Owner         string `json:"owner"`
	Status        string `json:"status"`
	RegistrarID   *int   `json:"registrar_id"`
	RegistrarName string `json:"registrar_name"`
	DNSProvider   string `json:"dns_provider"`
	ExpiryAt      string `json:"expiry_at"`
	CertExpiryAt  string `json:"cert_expiry_at"`
	CertCheckMsg  string `json:"cert_check_msg"`
	CertCount     int    `json:"cert_count"`
	DnsCount      int    `json:"dns_count"`   // 厂商原始 DNS 记录条数（DNS 记录页展开用）
	LastSynced    string `json:"last_synced"` // 最近一次同步时刻（独立记录，0 记录也算已同步）
	Stale         bool   `json:"stale"`
	DnsMigrated   bool   `json:"dns_migrated"` // 域名还在数据源账户但 DNS 已迁走(NS 非 GoDaddy)
	Ignored       bool   `json:"ignored"`
	IgnoreReason  string `json:"ignore_reason"`
	SourceStatus  string `json:"source_status"` // 数据源(GoDaddy)返回的域名状态，如 ACTIVE/TRANSFERRED_OUT
	Category      string `json:"category"`      // 展示分类：active/pending/dns_migrated/expired/transferred_out/cancelled/ownership/removed/ignored/unknown
	Origin        string `json:"origin"`        // manual=手动录入, sync=数据源同步
	LifeStatus    string `json:"life_status"`   // 主域名生命周期状态（domains.status，可自定义：使用中/备用/未使用/待下线/已下线）
	Suggest       string `json:"suggest"`       // 系统建议（仅未手动标时）：如"未使用"（0业务解析且非回源目标）
}

func (h *DomainHandler) List(c *gin.Context) {
	rows, err := h.DB.Query(`
		SELECT c.id, c.name, c.project, c.env, c.module, c.owner, c.status,
		       d.registrar_id, COALESCE(reg.name,''), d.dns_provider, d.expiry_at,
		       d.cert_expiry_at, d.cert_check_msg, d.stale, d.origin,
		       (SELECT COUNT(*) FROM ci_relations r WHERE r.dst_ci_id=c.id AND r.rel_type='protects'),
		       (SELECT COUNT(*) FROM dns_records dr WHERE dr.domain_ci_id=c.id),
		       d.last_synced_at, d.dns_migrated, d.ignored, d.ignore_reason, d.source_status,
		       d.status,
		       (SELECT COUNT(*) FROM domain_records br WHERE br.domain_ci_id=c.id) AS reso_count,
		       (SELECT COUNT(*) FROM domain_records cr WHERE LOWER(cr.cname)=LOWER(c.name) OR LOWER(cr.cname) LIKE CONCAT('%.', LOWER(c.name))) AS cname_ref
		FROM cis c
		JOIN domains d ON d.ci_id=c.id
		LEFT JOIN registrars reg ON reg.id=d.registrar_id
		WHERE c.type='domain'
		ORDER BY d.stale, c.id DESC`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []domainOut{}
	for rows.Next() {
		var o domainOut
		var regID sql.NullInt64
		var exp, certExp, lastSync sql.NullTime
		var stale, migrated, ignored int
		var resoCount, cnameRef int
		if err := rows.Scan(&o.CIID, &o.Name, &o.Project, &o.Env, &o.Module, &o.Owner, &o.Status,
			&regID, &o.RegistrarName, &o.DNSProvider, &exp, &certExp, &o.CertCheckMsg, &stale, &o.Origin, &o.CertCount,
			&o.DnsCount, &lastSync, &migrated, &ignored, &o.IgnoreReason, &o.SourceStatus,
			&o.LifeStatus, &resoCount, &cnameRef); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		o.Stale = stale == 1
		o.DnsMigrated = migrated == 1
		o.Ignored = ignored == 1
		// 系统建议（仅未手动标状态时给）：0 业务解析 且 不是别的域名的回源目标 → 建议未使用
		if o.LifeStatus == "" && resoCount == 0 && cnameRef == 0 {
			o.Suggest = "未使用"
		}
		if lastSync.Valid {
			o.LastSynced = lastSync.Time.Format("2006-01-02 15:04")
		}
		if regID.Valid {
			v := int(regID.Int64)
			o.RegistrarID = &v
		}
		expiryPast := exp.Valid && exp.Time.Before(time.Now())
		if exp.Valid {
			o.ExpiryAt = exp.Time.Format("2006-01-02")
		}
		o.Category = domainCategory(o.SourceStatus, o.Stale, o.DnsMigrated, o.Ignored, expiryPast)
		if certExp.Valid {
			o.CertExpiryAt = certExp.Time.Format("2006-01-02")
		}
		out = append(out, o)
	}
	c.JSON(http.StatusOK, out)
}

// normalizeDomain 归一到注册域名(eTLD+1)：去空格/转小写/去协议与路径，www.baidu.com → baidu.com。
func normalizeDomain(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.TrimPrefix(name, "http://")
	name = strings.TrimPrefix(name, "https://")
	if i := strings.IndexAny(name, "/:"); i >= 0 {
		name = name[:i]
	}
	name = strings.Trim(name, ".")
	if name == "" {
		return ""
	}
	if root, err := publicsuffix.EffectiveTLDPlusOne(name); err == nil && root != "" {
		return root
	}
	return name
}

// domainPatch 部分更新用。
//
// 🔴 字段必须是**指针**：nil = 没传（不动它），非 nil 指向零值 = 显式清空。
//
//	用普通 string 的话这两件事在 Go 里完全一样，
//	于是"只想改到期日"变成了"把其余字段全清空"（OPSCMDB-083）。
type domainPatch struct {
	Name        *string           `json:"name"`
	Project     *string           `json:"project"`
	Env         *string           `json:"env"`
	Module      *string           `json:"module"`
	Owner       *string           `json:"owner"`
	Status      *string           `json:"status"`
	RegistrarID *int              `json:"registrar_id"`
	DNSProvider *string           `json:"dns_provider"`
	ExpiryAt    *string           `json:"expiry_at"` // "2006-01-02"；空串 = 清掉
	Labels      map[string]string `json:"labels"`
}

type domainIn struct {
	Name        string            `json:"name"`
	Project     string            `json:"project"`
	Env         string            `json:"env"`
	Module      string            `json:"module"`
	Owner       string            `json:"owner"`
	Status      string            `json:"status"`
	RegistrarID *int              `json:"registrar_id"`
	DNSProvider string            `json:"dns_provider"`
	ExpiryAt    string            `json:"expiry_at"` // "2006-01-02" 或空
	Labels      map[string]string `json:"labels"`
}

func (h *DomainHandler) Create(c *gin.Context) {
	var in domainIn
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	in.Name = normalizeDomain(in.Name)
	if in.Name == "" {
		httpx.Required(c, "name")
		return
	}
	if in.Status == "" {
		in.Status = "active"
	}
	tx, err := h.DB.Begin()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	// ⚠️ 用 tx 查而不是 h.DB：事务外的查询看不到本事务的插入，
	//	并发下两个请求会同时认为"不存在"，照样造出两条同名域名
	if id, dup := ciExists(tx, "domain", in.Name); dup {
		tx.Rollback()
		failDuplicate(c, "域名", in.Name, id)
		return
	}
	res, err := tx.Exec(`INSERT INTO cis (type, name, project, env, module, owner, status) VALUES ('domain', ?, ?, ?, ?, ?, ?)`,
		in.Name, in.Project, in.Env, in.Module, in.Owner, in.Status)
	if err != nil {
		tx.Rollback()
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	ciID, _ := res.LastInsertId()
	if _, err := tx.Exec(`INSERT INTO domains (ci_id, registrar_id, dns_provider, expiry_at, origin) VALUES (?, ?, ?, NULLIF(?, ''), 'manual')`,
		ciID, nullableInt(in.RegistrarID), in.DNSProvider, in.ExpiryAt); err != nil {
		tx.Rollback()
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	replaceLabelsDB(h.DB, ciID, in.Labels)
	// 域名跨两张表：cis 存通用配置项属性，domains 存域名特有字段（注册商/到期日）。
	// 两条都记，回滚时才能把这个域名完整还原回去。
	AuditCreated(c, "cis", ciID)
	AuditCreated(c, "domains", ciID)
	SetAuditTarget(c, in.Name)
	c.JSON(201, gin.H{"ci_id": ciID})
}

// Update 部分更新一个域名。
//
// 🔴 PATCH 语义：**只改传过来的字段**（见 patchset.go）。
//
//	原来是一次 UPDATE 写死六列，值取自绑定后的结构体 ——
//	`{"expiry_at": "..."}` 这样一发，域名的 name 就被空串覆盖了（OPSCMDB-083）。
//	而 name 是域名的身份：清空后列表显示成一行空白，
//	按名字关联的逻辑（证书匹配、DNS 对账、拓扑连边）全部失联，
//	且**看不出发生过什么** —— 没有报错、没有审计差异提示。
func (h *DomainHandler) Update(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ciID := c.Param("ciid")
	var in domainPatch
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	// ⚠️ name 可以不传，但传了就不能是空 —— 空名字的域名不是一条合法记录，
	//	它只会在列表里显示成一行空白。想删就走 DELETE。
	if in.Name != nil && strings.TrimSpace(*in.Name) == "" {
		httpx.Invalid(c, "name", "非空")
		return
	}

	ci, dom := buildDomainPatch(in)

	// 一个字段都没传：多半是调用方写错了。返回 200 会让它以为改成功了。
	if ci.Empty() && dom.Empty() && in.Labels == nil {
		httpx.Invalid(c, "body", "至少要传一个要改的字段")
		return
	}

	// 先动 cis：改不到就说明这个 ciid 不属于本租户或不是域名，
	// 一行子数据都不碰（与 Delete 同一顺序，理由见那里）。
	if !ci.Empty() {
		res, err := sc.Exec(`UPDATE cis SET `+ci.SQL()+` WHERE tenant_id = ? AND id=? AND type='domain'`,
			append(ci.Args(), ciID)...)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			httpx.NotFound(c, "domain")
			return
		}
	} else {
		// 只改 domains 表时也要先确认这条记录归本租户 ——
		// 否则越权的 ciid 会绕过上面那道检查直接写子表
		var one int
		if sc.QueryRow(`SELECT 1 FROM cis WHERE tenant_id = ? AND id=? AND type='domain'`, ciID).
			Scan(&one) != nil {
			httpx.NotFound(c, "domain")
			return
		}
	}
	if !dom.Empty() {
		if _, err := sc.Exec(`UPDATE domains SET `+dom.SQL()+` WHERE tenant_id = ? AND ci_id=?`,
			append(dom.Args(), ciID)...); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	if in.Labels != nil {
		if id, err := parseID(ciID); err == nil {
			replaceLabelsDB(h.DB, id, in.Labels)
		}
	}
	c.JSON(200, gin.H{"ok": true})
}

// buildDomainPatch 把请求体拆成两张表各自的 SET 子句。
//
// 抽出来是为了能不连数据库直接测：这段逻辑的正确性完全在于
// **哪些字段进了 SET、哪些没进**，而那正是 OPSCMDB-083 出错的地方。
func buildDomainPatch(in domainPatch) (ci, dom *patchSet) {
	ci = &patchSet{}
	ci.Add("name", in.Name)
	ci.Add("project", in.Project)
	ci.Add("env", in.Env)
	ci.Add("module", in.Module)
	ci.Add("owner", in.Owner)
	ci.Add("status", in.Status)

	dom = &patchSet{}
	dom.AddExpr(in.RegistrarID != nil, "registrar_id=?", nullableInt(in.RegistrarID))
	dom.Add("dns_provider", in.DNSProvider)
	// 空串 = 显式清掉到期日（存 NULL），与"没传"是两回事
	dom.AddExpr(in.ExpiryAt != nil, "expiry_at=NULLIF(?, '')", derefStr(in.ExpiryAt))
	return ci, dom
}

// derefStr *string 取值，nil 当空串。只在已确认非 nil 的分支里用。
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func (h *DomainHandler) Delete(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	ciID := c.Param("ciid")
	tx, err := sc.Begin()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer tx.Rollback() //nolint:errcheck // Commit 成功后是 no-op
	// ⚠️ 先删父行（cis）。删不到就说明这个 ciid 不属于本租户，直接返回，
	//    一行子数据都不碰。原来的顺序是父行最后删 —— 别的租户的 ciid
	//    传进来会先把人家的解析记录全清掉，最后才发现父行删不动。
	res, err := tx.Exec(`DELETE FROM cis WHERE tenant_id = ? AND id=? AND type='domain'`, ciID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.NotFound(c, "domain")
		return
	}
	for _, stmt := range []string{
		`DELETE FROM domain_records WHERE tenant_id = ? AND domain_ci_id=?`, // 级联删业务解析（否则孤儿数据）
		`DELETE FROM dns_records WHERE tenant_id = ? AND domain_ci_id=?`,    // 级联删厂商原始记录
		`DELETE FROM ci_labels WHERE tenant_id = ? AND ci_id=?`,
		`DELETE FROM domains WHERE tenant_id = ? AND ci_id=?`,
	} {
		if _, err := tx.Exec(stmt, ciID); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}
	_, _ = tx.Exec(`DELETE FROM ci_relations WHERE tenant_id = ? AND (src_ci_id=? OR dst_ci_id=?)`, ciID, ciID)
	if err := tx.Commit(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	SetAuditTarget(c, ciID)
	c.JSON(200, gin.H{"ok": true})
}

// Sync 从注册商同步域名：第一期先支持手动录入，自动同步按 provider 接入（迭代）。
func (h *DomainHandler) Sync(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"synced": 0,
		"msg":    "自动同步需按注册商 provider 接入，当前请用手动录入；凭据已可配置供证书签发使用",
	})
}

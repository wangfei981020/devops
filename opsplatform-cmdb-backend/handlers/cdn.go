package handlers

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/cdnsource"
	"opsplatform-cmdb-backend/crypto"
	"opsplatform-cmdb-backend/logx"
)

// CDN（Cloudflare）只读接入。
//
// CDN 此前是整条链路上唯一的黑洞：能查到域名在注册商那儿的到期时间、能查到 K8s 里的
// Service 和 Pod，唯独中间这一跳——解析到哪、是否走了 CDN、SSL 什么模式——只能登录
// Cloudflare 控制台看。域名类故障因此总是缺最前面一环。
//
// Token 由用户在 CMDB 界面配置并加密存储，与 registrars/cloud_accounts 同一套做法；
// MCP 只暴露查询接口，不暴露账号管理，AI 接触不到凭证。

type CDNHandler struct {
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewCDNHandler(db *sql.DB, cipher *crypto.Cipher) *CDNHandler {
	return &CDNHandler{DB: db, Cipher: cipher}
}

// Register 账号管理走登录态（含写操作：配置 token）。
func (h *CDNHandler) Register(r *gin.RouterGroup) {
	r.GET("/cdn/accounts", h.ListAccounts)
	r.POST("/cdn/accounts", h.SaveAccount)
	r.DELETE("/cdn/accounts/:id", h.DeleteAccount)
	r.POST("/cdn/accounts/:id/verify", h.VerifyAccount)
	r.POST("/cdn/accounts/:id/sync", h.SyncAccount)
	// 只读查询（MCP 也走这几个）
	r.GET("/cdn/zones", h.ListZones)
	r.GET("/cdn/dns-records", h.ListDNSRecords)
	r.GET("/cdn/domain-check", h.DomainCheck)
}

func (h *CDNHandler) ListAccounts(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT a.id, a.cdn_id, COALESCE(d.name,''), a.name, a.account_tag, a.enabled,
		a.last_sync_at, a.last_result, (a.cred_enc IS NOT NULL AND a.cred_enc<>'') AS has_cred
		FROM cdn_accounts a LEFT JOIN cdns d ON d.id=a.cdn_id ORDER BY a.id`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, cdnID, enabled, hasCred int
		var cdnName, name, tag, lastResult string
		var lastSync sql.NullTime
		if rows.Scan(&id, &cdnID, &cdnName, &name, &tag, &enabled, &lastSync, &lastResult, &hasCred) != nil {
			continue
		}
		ls := ""
		if lastSync.Valid {
			ls = lastSync.Time.Format("2006-01-02 15:04:05")
		}
		// 绝不回显 token，只告诉前端配没配
		out = append(out, gin.H{"id": id, "cdn_id": cdnID, "cdn": cdnName, "name": name,
			"account_tag": tag, "enabled": enabled == 1, "last_sync_at": ls,
			"last_result": lastResult, "has_credential": hasCred == 1})
	}
	c.JSON(http.StatusOK, out)
}

func (h *CDNHandler) SaveAccount(c *gin.Context) {
	var in struct {
		ID         int    `json:"id"`
		CDNID      int    `json:"cdn_id"`
		Name       string `json:"name"`
		Token      string `json:"token"`
		AccountTag string `json:"account_tag"`
		Enabled    *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" || in.CDNID == 0 {
		c.JSON(400, gin.H{"error": "cdn_id/name 必填"})
		return
	}
	enabled := 1
	if in.Enabled != nil && !*in.Enabled {
		enabled = 0
	}
	if in.ID > 0 {
		// token 留空表示不修改，避免前端不回显导致误清空
		if in.Token == "" {
			_, err := h.DB.Exec(`UPDATE cdn_accounts SET cdn_id=?, name=?, account_tag=?, enabled=? WHERE id=?`,
				in.CDNID, in.Name, in.AccountTag, enabled, in.ID)
			if err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
		} else {
			enc, err := h.Cipher.Encrypt(in.Token)
			if err != nil {
				c.JSON(500, gin.H{"error": "加密失败: " + err.Error()})
				return
			}
			if _, err := h.DB.Exec(`UPDATE cdn_accounts SET cdn_id=?, name=?, cred_enc=?, account_tag=?, enabled=? WHERE id=?`,
				in.CDNID, in.Name, enc, in.AccountTag, enabled, in.ID); err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
		}
		WriteAudit(h.DB, c, "update_cdn_account", in.Name)
		c.JSON(http.StatusOK, gin.H{"ok": true, "id": in.ID})
		return
	}
	if in.Token == "" {
		c.JSON(400, gin.H{"error": "新增账号必须提供 token"})
		return
	}
	enc, err := h.Cipher.Encrypt(in.Token)
	if err != nil {
		c.JSON(500, gin.H{"error": "加密失败: " + err.Error()})
		return
	}
	res, err := h.DB.Exec(`INSERT INTO cdn_accounts (cdn_id,name,cred_enc,account_tag,enabled) VALUES (?,?,?,?,?)`,
		in.CDNID, in.Name, enc, in.AccountTag, enabled)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	WriteAudit(h.DB, c, "create_cdn_account", in.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": id})
}

func (h *CDNHandler) DeleteAccount(c *gin.Context) {
	id := c.Param("id")
	for _, t := range []string{"cdn_zone_settings", "cdn_dns_records", "cdn_zones"} {
		_, _ = h.DB.Exec("DELETE FROM "+t+" WHERE account_id=?", id)
	}
	if _, err := h.DB.Exec(`DELETE FROM cdn_accounts WHERE id=?`, id); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "delete_cdn_account", id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// VerifyAccount 验一下 token 能不能用。配错了当场就知道，不用等到同步失败再查。
func (h *CDNHandler) VerifyAccount(c *gin.Context) {
	cli, err := h.clientFor(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	if err := cli.Verify(ctx); err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "message": "token 有效"})
}

func (h *CDNHandler) SyncAccount(c *gin.Context) {
	id, _ := strconv.Atoi(c.Param("id"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	zones, records, err := h.syncOne(ctx, id)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
		return
	}
	WriteAudit(h.DB, c, "sync_cdn_account", c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ok": true, "zones": zones, "dns_records": records})
}

func (h *CDNHandler) clientFor(id string) (*cdnsource.Client, error) {
	var enc sql.NullString
	var provider string
	if err := h.DB.QueryRow(`SELECT a.cred_enc, COALESCE(d.name,'')
		FROM cdn_accounts a LEFT JOIN cdns d ON d.id=a.cdn_id WHERE a.id=?`, id).Scan(&enc, &provider); err != nil {
		return nil, err
	}
	// cdnsource 目前只实现了 Cloudflare 适配。厂商对不上必须当场报清楚，
	// 否则会拿着别家的 token 去调 api.cloudflare.com，报出来的认证错误指不到真正的原因。
	if !strings.EqualFold(strings.TrimSpace(provider), "cloudflare") {
		return nil, &cdnError{"暂未接入该 CDN 厂商（" + provider + "）：目前只实现了 Cloudflare 适配"}
	}
	if !enc.Valid || enc.String == "" {
		return nil, errNoCredential
	}
	tok, err := h.Cipher.Decrypt(enc.String)
	if err != nil {
		return nil, err
	}
	return cdnsource.NewCloudflare(tok), nil
}

var errNoCredential = &cdnError{"该账号尚未配置 API Token"}

type cdnError struct{ msg string }

func (e *cdnError) Error() string { return e.msg }

// syncOne 全量同步一个账号：zones → 每个 zone 的 DNS 记录与设置。
// 与 K8s 采集同样的原则：整体成功才落库，失败保留上一轮完整数据。
func (h *CDNHandler) syncOne(ctx context.Context, accountID int) (int, int, error) {
	cli, err := h.clientFor(strconv.Itoa(accountID))
	if err != nil {
		h.markSync(accountID, "失败: "+err.Error())
		return 0, 0, err
	}
	zones, err := cli.ListZones(ctx)
	if err != nil {
		h.markSync(accountID, "失败: "+err.Error())
		return 0, 0, err
	}

	zoneRows := make([][]any, 0, len(zones))
	recRows := [][]any{}
	setRows := [][]any{}
	for _, z := range zones {
		zoneRows = append(zoneRows, []any{accountID, z.ZoneID, z.Name, z.Status, boolInt(z.Paused),
			z.Plan, strings.Join(z.NameServers, ",")})
		recs, err := cli.ListDNSRecords(ctx, z.ZoneID)
		if err != nil {
			h.markSync(accountID, "失败(取 "+z.Name+" 的 DNS 记录): "+err.Error())
			return 0, 0, err
		}
		for _, r := range recs {
			recRows = append(recRows, []any{accountID, z.ZoneID, z.Name, r.RecordID,
				r.Type, r.Name, r.Content, boolInt(r.Proxied), r.TTL})
		}
		// 设置项拿不到不致命（token 可能没有 zone settings 读权限），跳过即可
		if sets, err := cli.ListZoneSettings(ctx, z.ZoneID); err == nil {
			for _, s := range sets {
				setRows = append(setRows, []any{accountID, z.ZoneID, z.Name, s.Name, s.Value})
			}
		}
	}

	tx, err := h.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	for _, t := range []string{"cdn_zones", "cdn_dns_records", "cdn_zone_settings"} {
		if _, err := tx.Exec("DELETE FROM "+t+" WHERE account_id=?", accountID); err != nil {
			return 0, 0, err
		}
	}
	if err := txInsert(tx, "cdn_zones",
		[]string{"account_id", "zone_id", "name", "status", "paused", "plan", "name_servers"}, zoneRows); err != nil {
		return 0, 0, err
	}
	if err := txInsert(tx, "cdn_dns_records",
		[]string{"account_id", "zone_id", "zone_name", "record_id", "type", "name", "content", "proxied", "ttl"}, recRows); err != nil {
		return 0, 0, err
	}
	if err := txInsert(tx, "cdn_zone_settings",
		[]string{"account_id", "zone_id", "zone_name", "name", "value"}, setRows); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	h.markSync(accountID, "成功")
	logx.J("cdn", "sync", map[string]any{"account_id": accountID, "zones": len(zoneRows), "records": len(recRows)})
	return len(zoneRows), len(recRows), nil
}

// CDNSyncIntervalHours CDN 配置变更频率远低于 K8s 资源，按小时同步足够，
// 也避免无谓消耗 Cloudflare 的 API 配额。
const CDNSyncIntervalHours = 6

// StartCDNScheduler 周期同步所有启用的 CDN 账号。单个账号失败不影响其余。
func StartCDNScheduler(db *sql.DB, cipher *crypto.Cipher) {
	h := NewCDNHandler(db, cipher)
	time.Sleep(90 * time.Second) // 让进程与 DB 先就绪，也错开启动时的其它同步
	for {
		rows, err := db.Query(`SELECT id FROM cdn_accounts WHERE enabled=1 AND cred_enc IS NOT NULL AND cred_enc<>''`)
		if err == nil {
			ids := []int{}
			for rows.Next() {
				var id int
				if rows.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
			rows.Close()
			for _, id := range ids {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				z, r, err := h.syncOne(ctx, id)
				cancel()
				if err != nil {
					logx.J("cdn", "sync_err", map[string]any{"account_id": id, "err": err.Error()})
					continue
				}
				logx.J("cdn", "synced", map[string]any{"account_id": id, "zones": z, "records": r})
			}
		}
		time.Sleep(CDNSyncIntervalHours * time.Hour)
	}
}

func (h *CDNHandler) markSync(id int, result string) {
	_, _ = h.DB.Exec(`UPDATE cdn_accounts SET last_sync_at=NOW(), last_result=? WHERE id=?`, trunc255(result), id)
}

// ---- 只读查询 ----

func (h *CDNHandler) ListZones(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT z.id, z.account_id, a.name, z.zone_id, z.name, z.status, z.paused, z.plan,
		z.name_servers, z.synced_at,
		(SELECT COUNT(*) FROM cdn_dns_records r WHERE r.account_id=z.account_id AND r.zone_id=z.zone_id) AS dns_count,
		(SELECT COALESCE(s.value,'') FROM cdn_zone_settings s WHERE s.account_id=z.account_id AND s.zone_id=z.zone_id AND s.name='ssl') AS ssl_mode
		FROM cdn_zones z LEFT JOIN cdn_accounts a ON a.id=z.account_id ORDER BY z.name`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, aid, paused, dnsCount int
		var acc, zid, name, status, plan, ns, ssl string
		var synced time.Time
		if rows.Scan(&id, &aid, &acc, &zid, &name, &status, &paused, &plan, &ns, &synced, &dnsCount, &ssl) != nil {
			continue
		}
		item := gin.H{"id": id, "account": acc, "zone_id": zid, "name": name, "status": status,
			"paused": paused == 1, "plan": plan, "name_servers": ns, "dns_count": dnsCount,
			"ssl_mode": ssl, "synced_at": synced.Format("2006-01-02 15:04:05")}
		// ssl=flexible 表示 CF 到源站是明文，用户看到的是小锁但回源没有加密
		if strings.EqualFold(ssl, "flexible") {
			item["risk"] = "SSL 模式为 flexible：CDN 到源站是明文传输，浏览器却显示已加密，建议改为 full/strict"
		}
		if status != "active" {
			item["risk"] = "Zone 状态为 " + status + "（非 active），配置可能未生效"
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, out)
}

func (h *CDNHandler) ListDNSRecords(c *gin.Context) {
	q := `SELECT zone_name, record_id, type, name, content, proxied, ttl FROM cdn_dns_records WHERE 1=1`
	args := []any{}
	if z := c.Query("zone"); z != "" {
		q += " AND zone_name=?"
		args = append(args, z)
	}
	if t := c.Query("type"); t != "" {
		q += " AND type=?"
		args = append(args, t)
	}
	if kw := strings.TrimSpace(c.Query("q")); kw != "" {
		q += " AND (name LIKE ? OR content LIKE ?)"
		args = append(args, "%"+kw+"%", "%"+kw+"%")
	}
	q += " ORDER BY zone_name, name"
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var zone, rid, typ, name, content string
		var proxied, ttl int
		if rows.Scan(&zone, &rid, &typ, &name, &content, &proxied, &ttl) != nil {
			continue
		}
		out = append(out, gin.H{"zone": zone, "record_id": rid, "type": typ, "name": name,
			"content": content, "proxied": proxied == 1, "ttl": ttl,
			"via_cdn": proxied == 1})
	}
	c.JSON(http.StatusOK, out)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func trunc255(s string) string {
	if len(s) <= 255 {
		return s
	}
	return s[:255]
}

// txInsert 事务内批量插入，分批避免占位符超限。
func txInsert(tx *sql.Tx, table string, cols []string, rows [][]any) error {
	const batch = 300
	if len(rows) == 0 {
		return nil
	}
	one := "(" + strings.TrimSuffix(strings.Repeat("?,", len(cols)), ",") + ")"
	for start := 0; start < len(rows); start += batch {
		end := start + batch
		if end > len(rows) {
			end = len(rows)
		}
		part := rows[start:end]
		args := make([]any, 0, len(part)*len(cols))
		for _, r := range part {
			args = append(args, r...)
		}
		q := "INSERT INTO " + table + " (" + strings.Join(cols, ",") + ") VALUES " +
			strings.TrimSuffix(strings.Repeat(one+",", len(part)), ",")
		if _, err := tx.Exec(q, args...); err != nil {
			return err
		}
	}
	return nil
}

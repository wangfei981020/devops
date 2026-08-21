package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"ops-cmdb-backend/internal/httpx"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/cdnsource"
	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/logx"
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
	// 迁移期同时持有：Store 用于有租户语义的读写，DB 供尚未迁移的内部函数。
	Store  *store.Store
	DB     *sql.DB
	Cipher *crypto.Cipher
}

func NewCDNHandler(st *store.Store, db *sql.DB, cipher *crypto.Cipher) *CDNHandler {
	return &CDNHandler{Store: st, DB: db, Cipher: cipher}
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
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	rows, err := sc.Query(`SELECT a.id, a.cdn_id, COALESCE(d.name,''), a.name, a.account_tag, a.enabled,
		a.last_sync_at, a.last_result, (a.cred_enc IS NOT NULL AND a.cred_enc<>'') AS has_cred
		FROM cdn_accounts a LEFT JOIN cdns d ON d.id=a.cdn_id WHERE a.tenant_id = ? ORDER BY a.id`)
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
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	var in struct {
		ID         int     `json:"id"`
		CDNID      int     `json:"cdn_id"`
		Name       string  `json:"name"`
		Token      string  `json:"token"`
		AccountTag *string `json:"account_tag"` // 指针：区分"没传"与"清空"
		Enabled    *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" || in.CDNID == 0 {
		httpx.Required(c, "cdn_id/name")
		return
	}
	// 新建时的缺省：没说就是启用。
	// ⚠️ **更新时不能用这个缺省** —— 见下面 patchSet 那段的说明。
	enabled := 1
	if in.Enabled != nil && !*in.Enabled {
		enabled = 0
	}
	if in.ID > 0 {
		p := &patchSet{}
		// name / cdn_id 上面已强制必填，这里一定有值
		p.Add("cdn_id", &in.CDNID)
		p.Add("name", &in.Name)
		// 🔴 account_tag 与 enabled 都按**三态**处理：
		//
		//	`enabled` 原来是 `没传 → 视为 true`。于是停用了一个账号之后，
		//	任何一次不带 enabled 的保存都会把它**重新启用** ——
		//	而界面上只显示"已保存"，没人会想到自己刚把它打开了（OPSCMDB-083）。
		//	`account_tag` 同理：不传就被空串覆盖。
		p.Add("account_tag", in.AccountTag)
		if in.Enabled != nil {
			v := 0
			if *in.Enabled {
				v = 1
			}
			p.Add("enabled", &v)
		}
		// token 留空表示不修改，避免前端不回显导致误清空
		if in.Token != "" {
			enc, err := h.Cipher.Encrypt(in.Token)
			if err != nil {
				httpx.Fail(c, httpx.CodeInternal, fmt.Errorf("加密失败: %w", err), nil)
				return
			}
			p.Add("cred_enc", &enc)
		}
		// ★ 越权修复：原为 `WHERE id=?`，任何租户都能改别人的 CDN 账号
		if _, err := sc.Exec(`UPDATE cdn_accounts SET `+p.SQL()+` WHERE tenant_id = ? AND id=?`,
			append(p.Args(), in.ID)...); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		SetAuditTarget(c, in.Name)
		c.JSON(http.StatusOK, gin.H{"ok": true, "id": in.ID})
		return
	}
	if in.Token == "" {
		httpx.Required(c, "token")
		return
	}
	enc, err := h.Cipher.Encrypt(in.Token)
	if err != nil {
		httpx.Fail(c, httpx.CodeInternal, fmt.Errorf("加密失败: %w", err), nil)
		return
	}
	res, err := sc.Insert(`INSERT INTO cdn_accounts (tenant_id,cdn_id,name,cred_enc,account_tag,enabled) VALUES (?,?,?,?,?,?)`,
		in.CDNID, in.Name, enc, derefStr(in.AccountTag), enabled)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	AuditCreated(c, "cdn_accounts", id)
	SetAuditTarget(c, in.Name)
	c.JSON(http.StatusOK, gin.H{"ok": true, "id": id})
}

func (h *CDNHandler) DeleteAccount(c *gin.Context) {
	sc, err := h.Store.Tenant(c.Request.Context())
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	id := c.Param("id")
	// 先删主表：影响 0 行说明这个账号不属于本租户，此时**不能**去删子表 ——
	// 否则拿别人的 account_id 调一次就把人家的 zone 数据清了。
	res, err := sc.Exec(`DELETE FROM cdn_accounts WHERE tenant_id = ? AND id=?`, id)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.NotFound(c, "account")
		return
	}
	for _, t := range []string{"cdn_zone_settings", "cdn_dns_records", "cdn_zones"} {
		_, _ = sc.Exec("DELETE FROM "+t+" WHERE tenant_id = ? AND account_id=?", id)
	}
	SetAuditTarget(c, id)
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
	// ⚠️ 字段名必须是 `msg`：前端的 actionMessage 读的是 msg / msg_key
	//	（lib/actionMessage.ts）。原来发的是 `message`，于是这条提示
	//	在界面上从来没显示过，一直落到"已保存"那句兜底文案上。
	c.JSON(http.StatusOK, gin.H{"ok": true,
		"msg_key": "cdn:tokenValid", "msg": "token 有效"})
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
	SetAuditTarget(c, c.Param("id"))
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
		h.markSync(ctx, accountID, "失败: "+err.Error())
		return 0, 0, err
	}
	zones, err := cli.ListZones(ctx)
	if err != nil {
		h.markSync(ctx, accountID, "失败: "+err.Error())
		return 0, 0, err
	}

	zoneRows := make([][]any, 0, len(zones))
	recRows := [][]any{}
	setRows := [][]any{}
	ruleRows := [][]any{}
	certRows := [][]any{}
	for _, z := range zones {
		zoneRows = append(zoneRows, []any{accountID, z.ZoneID, z.Name, z.Status, boolInt(z.Paused),
			z.Plan, strings.Join(z.NameServers, ",")})
		recs, err := cli.ListDNSRecords(ctx, z.ZoneID)
		if err != nil {
			h.markSync(ctx, accountID, "失败(取 "+z.Name+" 的 DNS 记录): "+err.Error())
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
		// 规则同理：token 可能没有 Page Rules / Config 读权限。拿不到只跳过这个 zone，
		// 不影响已经采到的 DNS——但日志里会留下 [cf-rules] ERROR 说明原因。
		if rules, err := cli.ListPageRules(ctx, z.ZoneID); err == nil {
			for _, r := range rules {
				ruleRows = append(ruleRows, ruleRow(accountID, z.ZoneID, z.Name, r))
			}
		}
		if rules, err := cli.ListRulesets(ctx, z.ZoneID); err == nil {
			for _, r := range rules {
				ruleRows = append(ruleRows, ruleRow(accountID, z.ZoneID, z.Name, r))
			}
		}
		// 边缘证书：需要 SSL and Certificates·Read 权限，没有就跳过（日志里有 [cf-cert] ERROR）
		if certs, err := cli.ListCertificates(ctx, z.ZoneID); err == nil {
			for _, ct := range certs {
				certRows = append(certRows, []any{accountID, z.ZoneID, z.Name, ct.PackID, ct.Type,
					strings.Join(ct.Hosts, ","), ct.Issuer, ct.Status, parseCFTime(ct.ExpiresOn), time.Now()})
			}
		}
	}

	tx, err := h.DB.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	for _, t := range []string{"cdn_zones", "cdn_dns_records", "cdn_zone_settings", "cdn_rules", "cdn_certificates"} {
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
	if err := txInsert(tx, "cdn_rules",
		[]string{"account_id", "zone_id", "zone_name", "source", "rule_id", "name", "phase", "kind",
			"priority", "status", "expression", "actions", "last_updated", "synced_at"}, ruleRows); err != nil {
		return 0, 0, err
	}
	if err := txInsert(tx, "cdn_certificates",
		[]string{"account_id", "zone_id", "zone_name", "pack_id", "type", "hosts",
			"issuer", "status", "expires_on", "synced_at"}, certRows); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	h.markSync(ctx, accountID, "成功")
	logx.J("cdn", "sync", map[string]any{"account_id": accountID, "zones": len(zoneRows), "records": len(recRows), "rules": len(ruleRows)})
	return len(zoneRows), len(recRows), nil
}

// CDNSyncIntervalHours CDN 配置变更频率远低于 K8s 资源，按小时同步足够，
// 也避免无谓消耗 Cloudflare 的 API 配额。
const CDNSyncIntervalHours = 6

// StartCDNScheduler 周期同步所有启用的 CDN 账号。单个账号失败不影响其余。
//
// # 后台任务与租户
//
// 调度器没有 HTTP 请求，也就没有租户上下文。它的正确形态是**按租户遍历**：
// 查出 (tenant_id, account_id) 的清单，为每一条构造该租户的上下文再执行。
//
// 绝不能图省事用一个"超级上下文"跳过隔离 —— 那样同步逻辑里任何一处写操作
// 都会失去租户约束，而后台任务的错误往往没人看，能悄悄跑很久。
//
// ⚠️ 这里的清单查询本身是跨租户的（要拿到所有租户的账号），
// 用 Raw 而非 store —— 这是调度器的固有属性，不是绕过隔离。
// 阶段 3 做分片调度时，这段会换成 leader 派发、worker 按租户领取。
// StartCDNScheduler 周期同步 CDN 的 Zone/DNS/设置。
//
// canRun 决定本轮要不要真的干活（多副本下只让 leader 跑）。
// 传 nil 表示不做约束 —— 单副本与本地开发是这个行为。
func StartCDNScheduler(st *store.Store, db *sql.DB, cipher *crypto.Cipher, canRun func() bool) {
	h := NewCDNHandler(st, db, cipher)
	time.Sleep(90 * time.Second) // 让进程与 DB 先就绪，也错开启动时的其它同步
	for {
		if canRun != nil && !canRun() {
			time.Sleep(60 * time.Second)
			continue
		}
		type job struct {
			tenant store.TenantID
			id     int
		}
		rows, err := db.Query(`SELECT tenant_id, id FROM cdn_accounts WHERE enabled=1 AND cred_enc IS NOT NULL AND cred_enc<>''`)
		if err == nil {
			jobs := []job{}
			for rows.Next() {
				var j job
				if rows.Scan(&j.tenant, &j.id) == nil {
					jobs = append(jobs, j)
				}
			}
			rows.Close()
			for _, j := range jobs {
				id := j.id
				base, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				ctx := store.WithTenant(base, j.tenant)
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

// markSync 回写同步结果。
//
// 接受 ctx 而不是 gin.Context：它既被 HTTP 触发的手动同步调用，
// 也被后台调度器调用，后者没有请求上下文。
func (h *CDNHandler) markSync(ctx context.Context, id int, result string) {
	// 🔴 这里原来是 `if err != nil { return }` —— **静默失败**。
	//
	//	数据本身是用 tx（裸 *sql.DB）写的，而这条回写走 Scoped。
	//	两条路径不同：后台调度器的 ctx 里没有租户信息时，
	//	`Store.Tenant` 返回错误，这里直接 return —— 什么都不做、也不留痕。
	//
	//	结果就是界面上那个自相矛盾的画面（OPSCMDB-031 P1-14）：
	//	  CDN 账号：**从未同步**
	//	  同一页下方：6 个站点的数据好好地列着
	//	用户看到"从未同步"会去点「立即同步」，或者怀疑那 6 个站点的数据不可信。
	//
	//	⚠️ 回写失败必须留痕。这条 UPDATE 失败不影响已经落库的数据，
	//	所以不该让整次同步失败；但它会让**时间戳永远停在过去**，
	//	而那正是判断"数据新不新"的唯一依据。
	sc, err := h.Store.Tenant(ctx)
	if err != nil {
		logx.J("cdn", "mark_sync_no_tenant", map[string]any{
			"account_id": id, "err": err.Error(),
			"note": "同步时间戳没能回写 —— 数据已经落库，但界面会一直显示「从未同步」。" +
				"多半是调用方的 ctx 里没有租户信息",
		})
		return
	}
	if _, err := sc.Exec(`UPDATE cdn_accounts SET last_sync_at=NOW(), last_result=? WHERE tenant_id = ? AND id=?`,
		trunc255(result), id); err != nil {
		logx.J("cdn", "mark_sync_failed", map[string]any{
			"account_id": id, "err": err.Error(),
			"note": "同步时间戳没能回写，界面会显示「从未同步」而实际数据是新的",
		})
	}
}

// ---- 只读查询 ----

func (h *CDNHandler) ListZones(c *gin.Context) {
	rows, err := h.DB.Query(`SELECT z.id, z.account_id, a.name, z.zone_id, z.name, z.status, z.paused, z.plan,
		z.name_servers, z.synced_at,
		(SELECT COUNT(*) FROM cdn_dns_records r WHERE r.account_id=z.account_id AND r.zone_id=z.zone_id) AS dns_count,
		-- ⚠️ COALESCE 必须包在**子查询外面**。写成 (SELECT COALESCE(s.value,'') ...) 时，
		-- 没有匹配行的话整个标量子查询返回 NULL，COALESCE 根本没机会执行 ——
		-- NULL 扫进 string 会让 rows.Scan 报错，而下面那个 continue 会把整行静默丢掉。
		-- 实测 4 个站点只返回 1 个（只有那个配过 ssl 设置的），且不报任何错。
		COALESCE((SELECT s.value FROM cdn_zone_settings s WHERE s.account_id=z.account_id AND s.zone_id=z.zone_id AND s.name='ssl'), '') AS ssl_mode
		FROM cdn_zones z LEFT JOIN cdn_accounts a ON a.id=z.account_id ORDER BY z.name`)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	out := []gin.H{}
	scanErrs := 0
	for rows.Next() {
		var id, aid, paused, dnsCount int
		var acc, zid, name, status, plan, ns, ssl string
		var synced time.Time
		if err := rows.Scan(&id, &aid, &acc, &zid, &name, &status, &paused, &plan, &ns, &synced, &dnsCount, &ssl); err != nil {
			// ⚠️ 扫描失败是**故障**，不是"这一行不存在"。原来这里直接 continue，
			// 于是坏一行就少一行、且界面上完全看不出来。现在记账并在响应里报出去。
			scanErrs++
			logx.J("cdn", "zone_scan_failed", map[string]any{"err": err.Error()})
			continue
		}
		// ⚠️ name_servers 库里是逗号分隔字符串（入库 strings.Join 过），接口给数组。
		// 直出字符串会让前端的 `(c.name_servers ?? []).join(' · ')` 整页崩溃（OPSCMDB-012）。
		item := gin.H{"id": id, "account": acc, "zone_id": zid, "name": name, "status": status,
			"paused": paused == 1, "plan": plan, "name_servers": splitCSV(ns), "dns_count": dnsCount,
			"ssl_mode": ssl, "synced_at": synced.Format("2006-01-02 15:04:05")}
		// ssl=flexible 表示 CF 到源站是明文，用户看到的是小锁但回源没有加密
		// 风险可以同时成立多条。原来是后一个 if 直接覆盖前一个 ——
		// 一个既 paused 又 flexible 的站点只会显示其中一条
		// 🔴 风险条目要发**结构化**的（key + 参数），不能只发拼好的中文句子。
		//	英文界面直接渲染那句中文，实测在 CDN 站点页看得到（OPSCMDB-054）。
		//	中文原句保留给 MCP / 直接调 API 的人 —— 他们读不到语言包。
		risks := []string{}
		riskKeys := []gin.H{}
		if strings.EqualFold(ssl, "flexible") {
			risks = append(risks, "SSL 模式为 flexible：CDN 到源站是明文传输，浏览器却显示已加密，建议改为 full/strict")
			riskKeys = append(riskKeys, gin.H{"key": "cdn:risk.sslFlexible"})
		}
		if status != "active" {
			risks = append(risks, "Zone 状态为 "+status+"（非 active），配置可能未生效")
			riskKeys = append(riskKeys, gin.H{"key": "cdn:risk.zoneNotActive", "params": gin.H{"status": status}})
		}
		if len(risks) > 0 {
			item["risk"] = strings.Join(risks, "；")
			item["risks"] = risks
			item["risk_keys"] = riskKeys
		}
		out = append(out, item)
	}
	if scanErrs > 0 {
		// 少了几行必须说出来，否则"库里有 4 个、界面显示 1 个"没人会发现
		c.JSON(http.StatusOK, gin.H{"items": out, "scan_errors": scanErrs,
			"warning_key":    "cdn:zonesPartiallyFailed",
			"warning_params": map[string]any{"count": scanErrs},
			"warning":        fmt.Sprintf("有 %d 个站点读取失败，未包含在列表里（不是没有站点）", scanErrs)})
		return
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

// ruleRow 把一条规则拍平成 cdn_rules 的一行。synced_at 用 NOW() 不方便走 txInsert，
// 这里显式传当前时间，保持与其它表「同一次同步同一时间戳」的一致性。
func ruleRow(accountID int, zoneID, zoneName string, r cdnsource.Rule) []any {
	return []any{accountID, zoneID, zoneName, r.Source, r.RuleID, r.Name, r.Phase, r.Kind,
		r.Priority, r.Status, r.Expression, r.Actions, r.LastUpdated, time.Now()}
}

// parseCFTime 解析 Cloudflare 的 RFC3339 时间，解析不了返回 nil（存 NULL）。
// 不返回零值时间——那会变成 0000-00-00，被读成「已过期很久」，是个会误导人的默认值。
func parseCFTime(s string) any {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		logx.J("cdn", "cert_time_parse_fail", map[string]any{
			"value": s, "warn": "无法解析 Cloudflare 返回的证书到期时间，该证书到期时间存为空",
		})
		return nil
	}
	return t
}

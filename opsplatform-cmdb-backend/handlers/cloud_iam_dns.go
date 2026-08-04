package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"opsplatform-cmdb-backend/cloudsource"
)

// GCP IAM 权限审计 + Cloud DNS 台账（只读）。
//
// ⚠️ 采集侧（cloudsource/gcp_iam_dns.go）未经真实凭证验证，本地 cloud_accounts
// 没有凭据。这里的查询接口在没有数据时会明确区分「确实没有」和「压根没采到」，
// 不让空结果被读成「没有风险」。

// SyncProjectIAMDNS 把一个 project 的 IAM 绑定与 Cloud DNS 刷进库（按 account+project 删旧插新）。
func SyncProjectIAMDNS(db *sql.DB, accountID int, project string, bindings []cloudsource.IAMBinding, zones []cloudsource.DNSZone) {
	logExec(db, "IAM同步写", `DELETE FROM cloud_iam_bindings WHERE cloud_account_id=? AND project=?`, accountID, project)
	for _, b := range bindings {
		logExec(db, "IAM同步写", `INSERT INTO cloud_iam_bindings
			(cloud_account_id,project,role,member_type,member,severity,issue,synced_at) VALUES (?,?,?,?,?,?,?,NOW())`,
			accountID, project, b.Role, b.MemberType, b.Member, b.Severity, b.Issue)
	}

	logExec(db, "DNS同步写", `DELETE FROM cloud_dns_records WHERE cloud_account_id=? AND project=?`, accountID, project)
	logExec(db, "DNS同步写", `DELETE FROM cloud_dns_zones WHERE cloud_account_id=? AND project=?`, accountID, project)
	for _, z := range zones {
		logExec(db, "DNS同步写", `INSERT INTO cloud_dns_zones
			(cloud_account_id,project,zone_name,dns_name,visibility,name_servers,record_count,synced_at) VALUES (?,?,?,?,?,?,?,NOW())`,
			accountID, project, z.Name, z.DNSName, z.Visibility, strings.Join(z.NameServers, ","), len(z.Records))
		for _, r := range z.Records {
			logExec(db, "DNS同步写", `INSERT INTO cloud_dns_records
				(cloud_account_id,project,zone_name,name,type,ttl,rrdatas,synced_at) VALUES (?,?,?,?,?,?,?,NOW())`,
				accountID, project, z.Name, r.Name, r.Type, r.TTL, strings.Join(r.RRDatas, ","))
		}
	}
}

func (h *NetworkHandler) RegisterIAMDNS(r *gin.RouterGroup) {
	r.GET("/cloud-iam", h.ListIAM)          // project?, only=issues?
	r.GET("/cloud-dns", h.ListCloudDNS)     // project?, q?
	r.GET("/dns-consistency", h.DNSCompare) // GCP Cloud DNS vs Cloudflare
}

// ListIAM 项目权限审计：谁对项目有什么权限，哪些是过宽的。
func (h *NetworkHandler) ListIAM(c *gin.Context) {
	q := `SELECT project,role,member_type,member,severity,issue,synced_at FROM cloud_iam_bindings WHERE 1=1`
	args := []any{}
	if p := c.Query("project"); p != "" {
		q += " AND project=?"
		args = append(args, p)
	}
	if c.Query("only") == "issues" {
		q += " AND severity<>''"
	}
	q += " ORDER BY FIELD(severity,'critical','high','medium','') , project, role"
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	items := []gin.H{}
	sum := map[string]int{}
	for rows.Next() {
		var proj, role, mt, member, sev, issue, synced string
		if rows.Scan(&proj, &role, &mt, &member, &sev, &issue, &synced) != nil {
			continue
		}
		it := gin.H{"project": proj, "role": role, "member_type": mt, "member": member, "synced_at": synced}
		if sev != "" {
			it["severity"], it["issue"] = sev, issue
			sum[sev]++
		}
		items = append(items, it)
	}
	out := gin.H{"total": len(items), "summary": sum, "items": items}
	if len(items) == 0 {
		// 「查不到」和「没有风险」必须分开——IAM 一条都没有是不可能的，
		// 只可能是没采到（无凭据 / 权限不足 / API 未启用）。
		out["empty_hint"] = "没有任何 IAM 数据。任何 GCP 项目都至少有一条权限绑定，" +
			"所以这说明「尚未采集成功」，而不是「没有风险」。请检查云账号凭据是否已配置，" +
			"以及服务账号是否有 roles/iam.securityReviewer（或 roles/viewer）；采集日志见 [gcp-iam] 开头的行"
	}
	c.JSON(http.StatusOK, out)
}

// ListCloudDNS GCP Cloud DNS 台账。
func (h *NetworkHandler) ListCloudDNS(c *gin.Context) {
	zq := `SELECT project,zone_name,dns_name,visibility,name_servers,record_count,synced_at FROM cloud_dns_zones WHERE 1=1`
	args := []any{}
	if p := c.Query("project"); p != "" {
		zq += " AND project=?"
		args = append(args, p)
	}
	zrows, err := h.DB.Query(zq+" ORDER BY project, dns_name", args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer zrows.Close()
	zones := []gin.H{}
	for zrows.Next() {
		var proj, zn, dn, vis, ns, synced string
		var cnt int
		if zrows.Scan(&proj, &zn, &dn, &vis, &ns, &cnt, &synced) != nil {
			continue
		}
		zones = append(zones, gin.H{"project": proj, "zone_name": zn, "dns_name": dn,
			"visibility": vis, "name_servers": ns, "record_count": cnt, "synced_at": synced})
	}

	rq := `SELECT project,zone_name,name,type,ttl,rrdatas FROM cloud_dns_records WHERE 1=1`
	rargs := []any{}
	if p := c.Query("project"); p != "" {
		rq += " AND project=?"
		rargs = append(rargs, p)
	}
	if kw := strings.TrimSpace(c.Query("q")); kw != "" {
		rq += " AND (name LIKE ? OR rrdatas LIKE ?)"
		rargs = append(rargs, "%"+kw+"%", "%"+kw+"%")
	}
	rrows, err := h.DB.Query(rq+" ORDER BY name LIMIT 500", rargs...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rrows.Close()
	records := []gin.H{}
	for rrows.Next() {
		var proj, zn, name, typ, rr string
		var ttl int
		if rrows.Scan(&proj, &zn, &name, &typ, &ttl, &rr) != nil {
			continue
		}
		records = append(records, gin.H{"project": proj, "zone": zn, "name": name,
			"type": typ, "ttl": ttl, "targets": rr})
	}

	out := gin.H{"zones": zones, "records": records, "record_count": len(records)}
	if len(zones) == 0 {
		// 这里的空是可能合法的——解析可能全在 Cloudflare。两种可能都要说，不能只说一种。
		out["empty_hint"] = "没有 Cloud DNS 托管区。可能是：(1) 域名解析全部托管在 Cloudflare 等外部服务；" +
			"(2) 尚未采集成功（无凭据 / 缺 roles/dns.reader / 项目未启用 Cloud DNS API）。" +
			"区分方法：查采集日志里 [gcp-dns] 开头的行"
	}
	c.JSON(http.StatusOK, out)
}

// DNSCompare 把 GCP Cloud DNS 与 Cloudflare 的解析放在一起比。
//
// 两边并存时最常见的坑是「改了没生效」——在一边改了，实际生效的是另一边。
// 判断哪边生效要看域名的 NS 指向谁，这里不下断言，只把冲突摆出来。
func (h *NetworkHandler) DNSCompare(c *gin.Context) {
	gcpMap := map[string][]dnsRec{}
	rows, err := h.DB.Query(`SELECT name,type,rrdatas FROM cloud_dns_records`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var n, t, rr string
			if rows.Scan(&n, &t, &rr) == nil {
				gcpMap[normalizeFQDN(n)] = append(gcpMap[normalizeFQDN(n)], dnsRec{t, rr})
			}
		}
	}
	cfMap := map[string][]dnsRec{}
	crows, err := h.DB.Query(`SELECT name,type,content FROM cdn_dns_records`)
	if err == nil {
		defer crows.Close()
		for crows.Next() {
			var n, t, ct string
			if crows.Scan(&n, &t, &ct) == nil {
				cfMap[normalizeFQDN(n)] = append(cfMap[normalizeFQDN(n)], dnsRec{t, ct})
			}
		}
	}

	conflicts := []gin.H{}
	for fqdn, gs := range gcpMap {
		cs, ok := cfMap[fqdn]
		if !ok {
			continue
		}
		gt := targetsOf(gs)
		ct := targetsOf(cs)
		if gt == ct {
			continue
		}
		conflicts = append(conflicts, gin.H{
			"fqdn": fqdn, "gcp": gt, "cloudflare": ct,
			"issue": "同一域名在 GCP Cloud DNS 与 Cloudflare 都有解析，且目标不一致",
			"action": "确认该域名的 NS 实际指向哪一方——只有 NS 指向的那一边才生效，" +
				"另一边的记录是无效配置，改了不会有任何效果",
		})
	}
	sort.Slice(conflicts, func(i, j int) bool {
		return conflicts[i]["fqdn"].(string) < conflicts[j]["fqdn"].(string)
	})

	out := gin.H{
		"gcp_fqdn_count":        len(gcpMap),
		"cloudflare_fqdn_count": len(cfMap),
		"conflicts":             conflicts,
		"conflict_count":        len(conflicts),
	}
	if len(gcpMap) == 0 || len(cfMap) == 0 {
		// 有一边为空时，"0 冲突"这个结论没有意义，必须说清楚。
		out["not_comparable"] = "有一方没有数据（GCP " + strconv.Itoa(len(gcpMap)) +
			" 条 / Cloudflare " + strconv.Itoa(len(cfMap)) + " 条），本次比对不成立——" +
			"「0 个冲突」在这种情况下不代表两边一致"
	}
	c.JSON(http.StatusOK, out)
}

// dnsRec 比对用的一条解析（类型+目标）。
type dnsRec struct{ typ, target string }

// normalizeFQDN 去掉尾点并转小写，让 GCP（带尾点）与 Cloudflare（不带）能对上。
func normalizeFQDN(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}

func targetsOf(rs []dnsRec) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		parts = append(parts, r.typ+":"+r.target)
	}
	sort.Strings(parts)
	return strings.Join(parts, " | ")
}

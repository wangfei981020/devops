package handlers

import (
	"database/sql"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/cloudsource"
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
		// 同上：库里逗号分隔，接口给数组。前端现在没用到这个字段，
		// 但留着字符串就是给下一个用它的人埋一个 .join 崩溃。
		zones = append(zones, gin.H{"project": proj, "zone_name": zn, "dns_name": dn,
			"visibility": vis, "name_servers": splitCSV(ns), "record_count": cnt, "synced_at": synced})
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
		// ⚠️ rrdatas 库里是逗号分隔字符串（入库 strings.Join 过），接口给数组。
		// 同 OPSCMDB-012：直出字符串会让前端 .join 崩溃。
		records = append(records, gin.H{"project": proj, "zone": zn, "name": name,
			"type": typ, "ttl": ttl, "targets": splitCSV(rr)})
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

// DNSCompare 把三方（GoDaddy / Cloudflare / GCP Cloud DNS）的解析放在一起比，
// 并按 NS 判定**哪一方才是生效的那一方**。
//
// # 原来漏了什么
//
// 比对双方写死成 GCP 与 Cloudflare，GoDaddy 全程不参与 —— 而 62 个域名
// 全都注册在 GoDaddy，解析记录也一直在采（写进 dns_records），
// 只是从没进过这个接口（OPSCMDB-031 P0-10）。
//
// 更要命的是：工具描述里自己写着「只有 NS 指向的那边才生效，另一边改了没用」，
// 而实现从来没照这句做过。实测 g32cf.com 在 CF 有 36 条记录、NS 却指向 GoDaddy ——
// 那些记录很可能根本不生效，页面上却和生效的记录长得一模一样。
//
// ⚠️ 三态：NS 没采到时**不下断言**（unknown），绝不能把"没采到"渲染成"配错了"。
func (h *NetworkHandler) DNSCompare(c *gin.Context) {
	auth := loadDNSAuthority(h.DB)

	load := func(query string, splitTargets bool) map[string][]dnsRec {
		m := map[string][]dnsRec{}
		rows, err := h.DB.Query(query)
		if err != nil {
			return m
		}
		defer rows.Close()
		for rows.Next() {
			var n, t, v string
			if rows.Scan(&n, &t, &v) != nil {
				continue
			}
			k := normalizeFQDN(n)
			if splitTargets {
				for _, one := range splitCSV(v) {
					m[k] = append(m[k], dnsRec{t, one})
				}
				continue
			}
			m[k] = append(m[k], dnsRec{t, v})
		}
		return m
	}

	gcpMap := load(`SELECT name,type,rrdatas FROM cloud_dns_records`, true)
	cfMap := load(`SELECT name,type,content FROM cdn_dns_records`, false)
	// 🔴 GoDaddy 这一路是本次补上的。它的 name 是**相对主机名**（@ / www），
	//	要拼上主域名才能和另外两方对齐 —— 不拼的话三方永远比不到一起，
	//	而接口会安静地返回「0 冲突」
	gdMap := loadGoDaddyRecords(h.DB)

	sides := []struct {
		provider DNSProvider
		key      string
		m        map[string][]dnsRec
	}{
		{ProviderGCP, "gcp", gcpMap},
		{ProviderCloudflare, "cloudflare", cfMap},
		{ProviderGoDaddy, "godaddy", gdMap},
	}

	conflicts := []gin.H{}
	oneSided := []gin.H{}
	ineffective := []gin.H{}

	// 两两比对。三方任取两方，共三对
	for i := 0; i < len(sides); i++ {
		for j := i + 1; j < len(sides); j++ {
			a, b := sides[i], sides[j]
			for fqdn, as := range a.m {
				bs, ok := b.m[fqdn]
				if !ok {
					continue
				}
				// ⚠️ **必须按记录类型分组比**。原来是把一个 fqdn 下所有类型的记录
				// 拼成一个字符串整体比较，于是「两边 A 记录完全相同、只是 Cloudflare 多一条 MX」
				// 会被判成"目标不一致"。而真实环境里 Cloudflare 侧普遍配着 MX/TXT/SPF、
				// GCP 侧只有 A/CNAME —— 几乎每个两边都配的域名都会误报。
				// 误报多了，真冲突就没人看了（与暴露面那一节同一个道理）。
				aByType, bByType := byType(as), byType(bs)
				for typ, avals := range aByType {
					bvals, both := bByType[typ]
					if !both {
						// 只有一边配了这个类型 —— 这不是"目标不一致"，多数情况完全正常
						oneSided = append(oneSided, gin.H{
							"fqdn": fqdn, "type": typ, "only_in": a.key, "targets": avals,
							// issue 保留给 MCP / 直接调 API 的人；issue_key 给界面翻译
							"issue":        "仅 " + providerLabel(a.provider) + " 侧配置了该类型记录",
							"issue_key":    "dns:consistency.oneSidedIssue",
							"issue_params": gin.H{"provider": providerLabel(a.provider)},
						})
						continue
					}
					if sameTargets(avals, bvals) {
						continue
					}
					conflicts = append(conflicts, gin.H{
						"fqdn": fqdn, "type": typ,
						"sides":         gin.H{a.key: strings.Join(avals, ", "), b.key: strings.Join(bvals, ", ")},
						"issue":         "同一域名的同一类型记录在 " + providerLabel(a.provider) + " 与 " + providerLabel(b.provider) + " 上目标不一致",
						"issue_key":     "dns:consistency.conflictIssue",
						"issue_params":  gin.H{"a": providerLabel(a.provider), "b": providerLabel(b.provider)},
						"action":        conflictAction(auth, fqdn),
						"action_key":    conflictActionKey(auth, fqdn),
						"action_params": conflictActionParams(auth, fqdn),
					})
				}
				for typ, bvals := range bByType {
					if _, both := aByType[typ]; !both {
						oneSided = append(oneSided, gin.H{
							"fqdn": fqdn, "type": typ, "only_in": b.key, "targets": bvals,
							"issue":        "仅 " + providerLabel(b.provider) + " 侧配置了该类型记录",
							"issue_key":    "dns:consistency.oneSidedIssue",
							"issue_params": gin.H{"provider": providerLabel(b.provider)},
						})
					}
				}
			}
		}
	}

	// 🔴 「配了但不生效」——本条是 P0-10 的核心。
	//
	//	NS 指向的不是这一方，那这一方的记录改了没有任何效果。
	//	它和"冲突"是两件事：可能只有一方配了、完全没冲突，但配在了不生效的那一方
	for _, side := range sides {
		for fqdn := range side.m {
			a := authorityOf(auth, fqdn)
			if a == nil || a.Provider == ProviderUnknown {
				// 没采到 NS / NS 指向多方 —— 不下断言
				continue
			}
			if a.Provider == side.provider {
				continue
			}
			ineffective = append(ineffective, gin.H{
				"fqdn": fqdn, "configured_in": side.key,
				"authoritative": string(a.Provider), "ns": a.NSHosts,
				"issue": "这些记录配在 " + providerLabel(side.provider) + "，但该域名的 NS 指向 " +
					providerLabel(a.Provider) + "——只有 NS 指向的那一方才生效",
				"issue_key": "dns:consistency.ineffectiveIssue",
				"action": "要么把记录改到 " + providerLabel(a.Provider) + " 上，" +
					"要么把 NS 切到 " + providerLabel(side.provider) + "。在这一方继续改不会有任何效果",
				"action_key": "dns:consistency.ineffectiveAction",
				// 两条文案共用同一组参数
				"issue_params": gin.H{
					"configured":    providerLabel(side.provider),
					"authoritative": providerLabel(a.Provider),
				},
				"action_params": gin.H{
					"configured":    providerLabel(side.provider),
					"authoritative": providerLabel(a.Provider),
				},
			})
		}
	}

	sortByFQDN(conflicts)
	sortByFQDN(oneSided)
	sortByFQDN(ineffective)

	// NS 指向多方的域名单列：谁生效取决于递归解析器问到了哪台，本身就是要修的问题
	splitNS := []gin.H{}
	for d, a := range auth {
		if a.Split {
			splitNS = append(splitNS, gin.H{"domain": d, "ns": a.NSHosts,
				"issue":     "NS 同时指向多方，解析结果取决于递归解析器问到了哪一台——这本身是个要修的配置",
				"issue_key": "dns:consistency.splitNsIssue"})
		}
	}

	// 🔴 判不出托管方的域名数，必须从**有记录的那批**里数，不能从 auth 里数。
	//
	//	auth 是由 NS 记录建出来的 —— 一条 NS 都没采到的域名**根本不在 auth 里**，
	//	按 auth 数出来永远是 0。本地实测就是这样：三方一条 NS 都没有，
	//	而接口返回 `unknown_ns_domains: 0`，看起来像"每个域名都判出来了"。
	//
	//	这正是本轮反复在修的那个形态：**一个 0 看起来完全正常，实际什么都没算**。
	//	（我自己写的这段当场犯了一次，被本地实测的返回值抓住。）
	unknownDomains := map[string]bool{}
	for _, side := range sides {
		for fqdn := range side.m {
			if a := authorityOf(auth, fqdn); a == nil || a.Provider == ProviderUnknown {
				unknownDomains[apexOf(fqdn)] = true
			}
		}
	}
	unknownNS := len(unknownDomains)
	sort.Slice(splitNS, func(i, j int) bool { return splitNS[i]["domain"].(string) < splitNS[j]["domain"].(string) })

	out := gin.H{
		"gcp_fqdn_count":        len(gcpMap),
		"cloudflare_fqdn_count": len(cfMap),
		"godaddy_fqdn_count":    len(gdMap),
		"conflicts":             conflicts,
		"conflict_count":        len(conflicts),
		// 「只有一边配了」单独一档：它不是冲突，但也不是完全无信息 ——
		// 混进 conflicts 会稀释真冲突，完全不给又会让人以为两边记录集合一致
		"one_sided":       oneSided,
		"one_sided_count": len(oneSided),
		// 「配了但不生效」：按 NS 判出来的，是 P0-10 要答的那个问题
		"ineffective":       ineffective,
		"ineffective_count": len(ineffective),
		"split_ns":          splitNS,
		// authority 每个域名的托管方，给前端逐行标「这条生不生效」用。
		//
		//	⚠️ 不给这张表的话，前端只能拿 ineffective 清单去反查，
		//	而"不在 ineffective 里"有两种含义（生效 / 判不出），
		//	反查必然把后者也渲染成生效 —— 又一次把"不知道"变成"没问题"。
		"authority": authorityOut(auth),
		// 有多少域名判不出托管方。>0 时上面的 ineffective 一定是**不完整**的
		"unknown_ns_domains": unknownNS,
	}

	// 有一方为空时，"0 冲突"这个结论没有意义，必须说清楚是哪一方空。
	//
	//	⚠️ 原来只报 GCP 和 Cloudflare 两个数 —— 而真正缺席的是第三方。
	//	"一个诚实的局部结论，可能建立在不完整的全局之上"（031 自己的总结）
	empty := []string{}
	for _, s := range sides {
		if len(s.m) == 0 {
			empty = append(empty, providerLabel(s.provider))
		}
	}
	if len(empty) > 0 {
		// ⚠️ 必须写明这几个数是**域名数**（准确说是 FQDN 数），不是记录条数。
		//	列表底下报的是记录条数，两个数字天然对不上（实测横幅 GCP 0 / CF 36，
		//	列表共 45 条），读的人会去算这笔账、算不平就怀疑数据有错
		//	（OPSCMDB-031 P2-11）。口径写在字面上最省事。
		out["not_comparable"] = "这几方没有数据：" + strings.Join(empty, "、") +
			"（按 FQDN 计：GCP " + strconv.Itoa(len(gcpMap)) + " / Cloudflare " + strconv.Itoa(len(cfMap)) +
			" / GoDaddy " + strconv.Itoa(len(gdMap)) + "，同一个 FQDN 下的多条记录只算一个，" +
			"所以这几个数不会等于列表里的记录条数）。" +
			"缺席的一方不参与比对，「0 个冲突」在这种情况下不代表各方一致"
	}
	if unknownNS > 0 {
		out["authority_incomplete"] = strconv.Itoa(unknownNS) +
			" 个域名没采到 NS 记录（或 NS 指向多方），判不出托管方。这些域名不参与「配了但不生效」的判定——" +
			"也就是说这份清单是不完整的，不能反过来当成「其余的都生效」。" +
			"补齐办法：同步一次 DNS（NS 记录会随解析一起采回来）"
	}
	c.JSON(http.StatusOK, out)
}

func sortByFQDN(list []gin.H) {
	sort.Slice(list, func(i, j int) bool {
		a, _ := list[i]["fqdn"].(string)
		b, _ := list[j]["fqdn"].(string)
		return a < b
	})
}

// conflictAction 冲突时的下一步。能判出托管方就直接说是哪一方生效。
// conflictActionKey / conflictActionParams 是 conflictAction 的可翻译版本。
//
// ⚠️ 两个分支的**语气完全不同**，不能合成一条：
//
//	判得出方向时说的是结论（"NS 指向 X，只有那一方生效"）；
//	判不出时说的是"去确认"，并且必须交代**为什么判不出**（没采到 NS）。
//	合成一条就会在判不出的时候编一个方向出来。
func conflictActionKey(auth map[string]*DNSAuthority, fqdn string) string {
	a := authorityOf(auth, fqdn)
	if a == nil || a.Provider == ProviderUnknown {
		return "dns:consistency.conflictActionUnknown"
	}
	return "dns:consistency.conflictActionKnown"
}

func conflictActionParams(auth map[string]*DNSAuthority, fqdn string) gin.H {
	a := authorityOf(auth, fqdn)
	if a == nil || a.Provider == ProviderUnknown {
		return nil
	}
	return gin.H{"authoritative": providerLabel(a.Provider), "ns": strings.Join(a.NSHosts, ", ")}
}

func conflictAction(auth map[string]*DNSAuthority, fqdn string) string {
	a := authorityOf(auth, fqdn)
	if a == nil || a.Provider == ProviderUnknown {
		// ⚠️ 判不出来时说"去确认"，不要编一个方向出来
		return "确认该域名的 NS 实际指向哪一方——只有 NS 指向的那一边才生效，" +
			"另一边的记录是无效配置，改了不会有任何效果。（我们没采到这个域名的 NS 记录，所以判不出是哪一方）"
	}
	return "该域名的 NS 指向 " + providerLabel(a.Provider) + "（" + strings.Join(a.NSHosts, ", ") +
		"），只有那一方的记录生效，其余各方改了不会有任何效果"
}

// loadGoDaddyRecords 读 GoDaddy 侧的解析记录，并把相对主机名补成 FQDN。
//
//	⚠️ GoDaddy 的 name 是相对的：`@` 表示主域名本身，`www` 表示 www.主域名。
//	不补全的话它永远和另外两方的 FQDN 对不上，接口会安静地返回「0 冲突」——
//	而"安静地什么都没比到"正是 P0-10 的形态。
func loadGoDaddyRecords(db *sql.DB) map[string][]dnsRec {
	m := map[string][]dnsRec{}
	rows, err := db.Query(`SELECT c.name, r.type, r.name, r.data
	                         FROM dns_records r JOIN cis c ON c.id = r.domain_ci_id
	                        WHERE c.type='domain'`)
	if err != nil {
		return m
	}
	defer rows.Close()
	for rows.Next() {
		var domain, typ, host, data string
		if rows.Scan(&domain, &typ, &host, &data) != nil {
			continue
		}
		m[normalizeFQDN(recordFQDN(host, domain))] = append(m[normalizeFQDN(recordFQDN(host, domain))], dnsRec{typ, data})
	}
	return m
}

type dnsRec struct{ typ, target string }

// normalizeFQDN 去掉尾点并转小写，让 GCP（带尾点）与 Cloudflare（不带）能对上。
func normalizeFQDN(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "."))
}

// byType 按记录类型分组。比对必须在**同类型之间**进行，见 DNSCompare 的注释。
func byType(rs []dnsRec) map[string][]string {
	m := map[string][]string{}
	for _, r := range rs {
		m[strings.ToUpper(r.typ)] = append(m[strings.ToUpper(r.typ)], r.target)
	}
	for k := range m {
		sort.Strings(m[k]) // 多值记录两边顺序不同不算冲突
	}
	return m
}

// sameTargets 同类型下目标集合是否一致（已排序，故可逐个比）。
func sameTargets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		// 末尾点是 DNS 的合法写法差异（example.com. 与 example.com 等价），不算冲突
		if !strings.EqualFold(strings.TrimSuffix(a[i], "."), strings.TrimSuffix(b[i], ".")) {
			return false
		}
	}
	return true
}

func targetsOf(rs []dnsRec) string {
	parts := make([]string, 0, len(rs))
	for _, r := range rs {
		parts = append(parts, r.typ+":"+r.target)
	}
	sort.Strings(parts)
	return strings.Join(parts, " | ")
}

// authorityOut 把托管权判定摊成给前端用的形状。
//
//	只输出**判得出**的域名。判不出的不出现在这张表里 ——
//	前端查不到就是 unknown，这样"没数据"和"判不出"天然是同一件事，
//	不需要第二个字段去区分（它们本来就是同一件事）。
func authorityOut(auth map[string]*DNSAuthority) map[string]gin.H {
	out := map[string]gin.H{}
	for d, a := range auth {
		if a.Provider == ProviderUnknown {
			continue
		}
		out[d] = gin.H{"provider": string(a.Provider), "label": providerLabel(a.Provider), "ns": a.NSHosts}
	}
	return out
}

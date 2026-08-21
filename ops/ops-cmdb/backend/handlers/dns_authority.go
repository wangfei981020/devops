package handlers

import (
	"database/sql"
	"strings"
)

// DNS 托管权判定：一个域名的解析**实际由谁提供**。
//
// # 为什么必须有这个
//
// 我们同时从三方采集解析记录（GoDaddy / Cloudflare / GCP Cloud DNS），
// 而一个域名**只有 NS 指向的那一方生效**，另一方配了也白配。
//
// 实测（OPSCMDB-031 P0-10）：`g32cf.com` 在 Cloudflare 里有 zone、
// 36 条记录里就有它的 A/CNAME/TXT，但它的 NS 是
// `ns57.domaincontrol.com` / `ns58.domaincontrol.com` —— **GoDaddy 的 NS**。
// 也就是说 CF 上那些记录很可能根本没生效，而 CMDB 把它们和生效的记录
// 一样展示，没有任何提示。
//
// 排查「改了解析没生效」时这会把人直接带进沟里：
// CF 上看到、CMDB 上也看到，于是认定配置没问题 ——
// 而真正生效的那份在另一边。
//
// ⚠️ 这个判据一直**写在注释和 MCP 工具描述里**（"只有 NS 指向的那边才生效"），
// 只是实现从来没照做。本轮反复出现的同一个模式。

// DNSProvider 解析托管方。
type DNSProvider string

const (
	ProviderGoDaddy    DNSProvider = "godaddy"
	ProviderCloudflare DNSProvider = "cloudflare"
	ProviderGCP        DNSProvider = "gcp"
	ProviderOther      DNSProvider = "other"
	// ProviderUnknown 没采到这个域名的 NS 记录。
	//
	//	🔴 必须和 other 分开。unknown 表示**我们不知道**，
	//	这时候绝不能断言任何一方的记录"不生效" ——
	//	那是把"没采到"渲染成"配错了"，比不提示更糟。
	ProviderUnknown DNSProvider = ""
)

// nsSuffixToProvider NS 主机名的后缀 → 托管方。
//
//	⚠️ 按后缀匹配而不是完整主机名：各家的 NS 是带序号的
//	（ns57.domaincontrol.com、cody.ns.cloudflare.com、ns-cloud-a1.googledomains.com），
//	写死完整名字必然漏。
var nsSuffixToProvider = []struct {
	suffix   string
	provider DNSProvider
}{
	{"domaincontrol.com", ProviderGoDaddy},
	{"ns.cloudflare.com", ProviderCloudflare},
	{"googledomains.com", ProviderGCP},
	{"google.com", ProviderGCP},
}

// ProviderOfNS 从一条 NS 主机名判断它属于哪一方。
func ProviderOfNS(host string) DNSProvider {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if h == "" {
		return ProviderUnknown
	}
	for _, m := range nsSuffixToProvider {
		if h == m.suffix || strings.HasSuffix(h, "."+m.suffix) {
			return m.provider
		}
	}
	// 认不出的 NS 是**真实存在的第三方**（DNSPod、阿里、自建…），
	// 不是"没采到"。归 other，并且它同样让另外三方的记录不生效
	return ProviderOther
}

// DNSAuthority 一个域名的托管权结论。
type DNSAuthority struct {
	// Provider NS 指向的那一方。ProviderUnknown = 没采到 NS
	Provider DNSProvider
	// NSHosts 判定依据的 NS 主机名，原样带出来供人核对
	NSHosts []string
	// Split NS 指向了多方（迁移中途，或配错了）。
	//
	//	⚠️ 这种情况**不能**挑一方说了算：两边都可能被解析到，
	//	谁生效取决于递归解析器问到了哪台。这本身就是个要修的问题
	Split bool
}

// loadDNSAuthority 从已采集的 NS 记录算出每个域名的托管方。
//
//	⚠️ 用**已采集的记录**而不是现场 net.LookupNS：
//	一次请求要判 60+ 个域名，现场查会把接口拖到几十秒，
//	而且解析失败时会退化成"unknown"，让整页结论随网络抖动而变。
//
//	代价是判据可能是旧的 —— 但采集时刻是可见的（每张表都有 synced_at），
//	而"慢到超时"是不可见的。
func loadDNSAuthority(db *sql.DB) map[string]*DNSAuthority {
	out := map[string]*DNSAuthority{}
	add := func(domain, nsHost string) {
		d := normalizeFQDN(domain)
		if d == "" || strings.TrimSpace(nsHost) == "" {
			return
		}
		a := out[d]
		if a == nil {
			a = &DNSAuthority{}
			out[d] = a
		}
		h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(nsHost), "."))
		for _, existing := range a.NSHosts {
			if existing == h {
				return
			}
		}
		a.NSHosts = append(a.NSHosts, h)
	}

	// 三方采到的 NS 记录都算数：NS 记录描述的是**这个域名托管在哪**，
	// 和它是从谁那儿读到的无关。
	//
	// 🔴 但三张表的 `name` 口径**不一样**，必须分开处理：
	//
	//	cdn_dns_records / cloud_dns_records  name 是 FQDN（`example.com`）
	//	dns_records（GoDaddy）               name 是**相对主机名**（`@` / `www`）
	//
	//	把 GoDaddy 那张表的 name 直接当 key，全部 NS 记录会塌进一个叫 `@` 的
	//	假域名里 —— 生产实测：`"@"` 这一个 key 底下挂了 51 个 NS 主机名，
	//	而 59 个真实域名报「没采到 NS，判不出托管方」，它们的 NS 明明就在库里。
	//
	//	⚠️ 同一个坑我在 loadGoDaddyRecords 里补过 `recordFQDN(host, domain)`，
	//	这里漏了 —— **修了一处漏一处**。所以两处现在都走同一个补全函数。
	for _, q := range []string{
		`SELECT name, content FROM cdn_dns_records   WHERE type='NS'`,
		`SELECT name, rrdatas FROM cloud_dns_records WHERE type='NS'`,
	} {
		rows, err := db.Query(q)
		if err != nil {
			continue
		}
		for rows.Next() {
			var name, val string
			if rows.Scan(&name, &val) != nil {
				continue
			}
			// cloud_dns_records 的 rrdatas 是逗号分隔的多条
			for _, one := range strings.Split(val, ",") {
				add(name, one)
			}
		}
		rows.Close()
	}

	// GoDaddy 侧：JOIN 出主域名，把相对主机名补成 FQDN 再入表
	if rows, err := db.Query(`SELECT c.name, r.name, r.data
	                            FROM dns_records r
	                            JOIN cis c ON c.id = r.domain_ci_id AND c.type='domain'
	                           WHERE r.type='NS'`); err == nil {
		for rows.Next() {
			var domain, host, val string
			if rows.Scan(&domain, &host, &val) != nil {
				continue
			}
			// `@` → 主域名本身；`sub` → sub.主域名（子域委派，同样是真实的 NS 记录）
			add(recordFQDN(host, domain), val)
		}
		rows.Close()
	}

	for _, a := range out {
		seen := map[DNSProvider]bool{}
		for _, h := range a.NSHosts {
			seen[ProviderOfNS(h)] = true
		}
		delete(seen, ProviderUnknown)
		switch len(seen) {
		case 0:
			a.Provider = ProviderUnknown
		case 1:
			for p := range seen {
				a.Provider = p
			}
		default:
			// NS 指向多方 —— 不挑一方说了算
			a.Split = true
			a.Provider = ProviderUnknown
		}
	}
	return out
}

// authorityOf 找 fqdn 所属域名的托管方。
//
//	NS 记录挂在**主域名**上（`example.com`），而要判的是
//	`api.example.com` 这样的子域，所以要逐级往上找。
func authorityOf(auth map[string]*DNSAuthority, fqdn string) *DNSAuthority {
	f := normalizeFQDN(fqdn)
	for {
		if a, ok := auth[f]; ok {
			return a
		}
		i := strings.Index(f, ".")
		if i < 0 {
			return nil
		}
		f = f[i+1:]
		if !strings.Contains(f, ".") {
			// 走到 TLD 了，再往上没有意义
			return nil
		}
	}
}

// providerLabel 给人看的名字。
func providerLabel(p DNSProvider) string {
	switch p {
	case ProviderGoDaddy:
		return "GoDaddy"
	case ProviderCloudflare:
		return "Cloudflare"
	case ProviderGCP:
		return "GCP Cloud DNS"
	case ProviderOther:
		return "第三方（不在我们采集范围内）"
	}
	return "未知（没采到该域名的 NS 记录）"
}

// apexOf 取 FQDN 的主域名（最后两段）。
//
//	只用于**计数去重**（"有几个域名判不出托管方"），不用于判定 ——
//	多级 TLD（co.uk、com.cn）在这里会被算成两段，
//	但那只会让同一个真实域名下的子域被算成一个，方向是安全的。
func apexOf(fqdn string) string {
	f := normalizeFQDN(fqdn)
	parts := strings.Split(f, ".")
	if len(parts) <= 2 {
		return f
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

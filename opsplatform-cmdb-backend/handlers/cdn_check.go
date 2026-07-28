package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// DNS 一致性校验：把 CDN 侧的解析目标与我们实际拥有的入口地址比对。
//
// 这是接 CDN 最主要的动机。此前有一类问题只能靠人肉发现：域名还解析着，
// 但指向的 IP 早已随着服务下线被回收——证书巡检里那一大批 "i/o timeout"
// 就是这么来的，而光看超时报错根本判断不出是"入口挂了"还是"IP 已经不是我们的了"。
// 两边一比对，结论立刻清楚。

type dnsCheckItem struct {
	Zone     string `json:"zone"`
	FQDN     string `json:"fqdn"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	ViaCDN   bool   `json:"via_cdn"`
	Severity string `json:"severity"`         // high/medium/low
	Issue    string `json:"issue,omitempty"`  // 判定出的问题
	Action   string `json:"action,omitempty"` // 建议动作
}

// DomainCheck GET /api/cdn/domain-check?zone=&only=issues
func (h *CDNHandler) DomainCheck(c *gin.Context) {
	// 先确认有没有可校验的数据。没数据却返回「ok + 空清单」，会被读成「查过了，没问题」，
	// 而实际含义是「根本没查」——这两者对调用方的意义完全相反。
	var recCount int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM cdn_dns_records`).Scan(&recCount)
	if recCount == 0 {
		var accCount int
		_ = h.DB.QueryRow(`SELECT COUNT(*) FROM cdn_accounts WHERE enabled=1`).Scan(&accCount)
		msg := "CMDB 里还没有任何 CDN 解析记录，无法校验（这不代表没有问题，而是还没有数据）。"
		if accCount == 0 {
			msg += "尚未配置任何 CDN 账号：请在 CMDB 的 CDN 账号页填入 Cloudflare 只读 API Token。"
		} else {
			msg += "已配置账号但尚未同步成功：可手动触发同步，或查看账号的 last_result 排查。"
		}
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": msg})
		return
	}

	owned := h.ownedIngressIPs()
	if len(owned) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"ok": false,
			"error": "没有采集到任何入口 IP（K8s LoadBalancer / 云 LB VIP / 静态 IP / 主机外网 IP 都为空），" +
				"无法判断解析目标是否仍属于我们；请先确认 K8s 与云资源同步正常",
		})
		return
	}
	scope := h.managedScope(len(owned))

	q := `SELECT zone_name, type, name, content, proxied FROM cdn_dns_records WHERE type IN ('A','AAAA','CNAME')`
	args := []any{}
	if z := c.Query("zone"); z != "" {
		q += " AND zone_name=?"
		args = append(args, z)
	}
	q += " ORDER BY zone_name, name"
	rows, err := h.DB.Query(q, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	items := []dnsCheckItem{}
	for rows.Next() {
		var zone, typ, name, content string
		var proxied int
		if rows.Scan(&zone, &typ, &name, &content, &proxied) != nil {
			continue
		}
		it := dnsCheckItem{Zone: zone, FQDN: name, Type: typ, Content: content,
			ViaCDN: proxied == 1, Severity: "low"}

		switch typ {
		case "A", "AAAA":
			switch {
			case owned[content]:
				if proxied == 0 {
					it.Severity = "medium"
					it.Issue = "未经 CDN 代理（灰云），源站 IP " + content + " 直接暴露在公网解析中"
					it.Action = "确认是否有意为之；需要隐藏源站就在 Cloudflare 把该记录切成橙云"
				}
			case wellKnownThirdParty(content) != "":
				// 公共 DNS、云厂商公告地址段这类本来就不该是自建入口，报出来只会制造噪声
				it.Severity = "low"
				it.Issue = "解析目标 " + content + " 属于第三方服务（" + wellKnownThirdParty(content) + "），非自建入口"
			default:
				// 不在已知入口里。但这只有在「已知入口集合是完整的」前提下才等于异常——
				// CMDB 纳管范围小于 DNS 覆盖范围时（比如 prod 没纳管），这个前提不成立。
				it.Severity, it.Issue, it.Action = scope.judge(content)
			}
		case "CNAME":
			// CNAME 指向域名而非 IP，无法直接与入口地址比对，这里只标注不判定，
			// 避免给出没有依据的结论。
			it.Issue = "CNAME 指向 " + content + "（未做可达性判定）"
		}
		items = append(items, it)
	}

	if c.Query("only") == "issues" {
		filtered := items[:0:0]
		for _, it := range items {
			if it.Severity != "low" {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}

	sum := gin.H{"total": len(items), "high": 0, "medium": 0, "low": 0, "owned_ip_count": len(owned)}
	for _, it := range items {
		sum[it.Severity] = sum[it.Severity].(int) + 1
	}
	// 判定边界要跟着结论一起给出，否则调用方无从判断该不该采信这些结论
	scopeInfo := gin.H{
		"trustworthy":       scope.trustworthy,
		"managed_clusters":  scope.clusters,
		"managed_hosts":     scope.hosts,
		"owned_ingress_ips": scope.ownedIPs,
	}
	if scope.note != "" {
		scopeInfo["warning"] = scope.note
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "summary": sum, "scope": scopeInfo, "items": items})
}

// managedScope 描述本次判定的「可信边界」。
//
// 「解析目标不在已知入口集合中」要能推出「这条解析是野的」，前提是集合完整。
// 而 CMDB 的纳管范围往往小于 DNS 的覆盖范围——域名会指向未纳管的环境（如 prod）、
// 指向第三方 SaaS、指向合作方。首次跑真实数据时就是这样：owned 只有 11 个 IP，
// 于是 20 条 high 几乎全是误报。误报比不给结论更糟：核对几条发现全是假的之后，
// 就再没人会认真看这个功能，真问题会被一起淹掉。
//
// 所以先判断集合本身可不可信，再决定结论的强度。
type managedScope struct {
	clusters    int
	hosts       int
	ownedIPs    int
	trustworthy bool
	note        string
}

func (h *CDNHandler) managedScope(ownedIPs int) managedScope {
	s := managedScope{ownedIPs: ownedIPs}
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM k8s_clusters WHERE enabled=1`).Scan(&s.clusters)
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM hosts WHERE stale=0`).Scan(&s.hosts)

	// 经验下限：每个纳管集群至少会有若干对外入口（LB/静态IP），再加上主机的外网 IP。
	// 达不到这个量级，说明要么采集不全、要么根本没纳管齐，判定就不该按「完整」处理。
	min := s.clusters * 3
	s.trustworthy = ownedIPs >= min && s.clusters > 0
	if !s.trustworthy {
		s.note = fmt.Sprintf(
			"已知入口 IP 仅 %d 个（已纳管 %d 个集群、%d 台主机），少于判定所需的量级；"+
				"未纳管环境（如生产集群）的入口天然不在集合中，因此「不在集合中」不能直接判为异常。"+
				"本次相关结论已降级为待确认。", ownedIPs, s.clusters, s.hosts)
	}
	return s
}

// judge 在当前可信边界下，给「不在已知入口集合中」的解析目标定性。
func (s managedScope) judge(ip string) (severity, issue, action string) {
	if s.trustworthy {
		return "high",
			"解析目标 " + ip + " 不在我方任何已知入口中（K8s LB / 云 LB / 静态 IP / 主机外网 IP 均无此地址）",
			"确认该 IP 是否已释放：若服务已下线请删除该解析记录；若仍在用则说明有资产未纳管，需补充采集"
	}
	// 集合不完整时只能说「查不到」，不能说「不是我方的」——这两者差别很大
	return "medium",
		"解析目标 " + ip + " 在已纳管范围内查不到对应入口（注意：纳管范围不完整，这不代表该 IP 不属于我方）",
		"先确认该 IP 属于哪个环境：若属于未纳管的环境（如生产集群），请把该环境纳管进 CMDB；" +
			"若确认已释放，再删除这条解析记录"
}

// thirdPartyRanges 明显不可能是自建入口的地址。命中这些不算异常，只做标注——
// 否则每次都会把它们混进待清理清单，逐条核对完才发现是噪声。
var thirdPartyRanges = []struct {
	prefix string
	name   string
}{
	{"8.8.8.", "Google 公共 DNS"},
	{"8.8.4.", "Google 公共 DNS"},
	{"1.1.1.", "Cloudflare 公共 DNS"},
	{"1.0.0.", "Cloudflare 公共 DNS"},
	{"223.5.5.", "阿里公共 DNS"},
	{"223.6.6.", "阿里公共 DNS"},
	{"114.114.", "114 公共 DNS"},
	{"127.", "回环地址"},
	{"0.0.0.0", "占位地址"},
}

// wellKnownThirdParty 命中返回服务名，否则返回空串。
func wellKnownThirdParty(ip string) string {
	for _, r := range thirdPartyRanges {
		if strings.HasPrefix(ip, r.prefix) {
			return r.name
		}
	}
	return ""
}

// ownedIngressIPs 汇总「我们自己的」对外入口地址：K8s LoadBalancer、云 LB VIP、
// 云静态 IP、主机外网 IP。判断解析目标是否还属于我们，就靠这个集合。
func (h *CDNHandler) ownedIngressIPs() map[string]bool {
	out := map[string]bool{}
	add := func(s string) {
		// k8s_services.external_ip 可能是逗号分隔的多个地址
		for _, v := range strings.Split(s, ",") {
			if v = strings.TrimSpace(v); v != "" {
				out[v] = true
			}
		}
	}
	queries := []string{
		`SELECT external_ip FROM k8s_services WHERE COALESCE(external_ip,'')<>''`,
		`SELECT vip FROM cloud_loadbalancers WHERE COALESCE(vip,'')<>''`,
		`SELECT address FROM cloud_addresses WHERE COALESCE(address,'')<>''`,
		`SELECT external_ip FROM hosts WHERE COALESCE(external_ip,'')<>'' AND stale=0`,
	}
	for _, q := range queries {
		rows, err := h.DB.Query(q)
		if err != nil {
			continue // 某张表还没数据不影响其余来源
		}
		for rows.Next() {
			var v string
			if rows.Scan(&v) == nil {
				add(v)
			}
		}
		rows.Close()
	}
	return out
}

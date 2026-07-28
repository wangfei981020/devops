package handlers

import (
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
			default:
				// 指向一个我们已知资产里查不到的 IP，多半是服务下线后 IP 被回收，
				// 而解析记录没跟着清理——访问会超时或落到别人的机器上。
				it.Severity = "high"
				it.Issue = "解析目标 " + content + " 不在我方任何已知入口中（K8s LB / 云 LB / 静态 IP / 主机外网 IP 均无此地址）"
				it.Action = "确认该 IP 是否已释放：若服务已下线请删除该解析记录；" +
					"若仍在用则说明有资产未纳管，需补充采集"
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
	c.JSON(http.StatusOK, gin.H{"ok": true, "summary": sum, "items": items})
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

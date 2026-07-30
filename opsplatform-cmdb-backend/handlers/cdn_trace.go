package handlers

import (
	"context"
	"database/sql"
	"net"
	"strings"
	"time"
)

// 判断一个域名是否经过 CDN —— 靠实时解析，而不是只查库。
//
// 起因：classic-baccarat-game.k8s-g32-uat.com 实际走 Cloudflare（dig 出来是
// 104.18.20.2），但全链路页 CDN 一栏是空的。原因不是没关联，是**数据压根没有**：
//
//	classic-baccarat-game.k8s-g32-uat.com  →  cf-host.k8s-g32-uat.com
//	                                       →  g32-uat-istio-gateway.uatcfzone.com  ← 只有这跳在 CF
//	                                       →  104.18.20.2
//
// 前两跳属于 k8s-g32-uat.com，而那个域在 CMDB 里 dns_provider 为空（没接 DNS 数据源），
// 所以库里既没有它的 CNAME 也没有 A 记录。只查 cdn_dns_records 必然查不到。
//
// 解法：域名解析是公开信息，不需要任何凭据——CMDB 自己解析一次就能把链走出来。
// 逐跳回查 cdn_dns_records，命中即确证；全都没命中再退到 IP 段判断。

const (
	// CNAME 逐跳查询与最终 A 记录查询各自独立计时：共用一个 3 秒预算时，
	// 三跳 CNAME 走完就没时间查 A 了，结果是「链路对了但 IP 为空」，兜底判断直接失效。
	cdnTraceTimeout   = 4 * time.Second
	cdnTraceIPTimeout = 3 * time.Second
	cdnTraceMaxHops   = 6 // 防 CNAME 环；正常链路两三跳就到底
)

// cloudflareV4 Cloudflare 公布的边缘 IPv4 段（https://www.cloudflare.com/ips-v4）。
//
// 只作兜底：命中它只能说明「流量经过 Cloudflare」，**不能说明是我方那个 CF 账号**
// ——第三方也可能把域名挂在自己的 CF 上。所以主依据始终是「CNAME 链命中
// cdn_dns_records」，那才能确证是我们纳管的账号。
var cloudflareV4 = []string{
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
}

var cloudflareNets = func() []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cloudflareV4))
	for _, c := range cloudflareV4 {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

func isCloudflareIP(ip string) bool {
	p := net.ParseIP(ip)
	if p == nil {
		return false
	}
	for _, n := range cloudflareNets {
		if n.Contains(p) {
			return true
		}
	}
	return false
}

// cdnTrace 一次实时解析的结果。
type cdnTrace struct {
	Hops     []string `json:"hops"`          // CNAME 链，含起点
	IPs      []string `json:"ips,omitempty"` // 最终解析到的地址
	ViaCDN   bool     `json:"via_cdn"`       // 是否判定为经过 CDN
	Managed  bool     `json:"managed"`       // 是否落在我方纳管的 CF 账号里
	MatchHop string   `json:"match_hop,omitempty"`
	Basis    string   `json:"basis"` // 判定依据，必须写清楚
	Err      string   `json:"error,omitempty"`
}

// traceCDN 解析域名并判断是否经过 CDN。
//
// db 用于逐跳回查 cdn_dns_records —— 判断「是不是我方纳管的那个 CF 账号」只能靠它，
// IP 段只能判断「是不是 Cloudflare」。两者结论强度不同，输出里必须区分。
func traceCDN(db *sql.DB, domain string) cdnTrace {
	t := cdnTrace{Hops: []string{domain}}
	ctx, cancel := context.WithTimeout(context.Background(), cdnTraceTimeout)
	defer cancel()
	r := &net.Resolver{}

	cur := domain
	for i := 0; i < cdnTraceMaxHops; i++ {
		// 每一跳都先回查库：命中就是确证，不必再往下走
		if zone, proxied, ok := lookupCDNRecord(db, cur); ok {
			t.Managed = true
			t.ViaCDN = proxied
			t.MatchHop = cur
			if proxied {
				t.Basis = "CNAME 链上的 " + cur + " 属于我方纳管的 CDN 站点 " + zone + "，且已开启代理（橙云）——确证经过 CDN"
			} else {
				t.Basis = "CNAME 链上的 " + cur + " 在我方 CDN 站点 " + zone +
					" 中有解析记录，但**未开启代理**（灰云）——流量直连源站，不经 CDN 防护与缓存"
			}
			ipCtx, ipCancel := context.WithTimeout(context.Background(), cdnTraceIPTimeout)
			t.IPs = lookupIPs(ipCtx, r, cur)
			ipCancel()
			return t
		}
		cname, err := r.LookupCNAME(ctx, cur)
		if err != nil {
			t.Err = "解析 CNAME 失败: " + err.Error()
			break
		}
		cname = strings.TrimSuffix(cname, ".")
		if cname == "" || cname == cur {
			break // 到底了，没有更多 CNAME
		}
		cur = cname
		t.Hops = append(t.Hops, cur)
	}

	ipCtx, ipCancel := context.WithTimeout(context.Background(), cdnTraceIPTimeout)
	defer ipCancel()
	t.IPs = lookupIPs(ipCtx, r, cur)
	for _, ip := range t.IPs {
		if isCloudflareIP(ip) {
			t.ViaCDN = true
			// 关键区分：IP 段命中只说明经过 Cloudflare，说明不了是哪个账号
			t.Basis = "解析到 " + ip + "，属 Cloudflare 公布的边缘 IP 段——流量经过 Cloudflare，" +
				"但链路上没有一跳落在我方纳管的 CF 账号里，可能是第三方 CF 账号或未纳管的站点"
			return t
		}
	}
	// 精确记录没命中，但 CNAME 链上有一跳的根域是我方纳管的 CF 站点——
	// 这已经足以说明流量进了我们那套 CDN。精确记录查不到可能只是该子域没在
	// CF 里单独登记（走通配符），不该因此判成「不走 CDN」。
	if !t.Managed {
		if hop, zone := matchManagedZone(db, t.Hops); zone != "" {
			t.Managed = true
			t.ViaCDN = true
			t.MatchHop = hop
			t.Basis = "CNAME 链指向 " + hop + "，其根域 " + zone +
				" 是我方纳管的 CDN 站点——流量进入我方 CDN（该 FQDN 在 CF 里没有单独的解析记录，可能走通配符）"
			return t
		}
	}

	if t.Basis == "" {
		if len(t.IPs) > 0 {
			t.Basis = "解析到 " + strings.Join(t.IPs, ", ") + "，不属于 Cloudflare IP 段，也未命中我方 CDN 站点——直连源站"
		} else if t.Err == "" {
			t.Basis = "未解析到任何地址"
		} else {
			t.Basis = t.Err
		}
	}
	return t
}

// lookupCDNRecord 在纳管的 CF 数据里查这个 FQDN。返回 zone、是否开代理、是否命中。
func lookupCDNRecord(db *sql.DB, fqdn string) (zone string, proxied bool, ok bool) {
	var p int
	err := db.QueryRow(`SELECT zone_name, proxied FROM cdn_dns_records WHERE name=? ORDER BY proxied DESC LIMIT 1`,
		fqdn).Scan(&zone, &p)
	if err != nil {
		return "", false, false
	}
	return zone, p == 1, true
}

func lookupIPs(ctx context.Context, r *net.Resolver, host string) []string {
	addrs, err := r.LookupHost(ctx, host)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if len(out) >= 6 { // 够判断了，CF 一个域名能返回一堆边缘 IP
			break
		}
		out = append(out, a)
	}
	return out
}

// matchManagedZone 找出 CNAME 链上第一个根域属于我方纳管 CF 站点的跳。
//
// 精确 FQDN 匹配之外的第二重依据：子域可能没在 CF 里单独登记（走通配符解析），
// 此时 cdn_dns_records 查不到这条 FQDN，但它的根域确实在我们的 CF 账号下。
func matchManagedZone(db *sql.DB, hops []string) (hop, zone string) {
	rows, err := db.Query(`SELECT name FROM cdn_zones`)
	if err != nil {
		return "", ""
	}
	defer rows.Close()
	zones := []string{}
	for rows.Next() {
		var z string
		if rows.Scan(&z) == nil && z != "" {
			zones = append(zones, z)
		}
	}
	// 从链路末端往前找：越靠后越接近真实入口
	for i := len(hops) - 1; i >= 0; i-- {
		for _, z := range zones {
			if hops[i] == z || strings.HasSuffix(hops[i], "."+z) {
				return hops[i], z
			}
		}
	}
	return "", ""
}

package handlers

import (
	"net"
	"regexp"
	"strings"
)

// TLS 巡检失败原因归类。
//
//	## 为什么必须在后端做
//
//	前端本来有一份 reasonOf()，但它只能看见 cert_check_msg 这一个字符串。
//	而"这条失败到底算不算失败"取决于**解析值是不是内网地址**——这个字段
//	前端拿不到。于是 148 条失败里混着约 70 条内网地址的记录：巡检器在公网
//	去连一个 10./172.16. 的地址，必然连不上，这不是故障，是这条记录压根
//	不该被公网探测。把它们和真失败混在一起，"检测失败 148"这个数字就没法用——
//	看的人只能整列忽略。
//
//	归类放后端还有第二个原因：MCP/AI 读的是接口，前端算的它拿不到。
//	同一个失败在人眼里是"内网不适用"、在 AI 眼里是"检测失败"，
//	这种不一致我们已经栽过（前端按 auth_source 自行推导权限那次）。

// 巡检结果的适用范围。
const (
	scopePublic   = "public"   // 公网可达目标，探测结果有效
	scopeInternal = "internal" // 内网地址，公网探测本就不适用——不算失败
)

// tlsReason 一条巡检失败的归类结果。
type tlsReason struct {
	Key   string // 稳定的分组键，前端按它筛选
	Label string // 给人看的中文说明
	Scope string // public / internal
}

// 从错误串里取**连接目标**的 IP。
//
//	⚠️ 不能扫"错误串里出现的任意 IP"。Go 的网络错误里至少有三种 IP：
//	  连接目标      dial tcp 172.16.14.161:443: i/o timeout          ← 要的是这个
//	  本机源地址    read tcp 10.0.0.5:51228->1.2.3.4:443: reset      ← 探测器自己
//	  DNS 服务器    dial tcp: lookup a.com on 10.96.0.10:53: no such host
//
//	第三种是最坑的：集群里 DNS 服务器就是 10.96.0.10 这种内网地址，
//	于是**每一条 DNS 解析失败**的错误串里都有个内网 IP。第一版我按"出现即算"
//	判断，本地实测把 125 条 DNS 失败全归成了"内网不适用"——真失败被藏起来，
//	比原来混在一起更糟。所以只认这两种明确是目标的写法。
var (
	dialTargetRe = regexp.MustCompile(`dial (?:tcp|udp)\s+(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):\d+`)
	remoteEndRe  = regexp.MustCompile(`->(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}):\d+`)
)

// isPrivateIP 判断是不是内网/不可公网路由的地址。
//
//	用 net 包自带的判定，不手写网段：RFC1918、回环、链路本地都覆盖到。
//	额外补 CGNAT（100.64/10）——GKE 和一些云内网会用这一段，
//	net.IP 不把它算 private，但它同样不可公网路由。
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return true
	}
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return true // 100.64.0.0/10 CGNAT
	}
	return false
}

// msgTargetIsPrivate 错误串里的**连接目标**是内网地址。
//
//	这是内网判定的第二条路：有些记录的 origin_ip 没落库（CNAME 链、
//	或者采集时还没解析到），但探测时的错误串里带着实际连的地址。
//	取不到目标就返回 false——宁可把它当成真失败让人看见，
//	也不要凭一个不确定的判据把真故障吞成"不适用"。
func msgTargetIsPrivate(msg string) bool {
	for _, re := range []*regexp.Regexp{dialTargetRe, remoteEndRe} {
		if m := re.FindStringSubmatch(msg); len(m) == 2 {
			return isPrivateIP(net.ParseIP(m[1]))
		}
	}
	return false
}

// originIsInternal 解析目标是否**全部**是内网地址。
//
//	origin_ip 可能是逗号分隔的多个地址（005 迁移的注释写明了）。
//	只要其中有一个公网地址，这条记录就应该能被公网探测到——
//	那它的失败是真失败，不能算"不适用"。所以要求全部为内网才判 internal。
func originIsInternal(originIP string) bool {
	parts := strings.Split(originIP, ",")
	seen := false
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		ip := net.ParseIP(p)
		if ip == nil {
			return false // 有解析不出来的值（CNAME 之类），不敢断言是内网
		}
		if !isPrivateIP(ip) {
			return false
		}
		seen = true
	}
	return seen
}

// classifyTLSError 把一条 cert_check_msg 归成原因类别。
//
//	originIP 是这条解析记录的目标地址（A 记录的值，可能为空）。
//	msg 为空表示这条没失败——返回零值，调用方据此判断。
func classifyTLSError(msg, originIP string) tlsReason {
	m := strings.TrimSpace(msg)
	if m == "" {
		return tlsReason{}
	}
	low := strings.ToLower(m)

	// 内网优先：先判范围再判原因。一条内网记录连不上的具体原因
	// （超时/拒绝/解析不到）没有排查价值——它本来就不该被公网探测。
	internal := originIsInternal(originIP) || msgTargetIsPrivate(m)
	if internal {
		return tlsReason{Key: "internal", Label: "内网地址，公网探测不适用", Scope: scopeInternal}
	}

	r := func(key, label string) tlsReason {
		return tlsReason{Key: key, Label: label, Scope: scopePublic}
	}
	switch {
	case strings.Contains(low, "no such host") || strings.Contains(low, "server misbehaving"):
		return r("dns", "DNS 解析不到该域名")
	case strings.Contains(low, "i/o timeout") || strings.Contains(low, "context deadline exceeded") ||
		strings.Contains(low, "timeout"):
		return r("timeout", "连接 443 超时（多半被防火墙丢包）")
	case strings.Contains(low, "connection refused"):
		return r("refused", "443 端口拒绝连接（未提供 HTTPS）")
	// EOF 和 connection reset 是同一类：TCP 通了但对端没按 TLS 说话。
	// 常见于 443 上跑的不是 TLS、或中间设备切断握手。
	// 这两个此前一条分支都没有，全都掉进"其他"里当原文展示。
	case strings.Contains(low, "connection reset"):
		return r("reset", "TLS 握手被对端重置（连接已建立）")
	case low == "连接 443 失败: eof" || strings.HasSuffix(low, ": eof") || strings.Contains(low, "unexpected eof"):
		return r("eof", "对端直接断开，443 上可能不是 TLS 服务")
	case strings.Contains(low, "no route to host") || strings.Contains(low, "network is unreachable"):
		return r("unreachable", "网络不可达")
	case strings.Contains(low, "handshake failure") || strings.Contains(low, "protocol version") ||
		strings.Contains(low, "unsupported"):
		return r("tls", "TLS 握手失败（协议/加密套件不匹配）")
	case strings.Contains(low, "x509") || strings.Contains(low, "certificate"):
		return r("x509", "证书链校验失败")
	case strings.Contains(low, "未取到证书"):
		return r("nocert", "连上了但没拿到证书")
	}
	// ⚠️ 认不出来的**不能闷头归到"其他"**——那会掩盖新出现的失败模式。
	// 保留原文前缀作为分组键，界面上就能看见"有一类没见过的失败，N 条"。
	return r("other:"+truncate(m, 40), truncate(m, 40))
}

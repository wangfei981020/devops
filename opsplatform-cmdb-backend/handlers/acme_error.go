package handlers

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"
)

// ACME 失败原因归纳。
//
//	## 为什么需要
//
//	签发失败时 last_error 存的是 ACME 的原始报文，长这样：
//
//	    obtain: error: one or more domains had a problem: [*.g66-uat.com]
//	    invalid authorization: acme: error: 400 :: urn:ietf:params:acme:error:dns
//	    :: DNS problem: NXDOMAIN looking up TXT for _acme-challenge.g66-uat.com
//
//	这串东西对人几乎没用：看得懂的人不需要它，看不懂的人也不知道该干嘛。
//	更麻烦的是，生产上那几条失败**根本不是"重试就能好"的**——
//	它们是配置本身注定申请不下来（内网域名找公网 CA 签、域名没有解析、
//	手动模式没人加记录）。不归纳的话，人会反复点重试，
//	每次都等一遍超时，还消耗 Let's Encrypt 的速率配额。
//
//	原始报文**照旧存着**（追溯要用），归纳只是加一层解释。

// AcmeReason 一条归纳后的失败原因。
type AcmeReason struct {
	Code string `json:"code"`
	// Title 一句话说清是什么问题（给列表里当标签用）
	Title string `json:"title"`
	// Detail 为什么会这样
	Detail string `json:"detail"`
	// Action 该怎么办。**这是最重要的字段**——没有它，归纳只是换个说法。
	Action string `json:"action"`
	// Retryable 重试有没有意义。false = 不改配置的话重试多少次都一样，
	// 界面据此把「重试」按钮置灰，别让人白等一轮超时还烧掉 LE 的配额。
	Retryable bool `json:"retryable"`
}

// classifyAcmeError 把原始 ACME 报文归纳成可行动的原因。
//
//	匹配顺序有讲究：从**最具体**到最笼统。比如 NXDOMAIN 和 unauthorized
//	都属于 dns-01 校验失败，但处置完全不同，必须先判具体的那个。
func classifyAcmeError(raw, challenge, cn string) *AcmeReason {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	s := strings.ToLower(raw)

	switch {
	// 手动 DNS 模式没人去加 TXT 记录。这个最容易被当成"系统坏了"，
	// 其实是流程卡在人这一步。
	// ⚠️ 判据要**编码无关**。原来只匹配中文"等待手动添加"，一旦这串字在
	//	某个环节被转码（实测：mysql 客户端没带 --default-character-set 就会变乱码）
	//	就归不了类，界面退回到"未归类"。`error presenting token` 是 lego 库
	//	发出的固定英文串，稳得多，放在最前面判。
	case strings.Contains(s, "error presenting token") ||
		strings.Contains(raw, "等待手动添加") ||
		(strings.Contains(s, "manual") && strings.Contains(s, "timeout")):
		return &AcmeReason{
			Code: "manual_dns_timeout", Title: "手动添加 TXT 记录超时",
			Detail: "这张证书用的是 manual-dns 模式：CMDB 生成校验值后要**人工**去 DNS 服务商加一条 TXT 记录，" +
				"20 分钟内没等到就放弃了。不是系统故障，是流程卡在人这一步。",
			Action: "重新签发，并在弹出校验值后立刻去 DNS 服务商加 TXT 记录；" +
				"或者把这个域名改用 dns-01 自动模式（需要该域名的 DNS 托管在已接入的服务商）。",
			Retryable: true,
		}

	// NXDOMAIN。⚠️ 这里藏着两个处置完全相反的问题，而**报文本身区分不了**：
	//
	//	  a) 域名真的没有公网解析（托管区被删/域名过期）→ 重试多少次都没用
	//	  b) 域名好好的，只是那条 _acme-challenge TXT 当时还没被公网看到
	//	     → 等一会儿重试就好
	//
	//	CA 两种情况给的都是 `NXDOMAIN looking up TXT for _acme-challenge.<域名>`，
	//	报文里没有任何字段能把 a 和 b 分开——2026-08-05 那批被归成 a 的记录，
	//	事后查主域其实全都正常解析，说明当时多半是 b 被误判了。
	//	所以这里**实际去查一次主域的 NS**，用事实来分，而不是猜。
	case strings.Contains(s, "nxdomain"):
		switch apexResolvable(apexFromAcmeError(raw, cn)) {
		case resolveOK:
			return &AcmeReason{
				Code: "dns_challenge_not_propagated", Title: "校验记录还没传播开（NXDOMAIN）",
				Detail: "CA 查 `_acme-challenge.<域名>` 的 TXT 时没查到。**主域的 NS 是好的**（已实时核对），" +
					"所以不是域名没解析，是那条校验记录当时还没在公网生效。\n" +
					"最常见的成因是**负缓存**：写 TXT 之前只要有人查过这个名字，「不存在」就会被" +
					"递归解析器按托管区 SOA 的 minimum 缓存住（GoDaddy 实测 600 秒）。" +
					"签发程序探测时直连权威服务器、绕过缓存，所以它看得到；CA 走递归解析器，" +
					"命中还没过期的负缓存，就扑空了。",
				Action: "**等 10 分钟以上再重试**，别连着点——连着重试会撞同一份负缓存，" +
					"还白白烧掉 Let's Encrypt 的配额（同一组域名每周 5 次重复签发）。\n" +
					"想先确认，手动查一次：`dig TXT _acme-challenge.<域名> @8.8.8.8`。",
				Retryable: true,
			}
		case resolveNotFound:
			return &AcmeReason{
				Code: "dns_nxdomain", Title: "域名没有公网解析（NXDOMAIN）",
				Detail: "Let's Encrypt 在公网查 _acme-challenge 的 TXT 记录时，发现这个域名根本不存在。" +
					"已实时核对：**主域的 NS 也查不到**，通常是域名已过期/未接入 DNS，或托管区被删了。",
				Action: "先确认这个域名还在用：查它的 NS 和到期日。若已废弃，把这张证书删掉或标忽略，" +
					"别让它一直占着失败位；若还要用，先把 DNS 托管配好再签发。",
				Retryable: false,
			}
		default: // resolveUnknown：查不动，别给结论
			return &AcmeReason{
				Code: "dns_nxdomain_unknown", Title: "校验没通过（NXDOMAIN，主域状态未知）",
				Detail: "CA 查 `_acme-challenge.<域名>` 的 TXT 没查到。这可能是域名真的没解析，" +
					"也可能只是校验记录还没传播开——**本次核对主域 NS 时没查通**（本机 DNS 不可用或超时），" +
					"所以无法替你判断是哪一种。这里不猜。",
				Action: "手动跑一次 `dig NS <域名> @8.8.8.8`：\n" +
					"　· 查得到 NS → 是传播问题，等 10 分钟以上再重试；\n" +
					"　· 查不到 NS → 域名没解析，先把 DNS 托管配好，重试没有意义。",
				Retryable: true,
			}
		}

	// 内网域名找公网 CA 签——这个从设计上就不可能成功。
	case strings.Contains(s, "unauthorized") && isInternalName(cn):
		return &AcmeReason{
			Code: "internal_domain_public_ca", Title: "内网域名不能用公网 CA 签发",
			Detail: "这是一个内网域名（含 inner/internal/.local 等），而 Let's Encrypt 是公网 CA——" +
				"它必须能从公网校验域名归属，内网域名做不到。这不是配置错误，是路子选错了。",
			Action: "改用内部 CA 自签，或者把校验用的 _acme-challenge 子域**单独**放到公网 DNS 上" +
				"（只暴露校验记录，业务解析仍在内网）。继续重试不会成功。",
			Retryable: false,
		}

	case strings.Contains(s, "unauthorized"):
		return &AcmeReason{
			Code: "dns_unauthorized", Title: "DNS 校验未通过（403 unauthorized）",
			Detail: "CA 没能在公网查到正确的 _acme-challenge TXT 记录。常见原因：记录写到了别的托管区、" +
				"DNS 尚未生效、或该域名的 DNS 不在已接入的服务商手里。",
			Action: "手动查一次 `dig TXT _acme-challenge.<域名>` 看记录在不在、值对不对；" +
				"确认该域名的 DNS 托管方和 CMDB 里配的数据源是同一个。",
			Retryable: true,
		}

	// LE 的速率限制。重试只会让配额更紧张。
	case strings.Contains(s, "too many") || strings.Contains(s, "rate limit") || strings.Contains(s, "ratelimited"):
		return &AcmeReason{
			Code: "rate_limited", Title: "触发 CA 速率限制",
			Detail: "Let's Encrypt 对同一主域名有签发频率限制（每周 50 张证书、同一组域名 5 次重复签发）。" +
				"反复重试失败的签发是最常见的耗尽方式。",
			Action: "先把失败原因修好再签，别继续重试——**重试本身就在消耗配额**。" +
				"限制通常一周内自然恢复。",
			Retryable: false,
		}

	case strings.Contains(s, "caa"):
		return &AcmeReason{
			Code: "caa_forbids", Title: "CAA 记录不允许该 CA 签发",
			Detail:    "域名的 CAA 记录限定了可签发的 CA，而当前 CA 不在允许列表里。",
			Action:    "给域名加一条 CAA 记录放行（如 `0 issue \"letsencrypt.org\"`），或改用被允许的 CA。",
			Retryable: false,
		}

	case strings.Contains(s, "connection refused") || strings.Contains(s, "timeout") || strings.Contains(s, "deadline exceeded"):
		return &AcmeReason{
			Code: "network", Title: "网络不通或超时",
			Detail:    "访问 CA 或 DNS 服务商的接口超时了，多半是网络抖动或出口受限。",
			Action:    "稍后重试。持续失败的话检查 CMDB 出网策略和 DNS 服务商接口可用性。",
			Retryable: true,
		}

	case strings.Contains(s, "panic"):
		return &AcmeReason{
			Code: "internal_panic", Title: "签发过程内部错误",
			Detail:    "CMDB 自身在签发过程中崩了，这是 bug，不是配置问题。",
			Action:    "把这条错误连同域名反馈给平台维护者，日志里有完整堆栈。",
			Retryable: true,
		}
	}

	return &AcmeReason{
		Code: "unknown", Title: "未归类的签发失败",
		Detail:    "这个错误还没有对应的归类规则，原始报文见下方。",
		Action:    "把原始报文反馈给平台维护者，可以补一条归类规则进来。",
		Retryable: true,
	}
}

// isInternalName 判断是不是内网域名。
//
//	这类域名找公网 CA 签是**注定失败**的——CA 必须能从公网验证归属。
//	判据保守一点：宁可漏判（归到笼统的 unauthorized，用户还能自己看出来），
//	也别把公网域名误判成内网、然后建议人家去用内部 CA。
func isInternalName(cn string) bool {
	// ⚠️ 必须按**域名标签**匹配，不能用子串包含。
	//	`winner.com` 里含 "inner."，用 strings.Contains 会把一个公网域名
	//	判成内网，然后建议人家"改用内部 CA"——把能修好的问题引向死路。
	//	（这条是被自己的单测抓出来的。）
	c := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(cn, "*."), "."))
	labels := strings.Split(c, ".")
	internal := map[string]bool{
		"inner": true, "internal": true, "intranet": true,
		"local": true, "lan": true, "intra": true, "corp": true,
	}
	for _, l := range labels {
		if internal[l] {
			return true
		}
	}
	return false
}

// ---------- NXDOMAIN 归因：主域到底解不解析 ----------

// resolveState 主域 NS 的查询结果。三态，不是布尔——「查不动」和「查不到」
// 是两回事，混成一个会把"我不知道"说成"域名死了"。
type resolveState int

const (
	resolveOK       resolveState = iota // 查到 NS，域名活着
	resolveNotFound                     // 权威答复 NXDOMAIN，域名真没解析
	resolveUnknown                      // 超时/本机 DNS 不可用，无法判断
)

// apexResolvable 查主域的 NS。做成变量是为了让单元测试能替换掉，
// 保持 classifyAcmeError 可测——线上走真实解析，测试里注入固定结果。
var apexResolvable = lookupApexNS

// publicResolvers 归因用的解析器。
//
//	⚠️ 不能用 net.DefaultResolver：
//	  · 容器里它读 /etc/resolv.conf → 集群 CoreDNS，和 CA 的视角不是一回事；
//	  · 实测（2026-09-18 本地验收）系统解析器查 LookupNS 很不稳定，同一个域名
//	    连查两次能一次成功一次超时，假域名也时而 NXDOMAIN 时而超时——
//	    那会让归类结果随机在「传播问题」和「主域状态未知」之间跳。
//	所以固定走公网递归解析器，和 acme.dns01Opts() 用的是同一组，口径一致。
var publicResolvers = []string{"8.8.8.8:53", "1.1.1.1:53"}

func newPublicResolver(server string) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, server)
		},
	}
}

// lookupApexNS 查主域的 NS，判断域名到底还在不在。
//
//	多个解析器依次试：**只要有一个查到 NS 就算活着**。
//	全部明确答 NXDOMAIN 才判定域名没解析——这个判定会把界面上的「重试」置灰，
//	错判的代价是让人以为没救了，所以宁可退回 Unknown 也不能轻易下这个结论。
func lookupApexNS(apex string) resolveState {
	if apex == "" {
		return resolveUnknown
	}
	notFound := 0
	for _, server := range publicResolvers {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		ns, err := newPublicResolver(server).LookupNS(ctx, apex)
		cancel()

		if err == nil && len(ns) > 0 {
			return resolveOK
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			notFound++
		}
	}
	if notFound == len(publicResolvers) {
		return resolveNotFound
	}
	return resolveUnknown
}

// apexFromAcmeError 取出该查哪个主域。
//
//	优先从报文里 `_acme-challenge.<域名>` 抠——那是 CA 实际查的那个名字，
//	比 cn 可靠（cn 可能是 `*.x.com` 这种通配符写法）。抠不到再退回 cn。
func apexFromAcmeError(raw, cn string) string {
	const marker = "_acme-challenge."
	if i := strings.Index(raw, marker); i >= 0 {
		rest := raw[i+len(marker):]
		end := strings.IndexFunc(rest, func(r rune) bool {
			return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ',' || r == ';' || r == '"'
		})
		if end > 0 {
			rest = rest[:end]
		}
		if d := strings.Trim(rest, ". "); d != "" {
			return d
		}
	}
	return strings.TrimPrefix(strings.TrimSpace(cn), "*.")
}

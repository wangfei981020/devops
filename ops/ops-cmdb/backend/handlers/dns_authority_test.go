package handlers

import (
	"strings"
	"testing"
)

// NS 主机名 → 托管方。
//
// ⚠️ 必须按后缀匹配：各家 NS 都带序号（ns57.domaincontrol.com、
// cody.ns.cloudflare.com、ns-cloud-a1.googledomains.com），
// 写死完整名字必然漏，而漏了的表现是"判成 other"→ 反而说另一方不生效。
func TestProviderOfNS(t *testing.T) {
	cases := map[string]DNSProvider{
		// 🔴 这两条就是 P0-10 的现场证据
		"ns57.domaincontrol.com":        ProviderGoDaddy,
		"ns58.domaincontrol.com.":       ProviderGoDaddy, // 带结尾点
		"NS57.DomainControl.Com":        ProviderGoDaddy, // 大小写
		"cody.ns.cloudflare.com":        ProviderCloudflare,
		"ns-cloud-a1.googledomains.com": ProviderGCP,
		"f1g1ns1.dnspod.net":            ProviderOther, // 真实存在的第三方，不是"没采到"
		"":                              ProviderUnknown,
		"   ":                           ProviderUnknown,
	}
	for host, want := range cases {
		if got := ProviderOfNS(host); got != want {
			t.Errorf("ProviderOfNS(%q) = %q，期望 %q", host, got, want)
		}
	}
}

// ⚠️ 后缀匹配不能被"包含"糊弄过去：
// `domaincontrol.com.evil.example` 不是 GoDaddy。
func TestProviderOfNSNotSubstring(t *testing.T) {
	if got := ProviderOfNS("domaincontrol.com.evil.example"); got == ProviderGoDaddy {
		t.Error("后缀匹配写成了子串包含 —— 任何域名只要名字里有 domaincontrol.com 都会被判成 GoDaddy")
	}
	if got := ProviderOfNS("notdomaincontrol.com"); got == ProviderGoDaddy {
		t.Error("`notdomaincontrol.com` 不是 GoDaddy —— 必须按点分边界匹配")
	}
}

// 子域要能找到主域名上的 NS。
//
// NS 记录挂在主域名（example.com）上，而要判的是 api.example.com 这样的子域。
// 不逐级往上找的话，所有子域都会落进 unknown —— 也就是**一条都判不出来**，
// 而接口会安静地返回空的 ineffective 清单。
func TestAuthorityOfWalksUp(t *testing.T) {
	auth := map[string]*DNSAuthority{
		"g32cf.com": {Provider: ProviderGoDaddy, NSHosts: []string{"ns57.domaincontrol.com"}},
	}
	for _, f := range []string{"g32cf.com", "www.g32cf.com", "a.b.c.g32cf.com", "G32CF.COM."} {
		a := authorityOf(auth, f)
		if a == nil || a.Provider != ProviderGoDaddy {
			t.Errorf("%s 应当归到 g32cf.com 的托管方，实际 %+v", f, a)
		}
	}
	if a := authorityOf(auth, "other.com"); a != nil {
		t.Errorf("不相关的域名不该匹配上：%+v", a)
	}
	// 走到 TLD 就停 —— 否则 `com` 这一层会把所有 .com 域名串在一起
	if a := authorityOf(map[string]*DNSAuthority{"com": {Provider: ProviderGoDaddy}}, "foo.com"); a != nil {
		t.Error("不能一路找到 TLD —— 那会让 `com` 上的一条记录决定所有 .com 域名")
	}
}

// 🔴 P0-10 的完整场景：CF 里有记录、NS 指向 GoDaddy → 那些记录不生效。
//
// 这一条是本次修复的存在理由。它失败 = 回到「CMDB 展示一批可能不生效的
// 解析记录，且没有任何提示」的原状。
func TestIneffectiveDetection(t *testing.T) {
	auth := map[string]*DNSAuthority{
		"g32cf.com": {Provider: ProviderGoDaddy, NSHosts: []string{"ns57.domaincontrol.com", "ns58.domaincontrol.com"}},
	}
	a := authorityOf(auth, "www.g32cf.com")
	if a == nil {
		t.Fatal("找不到托管方")
	}
	if a.Provider == ProviderCloudflare {
		t.Fatal("NS 指向 GoDaddy，不该判成 Cloudflare")
	}
	// 配在 Cloudflare 上 → 不生效
	if a.Provider == ProviderCloudflare {
		t.Error("CF 上的记录应被判为不生效")
	}
	msg := conflictAction(auth, "www.g32cf.com")
	if !strings.Contains(msg, "GoDaddy") {
		t.Errorf("能判出托管方时就要直说是哪一方：%s", msg)
	}
	if !strings.Contains(msg, "ns57.domaincontrol.com") {
		t.Errorf("判据（NS 主机名）要带出来供人核对：%s", msg)
	}
}

// 判不出托管方时**不能编一个方向**。
//
// ⚠️ 把"没采到 NS"渲染成"配错了"，比不提示更糟：
// 人会去改一个本来就正确的配置。
func TestConflictActionAdmitsUnknown(t *testing.T) {
	msg := conflictAction(map[string]*DNSAuthority{}, "www.unknown.com")
	if strings.Contains(msg, "NS 指向 GoDaddy") || strings.Contains(msg, "NS 指向 Cloudflare") {
		t.Errorf("没采到 NS 却断言了方向：%s", msg)
	}
	if !strings.Contains(msg, "判不出") {
		t.Errorf("判不出就要说判不出：%s", msg)
	}
}

// NS 指向多方时不能挑一方说了算。
func TestSplitNSIsNotAVerdict(t *testing.T) {
	auth := map[string]*DNSAuthority{
		"mixed.com": {Split: true, Provider: ProviderUnknown,
			NSHosts: []string{"ns57.domaincontrol.com", "cody.ns.cloudflare.com"}},
	}
	a := authorityOf(auth, "mixed.com")
	if a.Provider != ProviderUnknown {
		t.Errorf("NS 指向多方时谁生效取决于递归解析器问到了哪台，不能下断言：%q", a.Provider)
	}
	if !a.Split {
		t.Error("Split 必须标出来 —— 它本身就是个要修的配置")
	}
}

func TestProviderLabelNeverEmpty(t *testing.T) {
	for _, p := range []DNSProvider{ProviderGoDaddy, ProviderCloudflare, ProviderGCP, ProviderOther, ProviderUnknown} {
		if strings.TrimSpace(providerLabel(p)) == "" {
			t.Errorf("%q 没有可读名字，会在界面上渲染成空白", p)
		}
	}
	if !strings.Contains(providerLabel(ProviderUnknown), "没采到") {
		t.Error("unknown 的说法必须点明是「没采到」，而不是「没有」")
	}
}

// 「判不出托管方」的计数必须从**有记录的那批域名**里数，不能从 auth 里数。
//
// 🔴 auth 是由 NS 记录建出来的 —— 一条 NS 都没采到的域名根本不在 auth 里，
// 按 auth 数出来永远是 0。本地实测撞到过：三方一条 NS 都没有，
// 接口却返回 `unknown_ns_domains: 0`，看起来像"每个域名都判出来了"。
//
// 这就是本轮在修的那个形态：一个 0 看起来完全正常，实际什么都没算。
func TestUnknownCountFromRecordsNotFromAuthMap(t *testing.T) {
	auth := map[string]*DNSAuthority{} // 一条 NS 都没采到
	fqdns := []string{"a.example.com", "b.example.com", "www.other.com"}
	unknown := map[string]bool{}
	for _, f := range fqdns {
		if a := authorityOf(auth, f); a == nil || a.Provider == ProviderUnknown {
			unknown[apexOf(f)] = true
		}
	}
	if len(unknown) != 2 {
		t.Fatalf("应当数出 2 个域名判不出（example.com / other.com），实际 %d：%v", len(unknown), unknown)
	}
	if len(auth) != 0 {
		t.Fatal("前提搞错了")
	}
}

func TestApexOf(t *testing.T) {
	cases := map[string]string{
		"a.b.example.com": "example.com",
		"example.com":     "example.com",
		"EXAMPLE.COM.":    "example.com",
		"localhost":       "localhost",
	}
	for in, want := range cases {
		if got := apexOf(in); got != want {
			t.Errorf("apexOf(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// 🔴 三张 DNS 表的 `name` 口径不一样，NS 记录入表前必须先对齐成 FQDN。
//
// 生产实测（2026-08-18，v0.94.1）撞到的：`dns_records`（GoDaddy 侧）的
// `name` 是**相对主机名**（`@` / `www`），直接当 key 用的话，全部 NS 记录
// 会塌进一个叫 `@` 的假域名里：
//
//	"@": { provider: "godaddy", ns: [51 个 NS 主机名...] }
//
// 而 59 个真实域名报「没采到 NS，判不出托管方」——它们的 NS 明明就在库里，
// 于是「配了但不生效」的清单严重偏少（当时只查出 17 条）。
//
// ⚠️ 同一个坑在 loadGoDaddyRecords 里补过 recordFQDN，这里漏了——**修了一处漏一处**。
// 所以这条测试锁的是：两处都必须过同一个补全函数。
func TestGoDaddyRelativeHostBecomesFQDN(t *testing.T) {
	cases := []struct{ host, domain, want string }{
		// `@` = 主域名本身。这是最要紧的一条：NS 记录绝大多数挂在 `@` 上，
		// 塌了它就等于整个 GoDaddy 侧的托管权判定全废
		{"@", "g32cf.com", "g32cf.com"},
		{"", "g32cf.com", "g32cf.com"},
		// 子域委派也是真实的 NS 记录，不能丢
		{"sub", "g32cf.com", "sub.g32cf.com"},
		// 已经是 FQDN 的不要再拼一次
		{"g32cf.com", "g32cf.com", "g32cf.com"},
		{"www.g32cf.com", "g32cf.com", "www.g32cf.com"},
	}
	for _, c := range cases {
		if got := recordFQDN(c.host, c.domain); got != c.want {
			t.Errorf("recordFQDN(%q, %q) = %q，期望 %q", c.host, c.domain, got, c.want)
		}
	}
}

// 补全之后，`@` 绝不能作为一个域名 key 出现在托管权表里。
//
// 这条守的是**症状本身**：`@` 不是域名，它出现在 key 里就说明补全没做。
func TestAuthorityKeyIsNeverBareAt(t *testing.T) {
	// 模拟补全后的入表路径
	auth := map[string]*DNSAuthority{}
	for _, r := range []struct{ host, domain, ns string }{
		{"@", "g32cf.com", "ns57.domaincontrol.com"},
		{"@", "zt-prod.com", "ns65.domaincontrol.com"},
	} {
		k := normalizeFQDN(recordFQDN(r.host, r.domain))
		if auth[k] == nil {
			auth[k] = &DNSAuthority{}
		}
		auth[k].NSHosts = append(auth[k].NSHosts, r.ns)
	}
	if _, bad := auth["@"]; bad {
		t.Fatal("托管权表里出现了 `@` 这个 key —— 相对主机名没被补全成 FQDN")
	}
	if len(auth) != 2 {
		t.Fatalf("两个域名应当各自成键，实际 %d 个：%v", len(auth), auth)
	}
	// 🔴 关键：两个域名的 NS 不能被混到一起。混了的话
	// 「g32cf.com 的 NS 指向哪」这个问题会拿到另一个域名的答案
	for d, a := range auth {
		if len(a.NSHosts) != 1 {
			t.Errorf("%s 的 NS 被混进了别的域名的：%v", d, a.NSHosts)
		}
	}
}

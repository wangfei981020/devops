package handlers

import "testing"

// 用例全部来自**生产 certificates 表的真实 last_error**（2026-08-05 捞的 4 条）。
// 这几条的共同点：都不是"重试就能好"的，而是配置本身注定申请不下来。
// 不归纳的话人会反复点重试，每次等一轮超时，还烧掉 Let's Encrypt 的速率配额。
func TestClassifyAcmeError_生产真实报文(t *testing.T) {
	cases := []struct {
		name, cn, challenge, raw string
		wantCode                 string
		wantRetryable            bool
	}{
		{
			name: "域名没有公网解析", cn: "*.g66-uat.com", challenge: "dns-01",
			raw: "obtain: error: one or more domains had a problem: [*.g66-uat.com] " +
				"invalid authorization: acme: error: 400 :: urn:ietf:params:acme:error:dns :: " +
				"DNS problem: NXDOMAIN looking up TXT for _acme-challenge.g66-uat.com",
			wantCode: "dns_nxdomain", wantRetryable: false,
		},
		{
			name: "内网域名找公网 CA 签", cn: "*.inner.k8s-g32-uat.com", challenge: "dns-01",
			raw: "obtain: error: one or more domains had a problem: [*.inner.k8s-g32-uat.com] " +
				"invalid authorization: acme: error: 403 :: urn:ietf:params:acme:error:unauthorized :",
			wantCode: "internal_domain_public_ca", wantRetryable: false,
		},
		{
			name: "手动模式没人加 TXT 记录", cn: "*.k8s-g32-uat.com", challenge: "manual-dns",
			raw: "obtain: error: one or more domains had a problem: [*.k8s-g32-uat.com] " +
				"[*.k8s-g32-uat.com] acme: error presenting token: 等待手动添加 DNS TXT 记录超时（20 分钟）",
			wantCode: "manual_dns_timeout", wantRetryable: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyAcmeError(c.raw, c.challenge, c.cn)
			if got == nil {
				t.Fatal("不该返回 nil")
			}
			if got.Code != c.wantCode {
				t.Errorf("code = %q，期望 %q（归错类会给出错误的处置建议，比不归类更糟）", got.Code, c.wantCode)
			}
			if got.Retryable != c.wantRetryable {
				t.Errorf("retryable = %v，期望 %v", got.Retryable, c.wantRetryable)
			}
			if got.Action == "" {
				t.Error("Action 不能为空——没有「该怎么办」，归纳只是换了个说法")
			}
		})
	}
}

// 普通公网域名的 unauthorized 不能被误判成"内网域名"，
// 否则会建议人家去用内部 CA——把一个能修好的问题引向死路。
func TestClassifyAcmeError_公网域名不误判为内网(t *testing.T) {
	raw := "invalid authorization: acme: error: 403 :: urn:ietf:params:acme:error:unauthorized :"
	got := classifyAcmeError(raw, "dns-01", "*.example.com")
	if got.Code != "dns_unauthorized" {
		t.Fatalf("公网域名应归到 dns_unauthorized，实际 %q", got.Code)
	}
	if !got.Retryable {
		t.Error("公网域名的校验失败是可以修好再重试的")
	}
}

func TestIsInternalName(t *testing.T) {
	internal := []string{"*.inner.k8s-g32-uat.com", "svc.internal.corp", "db.local", "app.intra.net", "x.lan"}
	for _, s := range internal {
		if !isInternalName(s) {
			t.Errorf("%q 应判为内网域名", s)
		}
	}
	// 判据要保守：宁可漏判，也别把公网域名误判成内网
	public := []string{"*.g66-uat.com", "www.example.com", "api.slileisure.com", "winner.com"}
	for _, s := range public {
		if isInternalName(s) {
			t.Errorf("%q 是公网域名，误判成内网会把人引向「改用内部 CA」这条死路", s)
		}
	}
}

func TestClassifyAcmeError_速率限制不建议重试(t *testing.T) {
	got := classifyAcmeError("acme: error: 429 :: too many certificates already issued", "dns-01", "a.com")
	if got.Code != "rate_limited" {
		t.Fatalf("应归到 rate_limited，实际 %q", got.Code)
	}
	if got.Retryable {
		t.Error("触发速率限制时重试**本身就在消耗配额**，必须标成不可重试")
	}
}

func TestClassifyAcmeError_空错误返回nil(t *testing.T) {
	if classifyAcmeError("", "dns-01", "a.com") != nil {
		t.Error("没有错误时不该造一个原因出来")
	}
	if classifyAcmeError("   ", "dns-01", "a.com") != nil {
		t.Error("空白也算没错误")
	}
}

// 归类判据不能依赖中文字符串——它可能在传输/存储环节被转码。
// 实测踩过：mysql 客户端没带 --default-character-set 时中文变乱码，
// 归类直接退回"未归类"，界面上就看不到处置建议了。
func TestClassifyAcmeError_中文乱码时仍能归类(t *testing.T) {
	// 这串就是真实踩到的乱码形态（UTF-8 被按 latin1 解了一遍）
	garbled := "acme: error presenting token: ç­‰å¾…æ‰‹åŠ¨æ·»åŠ  DNS TXT è®°å½•è¶…æ—¶"
	got := classifyAcmeError(garbled, "manual-dns", "*.k8s-g32-uat.com")
	if got.Code != "manual_dns_timeout" {
		t.Fatalf("中文乱码时应靠英文串 `error presenting token` 归类，实际 %q", got.Code)
	}
}

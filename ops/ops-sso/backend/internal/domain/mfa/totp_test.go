package mfa

import (
	"testing"
	"time"
)

// RFC 6238 的官方测试向量（SHA-1，密钥 "12345678901234567890"）。
// 用官方向量而不是自己算一遍再断言 —— 后者只能证明"和我自己一致"，
// 证明不了"和用户手机上的 Google Authenticator 一致"。
func TestRFC6238Vectors(t *testing.T) {
	// "12345678901234567890" 的 base32
	const secret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	cases := []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}
	for _, c := range cases {
		got, err := Code(secret, time.Unix(c.unix, 0))
		if err != nil {
			t.Fatalf("unix=%d: %v", c.unix, err)
		}
		if got != c.want {
			t.Errorf("unix=%d: 期望 %s，得到 %s", c.unix, c.want, got)
		}
	}
}

func TestVerifyAcceptsDrift(t *testing.T) {
	secret, _ := NewSecret()
	now := time.Unix(1_700_000_000, 0)

	cur, _ := Code(secret, now)
	if !Verify(secret, cur, now) {
		t.Fatal("当前验证码必须通过")
	}
	// 手机慢 30 秒
	prev, _ := Code(secret, now.Add(-Period*time.Second))
	if !Verify(secret, prev, now) {
		t.Fatal("前一步的验证码应在容忍窗口内")
	}
	// 手机快 30 秒
	next, _ := Code(secret, now.Add(Period*time.Second))
	if !Verify(secret, next, now) {
		t.Fatal("后一步的验证码应在容忍窗口内")
	}
	// 差两步就不认了 —— 每放宽一步都在降低安全性
	far, _ := Code(secret, now.Add(2*Period*time.Second))
	if Verify(secret, far, now) {
		t.Fatal("超出 ±1 步的验证码必须拒绝")
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	secret, _ := NewSecret()
	now := time.Now()
	for _, c := range []string{"", "12345", "1234567", "abcdef", "000000 "} {
		if Verify(secret, c, now) {
			t.Errorf("%q 不该通过", c)
		}
	}
}

func TestProvisioningURI(t *testing.T) {
	u := ProvisioningURI("OneGate", "zhangwei", "ABCDEFGHIJKLMNOP")
	for _, want := range []string{"otpauth://totp/", "issuer=OneGate", "secret=ABCDEFGHIJKLMNOP", "period=30"} {
		if !contains(u, want) {
			t.Errorf("URI 里少了 %q：%s", want, u)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// ── 提权票据 ──

func TestTicketValidity(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	tk := Ticket{IssuedAt: now, ExpiresAt: now.Add(30 * time.Minute)}

	if !tk.Valid(now) {
		t.Fatal("刚签发的票据应有效")
	}
	if !tk.Valid(now.Add(29*time.Minute + 59*time.Second)) {
		t.Fatal("到期前 1 秒必须仍然有效")
	}
	if tk.Valid(now.Add(30 * time.Minute)) {
		t.Fatal("到点必须立刻失效 —— 「30 分钟」要是能拖到 31 分钟，这个数字就没意义了")
	}
}

// 票据作用域必须在路径分隔处对齐，否则「删项目」的票能用来删别的资源
func TestTicketScope(t *testing.T) {
	tk := Ticket{Scope: "/api/v2/projects"}
	cases := map[string]bool{
		"/api/v2/projects":           true,
		"/api/v2/projects/pay":       true,
		"/api/v2/projects/pay/repos": true,
		"/api/v2/projects-secret":    false, // ★ 名字像，但是另一个资源
		"/api/v2/projectsX":          false,
		"/api/v2/repositories":       false,
		"/api/v2":                    false,
	}
	for path, want := range cases {
		if got := tk.Covers(path); got != want {
			t.Errorf("Covers(%q) = %v，期望 %v", path, got, want)
		}
	}

	// 空作用域 = 覆盖该应用全部需验证操作
	any := Ticket{}
	if !any.Covers("/whatever") {
		t.Fatal("空作用域应覆盖全部路径")
	}
}

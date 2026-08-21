package idp

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func token(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	return "eyJhbGciOiJSUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}

func feishu() Config {
	return Config{
		ID: 1, Name: "飞书", Issuer: "https://open.feishu.cn", ClientID: "cli_abc",
		RedirectURI:  "https://gate.example.com/api/v1/auth/oidc/callback",
		AuthURL:      "https://open.feishu.cn/open-apis/authen/v1/index",
		SubjectClaim: "employee_id", NameClaim: "name", EmailClaim: "email",
		GroupsClaim: "groups", DeptClaim: "dept_path",
	}
}

func TestAuthorizeURLHasPKCEAndNonce(t *testing.T) {
	st, err := NewAuthState(1, "/console")
	if err != nil {
		t.Fatal(err)
	}
	u := feishu().AuthorizeURL(st)
	for _, want := range []string{
		"response_type=code", "client_id=cli_abc", "code_challenge_method=S256",
		"code_challenge=", "state=", "nonce=", "scope=openid%20profile%20email",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("授权地址里少了 %q\n%s", want, u)
		}
	}
	// verifier 绝不能出现在地址里 —— 出现了 PKCE 就白做了
	if strings.Contains(u, st.CodeVerifier) {
		t.Fatal("code_verifier 泄露到了授权地址里")
	}
}

func TestStateExpiry(t *testing.T) {
	st, _ := NewAuthState(1, "")
	now := st.CreatedAt
	if st.Expired(now.Add(9 * time.Minute)) {
		t.Fatal("10 分钟内不该过期")
	}
	if !st.Expired(now.Add(11 * time.Minute)) {
		t.Fatal("超过 10 分钟必须过期 —— 被截获的 state 不能长期可用")
	}
}

func TestValidateRejectsWrongIssuerAudienceNonce(t *testing.T) {
	cfg := feishu()
	st, _ := NewAuthState(1, "")
	st.Nonce = "N1"
	now := time.Unix(1_700_000_000, 0)
	base := map[string]any{
		"iss": "https://open.feishu.cn", "sub": "u1", "aud": "cli_abc",
		"exp": float64(now.Add(time.Hour).Unix()), "nonce": "N1",
	}

	cl, _ := ParseClaims(token(base))
	if err := cfg.Validate(cl, st, now); err != nil {
		t.Fatalf("正常 token 应通过：%v", err)
	}

	// 别人的 IdP 签的 token
	bad := clone(base)
	bad["iss"] = "https://evil.example.com"
	cl, _ = ParseClaims(token(bad))
	if err := cfg.Validate(cl, st, now); err == nil {
		t.Fatal("issuer 不符必须拒绝")
	}

	// 给别的应用签的 token 被拿来复用
	bad = clone(base)
	bad["aud"] = "cli_other"
	cl, _ = ParseClaims(token(bad))
	if err := cfg.Validate(cl, st, now); err == nil {
		t.Fatal("audience 不含本客户端必须拒绝")
	}

	// 重放：nonce 对不上
	bad = clone(base)
	bad["nonce"] = "N2"
	cl, _ = ParseClaims(token(bad))
	if err := cfg.Validate(cl, st, now); err == nil {
		t.Fatal("nonce 不符必须拒绝")
	}

	// 过期
	bad = clone(base)
	bad["exp"] = float64(now.Add(-time.Minute).Unix())
	cl, _ = ParseClaims(token(bad))
	if err := cfg.Validate(cl, st, now); err == nil {
		t.Fatal("过期 token 必须拒绝")
	}
}

func TestAudienceArrayForm(t *testing.T) {
	cfg := feishu()
	st, _ := NewAuthState(1, "")
	now := time.Unix(1_700_000_000, 0)
	cl, _ := ParseClaims(token(map[string]any{
		"iss": cfg.Issuer, "sub": "u1", "aud": []any{"other", "cli_abc"},
		"exp": float64(now.Add(time.Hour).Unix()), "nonce": st.Nonce,
	}))
	if err := cfg.Validate(cl, st, now); err != nil {
		t.Fatalf("aud 是数组时也应通过：%v", err)
	}
}

// ★ 我们发了 nonce，上游就必须原样回传。
// token 里干脆不带 nonce 时如果放行，攻击者只要**省略这个字段**就绕过了重放保护 ——
// 这条是写这组用例时被自己的测试失败逼出来的：一开始我以为是实现错了。
func TestMissingNonceIsRejected(t *testing.T) {
	cfg := feishu()
	st, _ := NewAuthState(1, "")
	now := time.Unix(1_700_000_000, 0)
	cl, _ := ParseClaims(token(map[string]any{
		"iss": cfg.Issuer, "sub": "u1", "aud": "cli_abc",
		"exp": float64(now.Add(time.Hour).Unix()),
		// 故意不带 nonce
	}))
	if err := cfg.Validate(cl, st, now); err == nil {
		t.Fatal("缺 nonce 必须拒绝 —— 否则省略字段即可绕过重放保护")
	}
}

// ★ 稳定标识缺失时必须停下来，绝不能退而求其次用 email
func TestMissingSubjectClaimIsFatal(t *testing.T) {
	cfg := feishu()
	cl, _ := ParseClaims(token(map[string]any{
		"sub": "u1", "email": "zhangwei@example.com", "name": "张伟",
	}))
	_, err := cfg.MapIdentity(cl)
	if err == nil {
		t.Fatal("配置的 employee_id 为空时必须报错，而不是回落到 email —— 人改邮箱就会变成另一个人")
	}
}

func TestMapIdentity(t *testing.T) {
	cfg := feishu()
	cl, _ := ParseClaims(token(map[string]any{
		"employee_id": "C10482", "name": "张伟", "email": "zhangwei@example.com",
		"groups": []any{"研发", "支付组"}, "dept_path": "/研发中心/后端组",
	}))
	id, err := cfg.MapIdentity(cl)
	if err != nil {
		t.Fatal(err)
	}
	if id.ExternalID != "C10482" || id.DisplayName != "张伟" || id.Email != "zhangwei@example.com" {
		t.Fatalf("映射结果不对：%+v", id)
	}
	if len(id.Groups) != 2 || id.DeptPath != "/研发中心/后端组" {
		t.Fatalf("组与部门映射不对：%+v", id)
	}
}

// 有的 IdP 把员工号当数字发；转字符串时不能带小数点
func TestNumericClaimHasNoDecimalPoint(t *testing.T) {
	cfg := feishu()
	cl, _ := ParseClaims(token(map[string]any{"employee_id": float64(10482)}))
	id, err := cfg.MapIdentity(cl)
	if err != nil {
		t.Fatal(err)
	}
	if id.ExternalID != "10482" {
		t.Fatalf("数字型 claim 应转成 %q，得到 %q（带小数点会让之后每次匹配都失败）", "10482", id.ExternalID)
	}
}

func TestNestedClaim(t *testing.T) {
	cfg := feishu()
	cfg.SubjectClaim = "user.employee_id"
	cl, _ := ParseClaims(token(map[string]any{
		"user": map[string]any{"employee_id": "C10482"},
	}))
	id, err := cfg.MapIdentity(cl)
	if err != nil || id.ExternalID != "C10482" {
		t.Fatalf("嵌套 claim 应取到值：%+v %v", id, err)
	}
}

func TestParseClaimsRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "a.b", "a.b.c.d", "x.!!!.y"} {
		if _, err := ParseClaims(bad); err == nil {
			t.Errorf("%q 应当解析失败", bad)
		}
	}
}

func clone(m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

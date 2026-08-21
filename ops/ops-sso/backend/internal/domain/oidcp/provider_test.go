package oidcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func client() Client {
	return Client{
		ClientID: "cmdb", AppID: 5, TenantID: 1,
		RedirectURIs: []string{
			"https://cmdb.corp.example.com/auth/callback",
			"http://localhost:5173/auth/callback",
		},
		PostLogout:  []string{"https://cmdb.corp.example.com/"},
		Scopes:      []string{"openid", "profile", "email", "groups"},
		RequirePKCE: true, IDTokenTTL: time.Hour,
		Claims:  []string{"sub", "name", "email", "groups", "employee_id"},
		Enabled: true,
	}
}

// ★ 回调地址必须精确匹配。
//
// 允许前缀/通配是 OIDC 最经典的一类漏洞：攻击者构造一个仍然"匹配"的地址，
// 就能让 code 直接送到自己手上，而整个登录流程看起来完全正常。
func TestRedirectURIIsExactMatchOnly(t *testing.T) {
	c := client()

	for _, ok := range c.RedirectURIs {
		if err := c.ValidateRedirectURI(ok); err != nil {
			t.Errorf("白名单里的地址应通过：%s", ok)
		}
	}

	bad := []string{
		"https://cmdb.corp.example.com/auth/callback2",    // 前缀相同
		"https://cmdb.corp.example.com/auth/callback/",    // 多个斜杠
		"https://cmdb.corp.example.com/auth/callback?x=1", // 带参数
		"https://cmdb.corp.example.com/auth/callback#x",   // 带片段
		"https://evil.com/?u=https://cmdb.corp.example.com/auth/callback",
		"https://cmdb.corp.example.com.evil.com/auth/callback", // 同源前缀
		"HTTPS://CMDB.CORP.EXAMPLE.COM/auth/callback",          // 大小写不同
		"",
	}
	for _, u := range bad {
		if err := c.ValidateRedirectURI(u); !errors.Is(err, ErrBadRedirectURI) {
			t.Errorf("%q 必须被拒绝，得到 %v", u, err)
		}
	}
}

func TestPostLogoutURI(t *testing.T) {
	c := client()
	if !c.ValidatePostLogout("") {
		t.Error("不指定登出跳转应允许")
	}
	if !c.ValidatePostLogout("https://cmdb.corp.example.com/") {
		t.Error("白名单里的应允许")
	}
	if c.ValidatePostLogout("https://evil.com/") {
		t.Error("白名单外的必须拒绝 —— 否则登出接口就是个开放重定向")
	}
}

// ── PKCE ──

func TestPKCEVerify(t *testing.T) {
	verifier := "a-verifier-that-is-long-enough-1234567890"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	if err := VerifyPKCE(challenge, "S256", verifier); err != nil {
		t.Fatalf("正确的 verifier 应通过：%v", err)
	}
	if err := VerifyPKCE(challenge, "S256", "wrong-verifier"); !errors.Is(err, ErrPKCEMismatch) {
		t.Fatal("错误的 verifier 必须被拒")
	}
	if err := VerifyPKCE(challenge, "S256", ""); !errors.Is(err, ErrPKCEMismatch) {
		t.Fatal("缺 verifier 必须被拒")
	}
}

// ★ 不支持 plain：challenge 就是 verifier 本身，
// 截获授权请求的人直接拿到 verifier —— 等于没做 PKCE
func TestPKCEPlainRejected(t *testing.T) {
	if err := VerifyPKCE("some-challenge", "plain", "some-challenge"); !errors.Is(err, ErrPKCEMismatch) {
		t.Fatal("plain 模式必须被拒 —— 它等于没做 PKCE")
	}
	if err := VerifyPKCE("x", "", "x"); !errors.Is(err, ErrPKCEMismatch) {
		t.Fatal("未指定 method 时必须被拒")
	}
}

func TestPKCESkippedWhenNoChallenge(t *testing.T) {
	// 没带 challenge 时本函数放行，由调用方按 client.RequirePKCE 决定
	if err := VerifyPKCE("", "", ""); err != nil {
		t.Fatalf("无 challenge 时应交由调用方决定：%v", err)
	}
}

// ── 授权码 ──

func TestAuthCodeIsHashedAndShortLived(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	ac, err := NewAuthCode("cmdb", 1001, 1, 77,
		"https://cmdb.corp.example.com/auth/callback", "N1", "openid profile",
		"chal", "S256", now, now)
	if err != nil {
		t.Fatal(err)
	}
	if ac.Code == "" || ac.Hash == "" {
		t.Fatal("应同时给出明文与哈希")
	}
	if ac.Hash == ac.Code {
		t.Fatal("存的必须是哈希，不是明文 —— 库泄露时未用的 code 也不该能换 token")
	}
	if HashCode(ac.Code) != ac.Hash {
		t.Fatal("哈希应可复算")
	}
	if got := ac.ExpiresAt.Sub(now); got != CodeTTL {
		t.Fatalf("有效期应为 %v，得到 %v", CodeTTL, got)
	}
	if CodeTTL > 2*time.Minute {
		t.Fatal("授权码活太久会延长它在浏览器历史/日志/Referer 里被截获的窗口")
	}
}

func TestAuthCodesAreUnique(t *testing.T) {
	now := time.Now()
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		ac, _ := NewAuthCode("cmdb", 1, 1, 0, "u", "", "", "", "", now, now)
		if seen[ac.Code] {
			t.Fatal("授权码重复 —— 随机源有问题")
		}
		seen[ac.Code] = true
	}
}

// ── id_token ──

func TestIDTokenClaims(t *testing.T) {
	c := client()
	s := Subject{UserID: 1001, Username: "zhangwei", DisplayName: "张伟",
		Email: "zhangwei@example.com", EmployeeID: "C10482",
		Groups: []string{"研发", "支付组"}, AuthSource: "oidc"}
	now := time.Unix(1_800_000_000, 0)

	cl := IDTokenClaims(c, s, "https://gate.example.com", "N1", "sid-1", now.Add(-time.Minute), now)

	if cl["iss"] != "https://gate.example.com" || cl["aud"] != "cmdb" {
		t.Fatalf("iss/aud 不对：%+v", cl)
	}
	if cl["sub"] != "1001" {
		t.Fatalf("sub 应为用户 ID 字符串，得到 %v", cl["sub"])
	}
	if cl["nonce"] != "N1" {
		t.Error("nonce 必须原样回传，否则下游无法防重放")
	}
	if cl["sid"] != "sid-1" {
		t.Error("必须带 sid —— 全局单点登出靠它通知下游")
	}
	if exp, iat := cl["exp"].(int64), cl["iat"].(int64); exp-iat != 3600 {
		t.Errorf("有效期应为客户端配置的 1 小时，得到 %d 秒", exp-iat)
	}
	// auth_time 是真正完成认证的时刻，不是签发 token 的时刻。
	// 下游据此判断"这个会话是不是刚认证过"
	if cl["auth_time"].(int64) != now.Add(-time.Minute).Unix() {
		t.Error("auth_time 应为认证时刻，不是签发时刻")
	}
	// ★ 走的哪条认证路径必须下发：应急通道进来的会话，下游应当更谨慎
	amr, _ := cl["amr"].([]string)
	if len(amr) != 1 || amr[0] != "oidc" {
		t.Errorf("amr 应带认证方式，得到 %v", cl["amr"])
	}
}

// ★ claim 白名单：应用要不到就泄不了
func TestClaimsAreWhitelisted(t *testing.T) {
	c := client()
	c.Claims = []string{"sub", "name"} // 只给这两样
	s := Subject{UserID: 1, DisplayName: "张伟", Email: "x@example.com",
		EmployeeID: "C1", Groups: []string{"研发"}, AuthSource: "local"}

	cl := IDTokenClaims(c, s, "https://g", "", "sid", time.Now(), time.Now())

	if _, ok := cl["email"]; ok {
		t.Error("未在白名单里的 email 不该下发")
	}
	if _, ok := cl["groups"]; ok {
		t.Error("未在白名单里的 groups 不该下发")
	}
	if _, ok := cl["employee_id"]; ok {
		t.Error("未在白名单里的 employee_id 不该下发")
	}
	if cl["name"] != "张伟" {
		t.Error("白名单里的 name 应该下发")
	}
}

func TestEmptyValuesAreOmitted(t *testing.T) {
	c := client()
	s := Subject{UserID: 1, AuthSource: "local"} // 什么都没有
	cl := IDTokenClaims(c, s, "https://g", "", "sid", time.Now(), time.Now())
	for _, k := range []string{"name", "email", "employee_id", "groups"} {
		if v, ok := cl[k]; ok {
			t.Errorf("空值不该写进 claim：%s = %v（下游会拿到空串并当成有效值）", k, v)
		}
	}
}

// ── 签名 ──

func TestSignAndStructure(t *testing.T) {
	k, err := NewSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	tok, err := SignIDToken(k, map[string]any{"sub": "1", "iss": "https://g"})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT 应有三段，得到 %d", len(parts))
	}

	hdrRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	var hdr map[string]string
	_ = json.Unmarshal(hdrRaw, &hdr)
	if hdr["alg"] != "RS256" {
		t.Errorf("alg 应为 RS256（生态里唯一被所有客户端库支持的），得到 %s", hdr["alg"])
	}
	if hdr["kid"] != k.Kid {
		t.Error("必须带 kid —— 密钥轮换时下游靠它选公钥")
	}
}

func TestPublicJWK(t *testing.T) {
	k, _ := NewSigningKey()
	jwk := k.PublicJWK()
	for _, f := range []string{"kty", "use", "alg", "kid", "n", "e"} {
		if _, ok := jwk[f]; !ok {
			t.Errorf("JWK 缺字段 %s", f)
		}
	}
	if jwk["kty"] != "RSA" || jwk["use"] != "sig" {
		t.Errorf("JWK 字段不对：%+v", jwk)
	}
	// 私钥绝不能出现在 JWK 里
	b, _ := json.Marshal(jwk)
	for _, leak := range []string{`"d"`, `"p"`, `"q"`, `"dp"`, `"dq"`, `"qi"`} {
		if strings.Contains(string(b), leak) {
			t.Fatalf("公开的 JWK 里出现了私钥字段 %s —— 这是灾难性泄露", leak)
		}
	}
}

// ── Discovery ──

func TestDiscoveryOnlyAdvertisesWhatWeImplement(t *testing.T) {
	d := Discovery("https://gate.example.com")

	if d["issuer"] != "https://gate.example.com" {
		t.Error("issuer 必须与配置一致 —— 下游会拿它校验 id_token 的 iss")
	}
	for _, k := range []string{
		"authorization_endpoint", "token_endpoint", "userinfo_endpoint",
		"jwks_uri", "end_session_endpoint",
	} {
		if v, _ := d[k].(string); !strings.HasPrefix(v, "https://gate.example.com/") {
			t.Errorf("%s 应基于 issuer，得到 %v", k, d[k])
		}
	}

	// ★ 只声明真正实现了的：声明了没做的，客户端库会照着调然后拿到 404，
	// 而对方的第一反应是"我配错了"
	rt, _ := d["response_types_supported"].([]string)
	if len(rt) != 1 || rt[0] != "code" {
		t.Errorf("只支持授权码流，不该声明其他，得到 %v", rt)
	}
	cm, _ := d["code_challenge_methods_supported"].([]string)
	if len(cm) != 1 || cm[0] != "S256" {
		t.Errorf("PKCE 只支持 S256，得到 %v", cm)
	}
	// 隐式流会把 token 放进 URL 片段，到处留痕 —— 不做也不声明
	for _, bad := range []string{"token", "id_token", "id_token token"} {
		for _, got := range rt {
			if got == bad {
				t.Errorf("不该声明隐式/混合流：%s", bad)
			}
		}
	}
}

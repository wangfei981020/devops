package handlers

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// SSRF：这个接口天生要发外部请求，最容易被做成内网探测入口。
func TestCheckIssuerSafe(t *testing.T) {
	bad := []struct{ in, why string }{
		{"http://accounts.google.com", "http 下 id_token 可被中间人替换"},
		{"https://127.0.0.1:8080", "回环"},
		{"https://10.0.0.5", "内网 A 段"},
		{"https://192.168.1.1", "内网 C 段"},
		{"https://169.254.169.254", "云元数据地址，泄的是实例凭据"},
		{"https://", "没有主机名"},
	}
	for _, tc := range bad {
		if err := checkIssuerSafe(tc.in, false); err == nil {
			t.Errorf("checkIssuerSafe(%q) 应当拒绝（%s），却放行了", tc.in, tc.why)
		}
	}
	// 公网域名要能过。解析不了就跳过——CI 里可能没有出网
	if err := checkIssuerSafe("https://accounts.google.com", false); err != nil &&
		!strings.Contains(err.Error(), "域名解析失败") {
		t.Errorf("公网 issuer 被拒: %v", err)
	}
}

// 自建身份源（内网 Keycloak / AD FS）是典型客户，不是攻击者。
// 显式勾选之后要能过，否则这一整类客户接不进来。
func TestCheckIssuerSafeAllowPrivate(t *testing.T) {
	ok := []string{
		"https://keycloak.internal.corp",
		"http://10.0.0.5:8080/realms/ops", // TLS 在入口层终止的常见形态
		"http://127.0.0.1:5556/dex",
	}
	for _, in := range ok {
		if err := checkIssuerSafe(in, true); err != nil {
			t.Errorf("勾选内网后 %q 仍被拒: %v", in, err)
		}
	}
	// 但开关本身不能变成"什么都放行"：协议还是要认
	if err := checkIssuerSafe("ftp://10.0.0.5", true); err == nil {
		t.Error("非 http(s) 协议即使勾选内网也不该放行")
	}
	if err := checkIssuerSafe("https://", true); err == nil {
		t.Error("缺主机名即使勾选内网也不该放行")
	}
}

// 开放跳转：?redirect= 不校验的话，这个接口就是个带我们域名的钓鱼跳板。
func TestStartRejectsOffsiteRedirect(t *testing.T) {
	keep := []struct{ in, want string }{
		{"/hosts", "/hosts"},
		{"/clusters?env=PROD", "/clusters?env=PROD"},
		{"https://evil.com", "/"},
		{"//evil.com", "/"}, // 协议相对地址，最容易漏掉的一种
		{"", "/"},
	}
	for _, tc := range keep {
		got := tc.in
		if !strings.HasPrefix(got, "/") || strings.HasPrefix(got, "//") {
			got = "/"
		}
		if got != tc.want {
			t.Errorf("redirect %q => %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

// id_token 是从浏览器转发来的，验签是唯一的安全边界。
// alg=none 和 alg=HS256（拿公钥当 HMAC 密钥）是两条经典绕过路径。
func TestVerifyIDTokenRejectsWeakAlg(t *testing.T) {
	for _, m := range []jwt.SigningMethod{jwt.SigningMethodNone, jwt.SigningMethodHS256} {
		tok := jwt.NewWithClaims(m, jwt.MapClaims{"sub": "admin"})
		var raw string
		var err error
		if m == jwt.SigningMethodNone {
			raw, err = tok.SignedString(jwt.UnsafeAllowNoneSignatureType)
		} else {
			raw, err = tok.SignedString([]byte("secret"))
		}
		if err != nil {
			t.Fatalf("造 token 失败: %v", err)
		}
		// keyfunc 里那道 SigningMethodRSA 断言必须先拦下来。
		// 拦不住的话，任何人都能自己造一个 {"sub":"admin"} 登进来
		_, perr := jwt.Parse(raw, func(tk *jwt.Token) (any, error) {
			if _, ok := tk.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, jwt.ErrTokenSignatureInvalid
			}
			return nil, nil
		})
		if perr == nil {
			t.Errorf("alg=%s 的 token 被接受了", m.Alg())
		}
	}
}

// redirect_uri 拼错一个字符，IdP 只会回一句 redirect_uri_mismatch。
// 所以它必须由后端算好显示给客户，而不是让客户自己拼。
func TestRedirectURIFollowsProxyHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct{ host, proto, want string }{
		{"localhost:30833", "", "http://localhost:30833/api/auth/sso/callback"},
		{"cmdb.example.com", "https", "https://cmdb.example.com/api/auth/sso/callback"},
	}
	for _, tc := range cases {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/api/idp-config", nil)
		c.Request.Host = tc.host
		if tc.proto != "" {
			c.Request.Header.Set("X-Forwarded-Proto", tc.proto)
		}
		if got := redirectURI(c); got != tc.want {
			t.Errorf("redirectURI = %q，期望 %q", got, tc.want)
		}
	}
}

// 有 IdP 把 sub 给成数字。当成字符串取会得到空串，
// 表现是"登录失败：缺少 subject"，而 id_token 里明明有。
func TestClaimStrAcceptsNumericSub(t *testing.T) {
	if got := claimStr(jwt.MapClaims{"sub": float64(1234567)}, "sub"); got != "1234567" {
		t.Errorf("数字 sub 取出来是 %q", got)
	}
	if got := claimStr(jwt.MapClaims{"sub": "abc"}, "nope"); got != "" {
		t.Errorf("缺失的 claim 应当返回空串，得到 %q", got)
	}
}

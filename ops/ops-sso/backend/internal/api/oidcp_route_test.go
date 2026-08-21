package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/domain/oidcp"
)

// ★ discovery 里声明的每个端点都必须真的有路由。
//
// 这条是端到端跑出来的：discovery 写着 /oidc/authorize，而路由挂在
// /api/v1/oidc/authorize —— 客户端库照着 discovery 调，拿到 404，
// 而对方的第一反应是"我配错了"，排查方向从一开始就是错的。
//
// 让这件事在**单测里**炸，而不是在客户联调时炸。
func TestDiscoveryPathsAreRouted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r, Deps{Issuer: "https://gate.example.com"})

	// 把注册过的路由收进集合
	routed := map[string]bool{}
	for _, ri := range r.Routes() {
		routed[ri.Method+" "+ri.Path] = true
	}

	d := oidcp.Discovery("https://gate.example.com")
	cases := []struct{ key, method string }{
		{"authorization_endpoint", http.MethodGet},
		{"token_endpoint", http.MethodPost},
		{"userinfo_endpoint", http.MethodGet},
		{"jwks_uri", http.MethodGet},
		{"end_session_endpoint", http.MethodGet},
	}
	for _, c := range cases {
		full, _ := d[c.key].(string)
		path := strings.TrimPrefix(full, "https://gate.example.com")
		if !routed[c.method+" "+path] {
			t.Errorf("discovery 声明了 %s = %s，但没有对应的 %s %s 路由 —— "+
				"客户端库会照着调然后拿到 404", c.key, full, c.method, path)
		}
	}

	// discovery 文档本身也要能访问，且**不需要认证** ——
	// 客户端库在配置阶段就会拉它，那时还没有任何凭证
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil))
	if w.Code != http.StatusOK {
		t.Errorf("discovery 文档必须无需认证即可访问，得到 %d", w.Code)
	}
}

// issuer 必须与 discovery 里所有端点的前缀一致 ——
// 下游会拿 issuer 校验 id_token 的 iss，两者不一致时校验必然失败
func TestIssuerMatchesEndpoints(t *testing.T) {
	const iss = "https://gate.corp.example.com"
	d := oidcp.Discovery(iss)
	if d["issuer"] != iss {
		t.Fatalf("issuer 应为 %s，得到 %v", iss, d["issuer"])
	}
	for _, k := range []string{
		"authorization_endpoint", "token_endpoint", "userinfo_endpoint",
		"jwks_uri", "end_session_endpoint",
	} {
		v, _ := d[k].(string)
		if !strings.HasPrefix(v, iss+"/") {
			t.Errorf("%s 应以 issuer 开头，得到 %s", k, v)
		}
	}
}

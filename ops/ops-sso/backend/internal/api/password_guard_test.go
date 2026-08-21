package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/domain/auth"
)

// 造一个"已登录但必须改密"的请求上下文
func guardRouterWithMustChange(must bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	fakeAuth := func(c *gin.Context) {
		bind(c, auth.Identity{UserID: 1004, TenantID: 1, Username: "admin", MustChange: must})
		c.Next()
	}
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	g := r.Group("/api/v1", fakeAuth, passwordChangeGuard())
	g.GET("/apps", ok)
	g.POST("/app-groups", ok)
	g.GET("/portal/apps", ok)
	g.GET("/audit-logs", ok)
	g.POST("/auth/password", ok)
	g.POST("/auth/logout", ok)
	g.GET("/auth/me", ok)
	return r
}

func code(r *gin.Engine, method, path string) int {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w.Code
}

// ★ 强制改密必须在**服务端**拦住。
//
// 只在前端跳转到改密页是可以绕过的 —— 直接调接口就行。
// 而"强制改密"这件事的全部意义就在于它不能被绕过。
func TestMustChangeBlocksEverythingElse(t *testing.T) {
	r := guardRouterWithMustChange(true)

	blocked := []struct{ m, p string }{
		{"GET", "/api/v1/apps"},
		{"POST", "/api/v1/app-groups"},
		{"GET", "/api/v1/portal/apps"},
		{"GET", "/api/v1/audit-logs"},
	}
	for _, c := range blocked {
		got := code(r, c.m, c.p)
		if got != http.StatusPreconditionRequired {
			t.Errorf("必须改密时 %s %s 应被拦（428），得到 %d", c.m, c.p, got)
		}
	}
}

// 428 而不是 403：告诉前端"还差一步"，而不是"不行"。
// 403 会让界面显示成一个死胡同，而这里明明有下一步可走。
func TestMustChangeUses428NotForbidden(t *testing.T) {
	r := guardRouterWithMustChange(true)
	w := httptest.NewRecorder()
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/apps", nil))
	_ = w2
	if w.Code == http.StatusForbidden {
		t.Fatal("不该用 403：403 表示「不行」，而这里是「还差一步」")
	}
	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("应为 428，得到 %d", w.Code)
	}
	if !contains(w.Body.String(), "auth.must_change_password") {
		t.Errorf("响应里应带原因码，得到 %s", w.Body.String())
	}
}

// ★ 放行清单必须足够小，但不能少了这三个
func TestMustChangeAllowsOnlyEscapeHatches(t *testing.T) {
	r := guardRouterWithMustChange(true)
	allowed := []struct{ m, p, why string }{
		{"POST", "/api/v1/auth/password", "改密本身"},
		{"POST", "/api/v1/auth/logout", "不让退出会把人困死在页面上"},
		{"GET", "/api/v1/auth/me", "前端靠它知道自己处于必须改密的状态"},
	}
	for _, c := range allowed {
		if got := code(r, c.m, c.p); got != http.StatusOK {
			t.Errorf("%s %s 必须放行（%s），得到 %d", c.m, c.p, c.why, got)
		}
	}
}

// 改完密之后一切照常
func TestNoGuardWhenNotRequired(t *testing.T) {
	r := guardRouterWithMustChange(false)
	for _, p := range []string{"/api/v1/apps", "/api/v1/portal/apps", "/api/v1/audit-logs"} {
		if got := code(r, "GET", p); got != http.StatusOK {
			t.Errorf("不需要改密时 %s 应正常，得到 %d", p, got)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/internal/store"
)

func init() { gin.SetMode(gin.TestMode) }

// fakeResolver 让中间件可测，不用起数据库。
type fakeResolver struct {
	id            store.TenantID
	impersonating bool
	err           error
}

func (f *fakeResolver) Resolve(*gin.Context) (store.TenantID, bool, error) {
	return f.id, f.impersonating, f.err
}

// run 跑一次请求，返回状态码与 handler 里看到的租户上下文。
func run(t *testing.T, r TenantResolver, extra ...gin.HandlerFunc) (int, store.TenantID, bool) {
	t.Helper()
	var seenCtx store.TenantID
	var seenOK bool

	g := gin.New()
	g.Use(Tenant(r))
	for _, h := range extra {
		g.Use(h)
	}
	g.GET("/x", func(c *gin.Context) {
		// 从**标准 context** 里读 —— store 层用的是这个，不是 gin.Context
		seenCtx, seenOK = store.TenantFrom(c.Request.Context())
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	return w.Code, seenCtx, seenOK
}

// ★ 租户必须落到标准 context 里 —— store 层只认那个。
// 只 c.Set 到 gin.Context 是最容易犯的错：编译通过、看起来对，但 store 拿不到。
func TestTenantLandsInRequestContext(t *testing.T) {
	code, id, ok := run(t, &fakeResolver{id: 7})
	if code != http.StatusOK {
		t.Fatalf("状态码 = %d, want 200", code)
	}
	if !ok || id != 7 {
		t.Errorf("store 上下文里的租户 = (%d, %v), want (7, true)", id, ok)
	}
}

// 没有租户时不拦请求 —— 平台级接口（租户管理、license）本来就不需要租户。
func TestNoTenantDoesNotBlock(t *testing.T) {
	code, _, ok := run(t, &fakeResolver{id: 0})
	if code != http.StatusOK {
		t.Errorf("无租户时不该拦，得到 %d", code)
	}
	if ok {
		t.Error("无租户时不该往 context 里塞值")
	}
}

// ★ 越权必须明确 403，不能静默降级成「无租户」。
// 后者会让攻击者以为只是查不到数据，继续试别的路径。
func TestNotMemberIsRejectedWith403(t *testing.T) {
	code, _, _ := run(t, &fakeResolver{err: errNotMember})
	if code != http.StatusForbidden {
		t.Errorf("越权应返回 403，得到 %d", code)
	}
}

// 其余错误（会话查不到之类）放行，交给认证中间件与 store 层处理。
func TestOtherErrorsPassThrough(t *testing.T) {
	code, _, ok := run(t, &fakeResolver{err: errors.New("db down")})
	if code != http.StatusOK {
		t.Errorf("非越权错误应放行，得到 %d", code)
	}
	if ok {
		t.Error("出错时不该塞租户上下文")
	}
}

// RequireTenant 给业务路由用：没选租户就明确告知，而不是让 store 报一个开发向的错。
func TestRequireTenantBlocksWhenAbsent(t *testing.T) {
	code, _, _ := run(t, &fakeResolver{id: 0}, RequireTenant())
	if code != http.StatusBadRequest {
		t.Errorf("RequireTenant 在无租户时应 400，得到 %d", code)
	}
}

func TestRequireTenantPassesWhenPresent(t *testing.T) {
	code, _, _ := run(t, &fakeResolver{id: 3}, RequireTenant())
	if code != http.StatusOK {
		t.Errorf("有租户时应放行，得到 %d", code)
	}
}

// TenantOf 从 gin 上下文取值，供 handler 使用。
func TestTenantOf(t *testing.T) {
	g := gin.New()
	g.Use(Tenant(&fakeResolver{id: 9}))
	var got store.TenantID
	g.GET("/x", func(c *gin.Context) { got = TenantOf(c) })
	g.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if got != 9 {
		t.Errorf("TenantOf = %d, want 9", got)
	}
}

func TestTenantOfWithoutContext(t *testing.T) {
	g := gin.New()
	var got store.TenantID = 99
	g.GET("/x", func(c *gin.Context) { got = TenantOf(c) })
	g.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if got != 0 {
		t.Errorf("无上下文时 TenantOf = %d, want 0", got)
	}
}

// ★ 端到端：中间件注入 → store 层能用它做查询。
// 这条串起两个包，防的是「各自都对但接不上」——
// 比如中间件塞的是 int64 而 store 期望 TenantID。
func TestEndToEndWithStore(t *testing.T) {
	g := gin.New()
	g.Use(Tenant(&fakeResolver{id: 5}))

	var scopedID store.TenantID
	var scopeErr error
	st := store.New(nil)
	g.GET("/x", func(c *gin.Context) {
		sc, err := st.Tenant(c.Request.Context())
		if err != nil {
			scopeErr = err
			return
		}
		scopedID = sc.TenantID()
	})
	g.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if scopeErr != nil {
		t.Fatalf("store 未能从请求上下文取到租户: %v", scopeErr)
	}
	if scopedID != 5 {
		t.Errorf("store 作用域租户 = %d, want 5", scopedID)
	}
}

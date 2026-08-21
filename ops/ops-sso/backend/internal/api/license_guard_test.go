package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"ops-sso-backend/internal/license"
)

// 造一个处于指定状态的授权管理器。
//
// 直接 Load 一个 payload 而不是签一串激活码再验：验签是共享内核的事，
// 它自己的测试锁着；这里要测的是中间件在**给定状态**下的行为。
func mgrWithStatus(t *testing.T, expired bool) *license.Manager {
	t.Helper()
	exp := time.Now().AddDate(1, 0, 0)
	if expired {
		// ⚠️ 要减掉整个宽限期才进只读。到期后 14 天内功能完全不受影响 ——
		// 这与本产品切换内核前的语义相反（旧实现到期即只读），见 license.GraceDays。
		exp = time.Now().AddDate(0, 0, -license.GraceDays-1)
	}
	m := license.NewManager()
	m.Load(&license.Payload{
		LicenseID: "LIC-TEST", IssuedAt: time.Now().AddDate(-1, 0, 0), ExpiresAt: exp,
		Products: map[string]license.ProductGrant{
			license.ProductID: {Features: []string{"gateway_connect", "path_policy"}},
		},
	}, "")
	return m
}

func guardRouter(m *license.Manager) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	d := Deps{License: m}
	g := r.Group("/api/v1", licenseWriteGuard(d))
	ok := func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	g.GET("/apps", ok)
	g.POST("/apps", ok)
	g.DELETE("/policies/:id", ok)
	g.POST("/policies/simulate", ok)
	g.POST("/auth/logout", ok)
	g.POST("/mfa/challenge", ok)
	g.POST("/auth/break-glass/issue", ok)
	return r
}

func do(r *gin.Engine, method, path string) int {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	return w.Code
}

func TestGuardAllowsEverythingWhenActive(t *testing.T) {
	r := guardRouter(mgrWithStatus(t, false))
	for _, c := range []struct{ m, p string }{
		{"GET", "/api/v1/apps"}, {"POST", "/api/v1/apps"}, {"DELETE", "/api/v1/policies/1"},
	} {
		if got := do(r, c.m, c.p); got != http.StatusOK {
			t.Errorf("授权正常时 %s %s 应放行，得到 %d", c.m, c.p, got)
		}
	}
}

// ★ 过期后：读全放行，写拒绝 —— 但**访问判定不受影响**（那条路径在网关里，压根不查 license）
func TestGuardBlocksWritesWhenExpired(t *testing.T) {
	r := guardRouter(mgrWithStatus(t, true))

	if got := do(r, "GET", "/api/v1/apps"); got != http.StatusOK {
		t.Errorf("过期后读接口必须照常，得到 %d", got)
	}
	for _, c := range []struct{ m, p string }{
		{"POST", "/api/v1/apps"}, {"DELETE", "/api/v1/policies/1"},
	} {
		if got := do(r, c.m, c.p); got != http.StatusPaymentRequired {
			t.Errorf("过期后 %s %s 应拒绝并给 402，得到 %d", c.m, c.p, got)
		}
	}
}

// ★ 即使只读期，这几个写操作也必须能用 ——
// 一个过期后连退出都点不动的系统，客户第一反应是"被绑架了"
func TestGuardKeepsEscapeHatchesOpen(t *testing.T) {
	r := guardRouter(mgrWithStatus(t, true))
	for _, p := range []string{
		"/api/v1/auth/logout",
		"/api/v1/mfa/challenge",
		"/api/v1/policies/simulate",
	} {
		if got := do(r, "POST", p); got != http.StatusOK {
			t.Errorf("只读期 %s 必须仍可用，得到 %d", p, got)
		}
	}
}

// 签发应急口令属于配置变更，只读期不给做 —— 白名单不能开得太宽
func TestGuardBlocksBreakGlassIssueWhenExpired(t *testing.T) {
	r := guardRouter(mgrWithStatus(t, true))
	if got := do(r, "POST", "/api/v1/auth/break-glass/issue"); got != http.StatusPaymentRequired {
		t.Errorf("只读期签发应急口令应被拒（属于配置变更），得到 %d", got)
	}
}

// 没有授权管理器时不能把写操作全挡掉 —— 那会让未装 license 的环境完全不可用
func TestGuardNoManagerDoesNotBlock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1", licenseWriteGuard(Deps{}))
	g.POST("/apps", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	if got := do(r, "POST", "/api/v1/apps"); got != http.StatusOK {
		t.Errorf("无授权管理器时不应阻断，得到 %d", got)
	}
}

func TestIsWrite(t *testing.T) {
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if !isWrite(m) {
			t.Errorf("%s 应算写", m)
		}
	}
	for _, m := range []string{"GET", "HEAD", "OPTIONS"} {
		if isWrite(m) {
			t.Errorf("%s 不该算写", m)
		}
	}
}

// ★ 开放重定向：`?next=https://evil.example.com` 会让攻击者拿我们的域名做钓鱼跳板，
// 而整个登录流程看起来完全正常。只允许站内相对路径。
func TestSafeNextBlocksOpenRedirect(t *testing.T) {
	blocked := []string{
		"https://evil.example.com",
		"http://evil.example.com/x",
		"//evil.example.com", // 协议相对，浏览器当绝对地址处理
		"///evil.example.com",
		"javascript:alert(1)",
		"",
		"relative/path", // 不以 / 开头，拼上去会落到当前目录
	}
	for _, n := range blocked {
		if got := safeNext(n); got != "/" {
			t.Errorf("safeNext(%q) = %q，应回落到 /", n, got)
		}
	}

	allowed := []string{"/", "/console", "/console/policies?tab=1", "/portal#a"}
	for _, n := range allowed {
		if got := safeNext(n); got != n {
			t.Errorf("safeNext(%q) = %q，站内路径应原样保留", n, got)
		}
	}
}

// ★ 宽限期内**写操作照常** —— 这与本产品切换共享内核前的语义相反。
//
//	旧实现：到期即转只读，"宽限 30 天"指的是只读但不停服
//	共享内核：到期后 14 天内功能完全不受影响，超出才转只读
//
// 采用后者是 LICENSING §5 的规定，也因为"过期第二天就配不了东西"
// 会把一次续费流程延误直接变成客户的生产事故。
func TestGuardAllowsWritesDuringGrace(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := license.NewManager()
	m.Load(&license.Payload{
		LicenseID: "LIC-TEST",
		IssuedAt:  time.Now().AddDate(-1, 0, 0),
		ExpiresAt: time.Now().AddDate(0, 0, -1), // 昨天到期，仍在宽限期内
		Products: map[string]license.ProductGrant{
			license.ProductID: {Features: []string{"gateway_connect"}},
		},
	}, "")

	if !m.CanWrite() {
		t.Fatalf("宽限期内必须可写，status=%s", m.Status())
	}
	if got := do(guardRouter(m), "POST", "/api/v1/apps"); got != http.StatusOK {
		t.Errorf("宽限期内写操作应放行，得到 %d", got)
	}
}

// 未激活（社区版）必须可写：连第一个应用都接不进来的话，装上去就是个空壳。
func TestGuardAllowsWritesWhenUnlicensed(t *testing.T) {
	m := license.NewManager()
	if !m.CanWrite() {
		t.Fatal("社区版必须可写 —— 否则新装的客户什么都配不了")
	}
	if got := do(guardRouter(m), "POST", "/api/v1/apps"); got != http.StatusOK {
		t.Errorf("社区版写操作应放行，得到 %d", got)
	}
}

// ★ 贴激活码是**唯一的自救通道**，只读期必须放行。
//
// 不放行的话，授权过期之后连续费都装不上 —— 客户付了钱也只能找我们远程改库。
// 这条和 break-glass/issue 那条恰好相反：那个属于配置变更，只读期不给做。
func TestGuardKeepsLicenseActivationOpen(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := mgrWithStatus(t, true) // 超出宽限期，已只读
	if m.CanWrite() {
		t.Fatal("前置条件不成立：这里应该已经是只读态")
	}
	r := gin.New()
	g := r.Group("/api/v1", licenseWriteGuard(Deps{License: m}))
	g.POST("/license", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	if got := do(r, "POST", "/api/v1/license"); got != http.StatusOK {
		t.Errorf("只读期必须仍能贴激活码，得到 %d —— 否则过期即死锁", got)
	}
}

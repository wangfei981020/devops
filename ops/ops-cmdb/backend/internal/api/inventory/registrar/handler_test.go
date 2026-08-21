package registrar

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"ops-cmdb-backend/crypto"
	"ops-cmdb-backend/internal/store"
	"ops-cmdb-backend/internal/testutil"
)

func init() { gin.SetMode(gin.TestMode) }

// serve 造一个带指定租户上下文的请求，返回响应。
//
// 直接构造 context 而不是走完整的认证中间件 —— 这里测的是**隔离**，
// 不是认证。认证在自己的测试里验。
func serve(h *Handler, tenant store.TenantID, method, path, body string) *httptest.ResponseRecorder {
	g := gin.New()
	g.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(store.WithTenant(c.Request.Context(), tenant))
		c.Next()
	})
	h.Register(g.Group(""))

	var r *http.Request
	if body != "" {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}

func newHandler(t *testing.T, env *testutil.TenantEnv) *Handler {
	t.Helper()
	c, err := crypto.New("test-key")
	if err != nil {
		t.Fatalf("构造 cipher: %v", err)
	}
	return New(env.Store, c)
}

// ★ 这条测试证明原实现的漏洞是真的，也证明修复有效。
//
// 原实现 Update 用 `WHERE id = ?` —— 租户 A 只要知道 id 就能改租户 B 的注册商，
// 而注册商里存着域名厂商的 API 凭据（能改 DNS、能续费扣钱）。
//
// 这类漏洞的特征：功能测试全过（改自己的能成功），**只有跨租户用例能抓到**。
func TestUpdateCannotTouchOtherTenant(t *testing.T) {
	env := testutil.NewTenantEnv(t)
	defer env.Close()
	env.SeedRow("registrars", []string{"name", "provider", "enabled"}, "godaddy-acct", "godaddy", 1)

	h := newHandler(t, env)
	// 租户 A 拿着租户 B 的行 id 去改
	w := serve(h, env.A.ID, http.MethodPut,
		"/registrars/"+itoa(env.B.RowID),
		`{"name":"hijacked","provider":"evil","enabled":0}`)

	if w.Code != http.StatusNotFound {
		t.Errorf("跨租户 Update 返回 %d，want 404（403 会泄露该 id 存在）", w.Code)
	}

	// 更要紧的是：B 的数据必须没被动过
	var name string
	err := env.DB.QueryRow(`SELECT name FROM registrars WHERE id = ?`, env.B.RowID).Scan(&name)
	if err != nil {
		t.Fatalf("查 B 的数据: %v", err)
	}
	if name != "godaddy-acct" {
		t.Errorf("租户 B 的数据被租户 A 改成了 %q —— 跨租户越权写", name)
	}
}

// ★ 删除同样不能跨租户。
func TestDeleteCannotTouchOtherTenant(t *testing.T) {
	env := testutil.NewTenantEnv(t)
	defer env.Close()
	env.SeedRow("registrars", []string{"name", "provider", "enabled"}, "acct", "godaddy", 1)

	h := newHandler(t, env)
	w := serve(h, env.A.ID, http.MethodDelete, "/registrars/"+itoa(env.B.RowID), "")

	if w.Code != http.StatusNotFound {
		t.Errorf("跨租户 Delete 返回 %d，want 404", w.Code)
	}
	env.AssertVisible(t, "registrars", env.B.RowID, env.B.ID)
}

// 列表只返回自己租户的行。
func TestListIsScopedToTenant(t *testing.T) {
	env := testutil.NewTenantEnv(t)
	defer env.Close()
	env.SeedRow("registrars", []string{"name", "provider", "enabled"}, "acct", "godaddy", 1)

	h := newHandler(t, env)
	w := serve(h, env.A.ID, http.MethodGet, "/registrars", "")
	if w.Code != http.StatusOK {
		t.Fatalf("List 返回 %d", w.Code)
	}
	body := w.Body.String()
	// A 的那行在，B 的那行不在
	if !strings.Contains(body, itoa(env.A.RowID)) {
		t.Errorf("列表里没有自己的行 %d：%s", env.A.RowID, body)
	}
	if strings.Contains(body, itoa(env.B.RowID)) {
		t.Errorf("列表里出现了别的租户的行 %d —— 跨租户泄露：%s", env.B.RowID, body)
	}
}

// 新建的行必须落在自己租户名下，不能没有归属。
func TestCreateLandsInOwnTenant(t *testing.T) {
	env := testutil.NewTenantEnv(t)
	defer env.Close()
	env.SeedRow("registrars", []string{"name", "provider", "enabled"}, "seed", "godaddy", 1)

	before := env.CountFor("registrars", env.A.ID)
	h := newHandler(t, env)
	w := serve(h, env.A.ID, http.MethodPost, "/registrars",
		`{"name":"new-acct","provider":"godaddy"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("Create 返回 %d: %s", w.Code, w.Body.String())
	}
	if after := env.CountFor("registrars", env.A.ID); after != before+1 {
		t.Errorf("A 租户行数 %d → %d，期望 +1", before, after)
	}
	if n := env.CountFor("registrars", env.B.ID); n != 1 {
		t.Errorf("B 租户行数变成了 %d —— 新建的行落到了别人名下", n)
	}
}

// ★ 没有租户上下文时必须拒绝，不能当成"查全部"。
func TestNoTenantContextIsRejected(t *testing.T) {
	env := testutil.NewTenantEnv(t)
	defer env.Close()

	h := newHandler(t, env)
	// 不挂租户中间件
	g := gin.New()
	h.Register(g.Group(""))
	w := httptest.NewRecorder()
	g.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/registrars", nil))

	if w.Code == http.StatusOK {
		t.Errorf("无租户上下文时返回 200 —— 应当拒绝而不是查全表：%s", w.Body.String())
	}
}

// 凭据绝不能出现在列表响应里。生产上出过两个 P0，都是这类。
func TestCredentialNeverLeaksInList(t *testing.T) {
	env := testutil.NewTenantEnv(t)
	defer env.Close()

	h := newHandler(t, env)
	// 先建一个带凭据的
	w := serve(h, env.A.ID, http.MethodPost, "/registrars",
		`{"name":"acct","provider":"godaddy","credential":{"api_key":"SUPER-SECRET-KEY","api_secret":"SECRET-VALUE"}}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("Create 返回 %d: %s", w.Code, w.Body.String())
	}
	env.SeedRow("registrars", []string{"name", "provider", "enabled"}, "x", "y", 1) // 便于 Close 清理

	w = serve(h, env.A.ID, http.MethodGet, "/registrars", "")
	body := w.Body.String()
	for _, secret := range []string{"SUPER-SECRET-KEY", "SECRET-VALUE", "credential_enc"} {
		if strings.Contains(body, secret) {
			t.Errorf("列表响应里泄露了凭据内容 %q：%s", secret, body)
		}
	}
	if !strings.Contains(body, `"has_cred":true`) {
		t.Errorf("应当只回传「有没有凭据」的标志：%s", body)
	}
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// ★ 正常路径：改自己的行，字段必须落到正确的列上。
//
// 跨租户用例挡不住参数错位这类 bug —— 绑错位时 WHERE 条件同样匹配不到行，
// 于是照样返回 404，测试照样"通过"。必须有一条"改成功并逐字段核对"的用例。
func TestUpdateOwnRowBindsColumnsCorrectly(t *testing.T) {
	env := testutil.NewTenantEnv(t)
	defer env.Close()
	env.SeedRow("registrars", []string{"name", "provider", "enabled"}, "before", "godaddy", 1)

	h := newHandler(t, env)
	w := serve(h, env.A.ID, http.MethodPut,
		"/registrars/"+itoa(env.A.RowID),
		`{"name":"after","provider":"namecheap","enabled":0}`)
	if w.Code != http.StatusOK {
		t.Fatalf("改自己的行返回 %d，body=%s", w.Code, w.Body.String())
	}

	var name, provider string
	var enabled int
	var tenantID int64
	err := env.DB.QueryRow(`SELECT tenant_id, name, provider, enabled FROM registrars WHERE id = ?`, env.A.RowID).
		Scan(&tenantID, &name, &provider, &enabled)
	if err != nil {
		t.Fatalf("回查: %v", err)
	}
	if tenantID != int64(env.A.ID) {
		t.Errorf("tenant_id 被改成了 %d（应保持 %d）—— 参数错位", tenantID, env.A.ID)
	}
	if name != "after" || provider != "namecheap" || enabled != 0 {
		t.Errorf("字段落错位置：name=%q provider=%q enabled=%d，want after/namecheap/0",
			name, provider, enabled)
	}
}

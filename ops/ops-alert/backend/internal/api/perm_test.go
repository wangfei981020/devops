package api

import (
	"strings"
	"testing"
)

// 这些路由由 Register 注册，与真实注册顺序无关——
// 测试关心的是"每一条都能解析出权限码"，不是它们怎么挂上去的。
//
// ⚠️ 新增接口时必须往这里加一行。忘了加的后果不是测试失败，
// 而是**运行期 403**（fail-closed），那时候排查成本高得多。
// 启动自检 AuditPermCheck 会打印同样的清单，两道防线是互补的：
// 这里拦的是"写代码时"，那里拦的是"跑起来时动态注册的路由"。
var allRoutes = []struct{ method, path string }{
	{"GET", "/api/v1/me"},
	{"GET", "/api/v1/license"},
	{"GET", "/api/v1/license/fingerprint"},
	{"POST", "/api/v1/license"},
	{"GET", "/api/v1/selfcheck"},
	{"GET", "/api/v1/datasources"},
	{"POST", "/api/v1/datasources"},
	{"PUT", "/api/v1/datasources/:id"},
	{"DELETE", "/api/v1/datasources/:id"},
	{"POST", "/api/v1/datasources/:id/test"},
	{"GET", "/api/v1/notifiers"},
	{"POST", "/api/v1/notifiers"},
	{"DELETE", "/api/v1/notifiers/:id"},
	{"POST", "/api/v1/notifiers/:id/test"},
	{"GET", "/api/v1/routes"},
	{"POST", "/api/v1/routes"},
	{"DELETE", "/api/v1/routes/:id"},
	{"POST", "/api/v1/routes/simulate"},
	{"GET", "/api/v1/silences"},
	{"POST", "/api/v1/silences"},
	{"DELETE", "/api/v1/silences/:id"},
	{"GET", "/api/v1/audit"},
	{"GET", "/api/v1/report"},
	{"PUT", "/api/v1/report"},
	{"POST", "/api/v1/report/preview"},
	{"POST", "/api/v1/report/send"},
	{"POST", "/api/v1/explore"},
	{"GET", "/api/v1/msg-templates"},
	{"POST", "/api/v1/msg-templates"},
	{"PUT", "/api/v1/msg-templates/:id"},
	{"DELETE", "/api/v1/msg-templates/:id"},
	{"POST", "/api/v1/msg-templates/preview"},
	{"GET", "/api/v1/sso"},
	{"PUT", "/api/v1/sso"},
	{"POST", "/api/v1/sso/test"},
	{"GET", "/api/v1/rules"},
	{"GET", "/api/v1/rules/kinds"},
	{"GET", "/api/v1/rules/templates"},
	{"POST", "/api/v1/rules/templates/preview"},
	{"GET", "/api/v1/rules/:id"},
	{"GET", "/api/v1/rules/:id/runs"},
	{"GET", "/api/v1/rules/:id/quality"},
	{"POST", "/api/v1/rules"},
	{"PUT", "/api/v1/rules/:id"},
	{"DELETE", "/api/v1/rules/:id"},
	{"POST", "/api/v1/rules/:id/toggle"},
	{"POST", "/api/v1/rules/dryrun"},
	{"GET", "/api/v1/backtests"},
	{"GET", "/api/v1/backtests/:id"},
	{"POST", "/api/v1/backtests"},
	{"POST", "/api/v1/import/preflight"},
	{"POST", "/api/v1/import/apply"},
	{"GET", "/api/v1/noise/top"},
	{"GET", "/api/v1/incidents"},
	{"GET", "/api/v1/incidents/series"},
	{"GET", "/api/v1/incidents/:id"},
	{"GET", "/api/v1/incidents/:id/trace"},
	{"POST", "/api/v1/incidents/:id/ack"},
	{"GET", "/api/v1/users"},
	{"POST", "/api/v1/users"},
	{"PUT", "/api/v1/users/:id/role"},
	{"PUT", "/api/v1/users/:id/status"},
	{"PUT", "/api/v1/users/:id/password"},
	{"DELETE", "/api/v1/users/:id"},
	{"GET", "/api/v1/roles"},
	{"GET", "/api/v1/mcp/tokens"},
	{"POST", "/api/v1/mcp/tokens"},
	{"DELETE", "/api/v1/mcp/tokens/:id"},
}

func TestEveryRouteHasPermRule(t *testing.T) {
	for _, r := range allRoutes {
		if _, ok := resolvePerm(r.method, r.path); !ok {
			t.Errorf("路由没有权限规则，运行期会直接 403: %s %s", r.method, r.path)
		}
	}
}

// 读操作不能落到写权限码上。
//
// 这条测的是一类真实事故：只读角色打开页面，列表接口要写权限，
// 于是整页 403 —— 而"只读"这个角色存在的意义就是能看。
func TestReadRoutesNeedReadPerm(t *testing.T) {
	for _, r := range allRoutes {
		if r.method != "GET" {
			continue
		}
		code, ok := resolvePerm(r.method, r.path)
		if !ok {
			continue // 由 TestEveryRouteHasPermRule 报
		}
		if strings.HasPrefix(code, "alert:") {
			t.Errorf("GET 落到写权限码上，只读角色会看不了: %s %s → %s", r.method, r.path, code)
		}
	}
}

// 写操作必须要写权限码，只有显式列进 permExactRules 的除外。
//
// 反向的事故同样真实：某个 POST 漏配写权限，只读角色也能改数据，
// 而界面上一切正常——这类洞只有靠遍历才发现得了。
func TestWriteRoutesNeedWritePerm(t *testing.T) {
	for _, r := range allRoutes {
		if r.method == "GET" {
			continue
		}
		key := r.method + " " + r.path
		if _, exempt := permExactRules[key]; exempt {
			continue // 预演类接口：POST 但不改状态，见 permExactRules 的说明
		}
		code, ok := resolvePerm(r.method, r.path)
		if !ok {
			continue
		}
		if !strings.HasPrefix(code, "alert:") {
			t.Errorf("写操作没要求写权限，只读角色能改数据: %s → %s", key, code)
		}
	}
}

// 最长前缀优先。/api/v1/mcp/tokens 必须赢过 /api/v1/mcp，
// 否则令牌管理会落到错误的权限码上（而且是"更宽松"的那个）。
func TestLongestPrefixWins(t *testing.T) {
	code, ok := resolvePerm("GET", "/api/v1/mcp/tokens")
	if !ok || code != "menu:alert_mcp" {
		t.Errorf("mcp/tokens 应命中 menu:alert_mcp，实际 %q (ok=%v)", code, ok)
	}
}

// 预演类接口是 POST 但只读。只读角色必须能跑，否则只能拿生产验证配置。
func TestSimulationRoutesAreReadable(t *testing.T) {
	// ⚠️ 新增预演类接口必须加进这份清单。
	// 漏加的后果不是测试失败，而是它**落进前缀规则**拿到写权限码 ——
	// 前缀规则总能兜住，所以 TestEveryRouteHasPermRule 照样通过，
	// 只有只读角色实际调用时才会 403。模板预览就是这么漏过去的。
	for _, path := range []string{
		"/api/v1/routes/simulate",
		"/api/v1/rules/dryrun",
		"/api/v1/import/preflight",
		"/api/v1/rules/templates/preview",
		"/api/v1/report/preview",
		"/api/v1/explore",
		"/api/v1/msg-templates/preview",
		"/api/v1/sso/test",
	} {
		code, ok := resolvePerm("POST", path)
		if !ok {
			t.Fatalf("%s 没有权限规则", path)
		}
		if strings.HasPrefix(code, "alert:") {
			t.Errorf("%s 是预演接口，不该要写权限，实际 %s", path, code)
		}
	}
}

// 未映射的路由必须解析失败（调用方据此拒绝）。
// 这条挂了就说明 fail-closed 变成了 fail-open，是最危险的回归。
func TestUnmappedRouteIsRejected(t *testing.T) {
	if _, ok := resolvePerm("GET", "/api/v1/something-new"); ok {
		t.Error("未映射的路由被解析成功了，fail-closed 失效")
	}
}

// 跳过鉴权的清单必须精确到路径，不能是前缀。
//
// 这条是真实事故的回归测试：permSkipPrefixes 曾按前缀跳过 "/api/v1/mcp"，
// 结果把 /api/v1/mcp/tokens 一起放行了 —— 只读用户能创建 MCP 令牌，
// 拿到令牌后就绕开了自己在界面上的全部限制。
// 权限矩阵里表现为"读 MCP 令牌 / 写 MCP 令牌"两列全 200。
func TestSkipListDoesNotLeakToSubpaths(t *testing.T) {
	for _, r := range []struct{ method, path string }{
		{"GET", "/api/v1/users"},
		{"POST", "/api/v1/users"},
		{"PUT", "/api/v1/users/:id/role"},
		{"PUT", "/api/v1/users/:id/status"},
		{"PUT", "/api/v1/users/:id/password"},
		{"DELETE", "/api/v1/users/:id"},
		{"GET", "/api/v1/roles"},
		{"GET", "/api/v1/sso"},
		{"PUT", "/api/v1/sso"},
		{"GET", "/api/v1/mcp/tokens"},
		{"POST", "/api/v1/mcp/tokens"},
		{"DELETE", "/api/v1/mcp/tokens/:id"},
	} {
		if permSkipped(r.method, r.path) {
			t.Errorf("%s %s 被跳过鉴权了，只读用户能操作 MCP 令牌", r.method, r.path)
		}
		code, ok := resolvePerm(r.method, r.path)
		if !ok || code == "" {
			t.Errorf("%s %s 必须要求权限码，实际 %q(ok=%v)", r.method, r.path, code, ok)
		}
	}
	// MCP RPC 端点本身仍要跳过：它用 X-MCP-Token，没有登录态
	if !permSkipped("POST", "/api/v1/mcp") {
		t.Error("MCP RPC 端点应跳过会话鉴权，否则接入方全部连不上")
	}
}

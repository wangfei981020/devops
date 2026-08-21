package api

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// nonAdminAllowed 允许挂在**非管理员**路由组上的接口。
//
// 这份清单就是「普通用户能做什么」的完整定义，改它需要想清楚：
// 多放一条进来，等于把一件事对所有登录用户开放。
var nonAdminAllowed = map[string]bool{
	// 自己的事
	`GET /auth/me`:              true,
	`POST /auth/logout`:         true,
	`POST /auth/password`:       true,
	`GET /auth/sessions`:        true, // 只列自己的
	`DELETE /auth/sessions/:id`: true, // handler 里校验归属，只能下线自己的
	`GET /mfa/status`:           true,
	`POST /mfa/enroll`:          true,
	`POST /mfa/enroll/confirm`:  true,
	`POST /mfa/challenge`:       true,

	// 门户：只返回这个人看得见的内容
	`GET /portal/apps`:   true,
	`GET /portal/grants`: true,
	`GET /portal/status`: true,

	// 提申请是每个人的权利；**批**是管理员的事（decide 在 adm 组）
	`POST /access-requests`: true,
	// 看申请列表也是每个人的权利，**但 handler 里强制只返回自己的** ——
	// 非管理员一律按 mine=自己，不看前端传了什么。
	// 放行的前提是那段强制存在；删掉它就等于把全公司的申请记录公开。
	`GET /access-requests`: true,

	// 授权状态只读：过期时整站转只读，普通用户也该能看到为什么突然改不了东西
	`GET /license`: true,
}

var routeRe = regexp.MustCompile(`(?m)^\s*(v1|adm)\.(GET|POST|PUT|DELETE|PATCH)\("([^"]*)"`)

// TestConsoleRoutesAreAdminOnly 锁住「哪些接口不要求管理员」这份清单。
//
// # 为什么要有这个测试
//
// 权限的失效方向是**放行**：新加一个控制台接口时挂到 v1 而不是 adm 上，
// 它从此对所有登录用户开放 —— 不报错、不打日志、界面完全正常，
// 只有真被人调用了才会显形，而那时已经晚了。
//
// 这个测试把「非管理员能调的接口」变成一份要显式维护的清单：
// 挂错组会直接让测试红，而不是等到出事。
//
// ⚠️ 它读的是 router.go 的源码而不是跑起来的路由表。这么做是因为
// gin 的路由树里拿不到"这条挂了哪些中间件"。代价是正则依赖写法，
// 所以下面第二个断言会检查它到底有没有抽到东西 ——
// 抽不到而静默通过，是这类源码级测试最容易出的问题。
func TestConsoleRoutesAreAdminOnly(t *testing.T) {
	src, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("读不到 router.go: %v", err)
	}

	matches := routeRe.FindAllStringSubmatch(string(src), -1)
	if len(matches) < 30 {
		t.Fatalf("只抽到 %d 条路由 —— 正则多半跟不上写法了，"+
			"这时候「测试通过」毫无意义", len(matches))
	}

	var leaked []string
	var sawAdmin int
	for _, m := range matches {
		group, method, path := m[1], m[2], m[3]
		if group == "adm" {
			sawAdmin++
			continue
		}
		key := method + " " + path
		if !nonAdminAllowed[key] {
			leaked = append(leaked, key)
		}
	}
	if sawAdmin == 0 {
		t.Fatal("一条管理员路由都没抽到 —— 正则或分组写法变了")
	}

	if len(leaked) > 0 {
		sort.Strings(leaked)
		t.Errorf("这些接口挂在非管理员组上，任何登录用户都能调：\n  %s\n\n"+
			"要么挪到 adm 组，要么想清楚后加进 nonAdminAllowed —— "+
			"加进去等于把这件事对所有登录用户开放。",
			strings.Join(leaked, "\n  "))
	}
}

// TestNonAdminAllowlistHasNoDeadEntries 清单里不该有已经不存在的路由。
//
// 死条目会让人以为某个接口仍然对普通用户开放（或反之），
// 而清单的全部价值就在于它说的是真的。
func TestNonAdminAllowlistHasNoDeadEntries(t *testing.T) {
	src, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("读不到 router.go: %v", err)
	}
	live := map[string]bool{}
	for _, m := range routeRe.FindAllStringSubmatch(string(src), -1) {
		live[m[2]+" "+m[3]] = true
	}
	for key := range nonAdminAllowed {
		if !live[key] {
			t.Errorf("nonAdminAllowed 里的 %q 在 router.go 里已经不存在了，删掉它", key)
		}
	}
}

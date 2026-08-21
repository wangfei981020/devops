package api

import (
	"os"
	"strings"
	"testing"
)

// 「必须改密」的会话不能换出授权码。
//
// # 为什么值得一个测试
//
// passwordChangeGuard 只挂在业务接口那一组上，而 /oidc/authorize 在根路由上——
// **它天然绕过那道闸门**。实测撞到过：管理员刚重置完口令的账号，
// 直接走 OIDC 就拿到了 code，下游照常放行，而它一次都没改过密。
//
// 危险之处在于 bootstrap 口令是**重置的那个人知道的**，
// 「必须改密」的全部意义就是让它在被本人改掉之前什么都做不了。
//
// 真跑一遍 HTTP 要搭一整套 Deps，成本不划算；这里锁的是那段判断还在。
// 有人重构 oidcAuthorize 时顺手删掉它，这个测试会拦下来。
func TestAuthorizeBlocksMustChange(t *testing.T) {
	src, err := os.ReadFile("oidcp_handlers.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"if id.MustChange {", `"/login?next="`} {
		if !strings.Contains(string(src), want) {
			t.Fatalf("oidcAuthorize 里对「必须改密」的拦截没了（缺 %q）——\n"+
				"这条路绕过 passwordChangeGuard，管理员刚重置过的口令能直接换出下游系统的访问权", want)
		}
	}
}

package api

import (
	"regexp"
	"testing"
)

var clientIDRe = regexp.MustCompile(`^client_[0-9A-Za-z]{22}$`)

// 形状必须稳定：client_id 会被抄进对方系统的配置文件。
// 哪天不小心生成出带 `+` `/` `=` 的值，那些字符在 URL 查询串里要转义，
// 转义过一次的 client_id 和原值比对不上 —— 那种问题查起来极其费劲。
func TestGenerateClientIDShape(t *testing.T) {
	for i := 0; i < 50; i++ {
		id, err := generateClientID()
		if err != nil {
			t.Fatal(err)
		}
		if !clientIDRe.MatchString(id) {
			t.Fatalf("形状不对: %q", id)
		}
	}
}

// 撞了就是两个应用共用一个 client_id —— 后果是登录串台。
// 22 位 base62 约 131 位熵，撞不了；这个测试防的是有人把长度改小。
func TestGenerateClientIDUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 2000; i++ {
		id, err := generateClientID()
		if err != nil {
			t.Fatal(err)
		}
		if seen[id] {
			t.Fatalf("重复: %s", id)
		}
		seen[id] = true
	}
}

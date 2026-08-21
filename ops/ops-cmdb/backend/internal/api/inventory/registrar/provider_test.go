package registrar

import "testing"

// ★ 打错的 provider 必须被拦下。
//
// 不校验的话会**静默建出一条永远不同步的记录**：同步时按 provider 找不到实现，
// 而界面上它显示"已启用"。表现只是"这个注册商下的域名到期日一直不更新"，
// 没有任何报错指向真因。
func TestProvidersWhitelist(t *testing.T) {
	for _, ok := range []string{"godaddy", "namecheap", "aliyun", "cloudflare", "other"} {
		if _, found := Providers[ok]; !found {
			t.Errorf("%q 应当在白名单里", ok)
		}
	}
	// 常见拼写错误：一个字母之差，肉眼极难发现
	for _, bad := range []string{"godady", "GoDaddy", "namechep", "", "gcp"} {
		if _, found := Providers[bad]; found {
			t.Errorf("%q 不该通过白名单", bad)
		}
	}
}

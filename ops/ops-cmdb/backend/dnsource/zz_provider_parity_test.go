package dnsource

import "testing"

// 白名单里的每个厂商，要么有 adapter 实现，要么必须被 SyncSupported 明确标成"不支持"。
//
// 🔴 这个测试钉的是一次真实事故：注册商界面提供 5 个厂商可选，
// 而 NewAdapter 只实现了 godaddy。选了 Cloudflare 会保存成功、显示「已启用」，
// 同步时才失败，用户侧的表现只有"到期日一直不更新"。
//
// 判据：**能选 ≠ 能用**。凡是让用户选的东西，都要能回答"选了会发生什么"。
func TestSyncSupportedMatchesAdapters(t *testing.T) {
	// 与 internal/api/inventory/registrar.Providers 保持一致。
	// ⚠️ 不能 import 那个包（会形成循环依赖），所以在这里重列一份，
	// 两边不一致时本测试会失败——这正是它的目的。
	all := []string{"godaddy", "namecheap", "aliyun", "cloudflare", "other"}

	for _, p := range all {
		if p == "other" {
			// "other" 是明确的"只登记不同步"，不该有 adapter
			if SyncSupported(p) {
				t.Errorf("other 不应被标成支持同步")
			}
			continue
		}
		_, err := NewAdapter(p, map[string]string{}, nil)
		hasAdapter := err == nil
		if hasAdapter != SyncSupported(p) {
			t.Errorf("%s: NewAdapter 可用=%v 但 SyncSupported=%v —— "+
				"两者必须一致，否则界面会把「能选」显示成「能用」",
				p, hasAdapter, SyncSupported(p))
		}
	}
}

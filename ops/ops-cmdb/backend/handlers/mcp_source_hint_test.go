package handlers

import (
	"strings"
	"testing"
)

// 依赖外部数据源的工具，空结果时必须能区分三态。
//
// 守的是 OPSCMDB-031 P1-2：`list_domains` / `list_certificates` 在没接注册商时
// 返回**裸空数组**，和「接了、同步了、确实 0 条」长得一模一样 ——
// 读的人只会得出「我们没有域名」这个完全错误的结论。
//
// ⚠️ 界面上其实一直是对的（全局态势把域名标成「未接入」），
// 说明这个信息后端本来就有，只是没通过 MCP 暴露。
// **两条链路对同一件事给出不同答案**是本项目反复出现的问题。
//
// 这条测试锁的是映射表本身 —— 运行期的三态分支在 hintIfSourceMissing 里，
// 它要连库，这里只保证「该登记的工具一个都没漏、每条都说得清下一步」。
func TestSourceBackedToolsAreComplete(t *testing.T) {
	// 这些工具的数据**完全**来自外部接入，没接就一定是空的。
	// 漏登记一个，它就会在没接数据源时安静地返回 []
	mustHave := []string{
		"list_domains", "list_certificates",
		"list_cdn_zones", "list_cdn_dns", "list_cdn_rules", "list_cdn_certificates",
		"harbor_projects",
	}
	for _, name := range mustHave {
		src, ok := sourceBackedTools[name]
		if !ok {
			t.Errorf("%s 没有登记数据源 —— 没接入时它会返回裸空数组，被读成「确实没有」", name)
			continue
		}
		if strings.TrimSpace(src.table) == "" {
			t.Errorf("%s 没写检查哪张表", name)
		}
		if strings.TrimSpace(src.hint) == "" {
			t.Errorf("%s 没写提示文案", name)
		}
	}
}

// 每条提示都必须点破「空 ≠ 没有」，并给出下一步。
//
// 只说「没有数据源」而不说「这不等于没有域名」，读的人还是会把空当成事实 ——
// 而那正是这条修复要防的那件事。
func TestSourceHintsSayEmptyIsNotAbsence(t *testing.T) {
	for name, src := range sourceBackedTools {
		if !strings.Contains(src.hint, "还没有接入") {
			t.Errorf("%s 的提示没说清是「没接入」：%s", name, src.hint)
		}
		// 至少要有一个：把「空」和「没有」拆开的措辞，或者去哪儿接的指引
		hasWarn := strings.Contains(src.hint, "不等于")
		hasNext := strings.Contains(src.hint, "去「") || strings.Contains(src.hint, "取不到")
		if !hasWarn && !hasNext {
			t.Errorf("%s 的提示既没点破「空≠没有」，也没给下一步：%s", name, src.hint)
		}
	}
}

// 🔴 tools 表里真实存在的工具名才有意义 —— 登记一个拼错的名字，
// 那条提示永远不会触发，而且**没有任何地方会报错**。
func TestSourceBackedToolNamesExist(t *testing.T) {
	known := map[string]bool{}
	for _, tl := range mcpTools {
		known[tl.Name] = true
	}
	for name := range sourceBackedTools {
		if !known[name] {
			t.Errorf("sourceBackedTools 里的 %q 不是一个真实的工具名 —— "+
				"这条提示永远不会触发，而且不会有任何报错", name)
		}
	}
}

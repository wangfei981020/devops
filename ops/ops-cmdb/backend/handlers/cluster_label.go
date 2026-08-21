package handlers

import "strings"

// clusterLabel 集群名的展示口径 —— 与前端 lib/clusterLabel.ts **必须一致**。
//
// 只有部分集群配了中文别名。写成 `COALESCE(display_name, name)` 时，
// 配了别名的显示「开发环境集群」、其余显示技术名，同一个下拉里混着两套命名，
// 用户无从判断「开发环境集群」是哪一个（OPSCMDB-031 NEW-3/5 → 053 → 078）。
//
// 🔴 技术名必须始终可见：它是 kubectl、MCP 工具、PromQL 标签里用的标识。
//
//	别名只是给人看的加注，不能替代它。
//
// ⚠️ 只在**拼单个字符串标签**的地方用（比如 `集群 · ns/name` 这种）。
//
//	返回结构化数据时应当两个字段都给（cluster_name + cluster_display_name），
//	让调用方自己决定怎么排版 —— 拍成一个字符串就再也拆不开了。
func clusterLabel(displayName, name string) string {
	n := strings.TrimSpace(name)
	d := strings.TrimSpace(displayName)
	if d == "" || d == n {
		return n
	}
	// 没有技术名时只能显示别名。这本身是数据问题，
	// 但显示成「（）」或空白更糟 —— 那看起来像渲染坏了。
	if n == "" {
		return d
	}
	return d + "（" + n + "）"
}

package handlers

import (
	"strings"
	"testing"
)

// 工具清单必须按令牌角色过滤，而不是"全都列出来、调的时候再拒"。
//
// 这两种做法对人差别不大，对 AI 是天壤之别：
// AI 拿到 tools/list 之后据此规划排查路径。清单里有 cost_overview，
// 它就会把"查一下成本"排进计划，然后撞一个"没有操作权限"。
// 它无从判断这是权限问题还是系统故障，通常反应是重试，或者向人报"系统异常"。
//
// 这条判据以前只用在授权分档上（EE 工具没买就不列），角色这条轴是漏的。
func TestToolsListScopedByRole(t *testing.T) {
	h := &MCPHandler{}
	// 一个只有资产类菜单权限的角色
	viewer := map[string]bool{
		"menu:cmdb_hosts": true, "menu:cmdb_k8s_clusters": true, "menu:cmdb_k8s_pods": true,
	}

	var visible, hidden []string
	for _, tool := range mcpTools {
		if h.roleCanCall(tool, viewer, "cmdb_viewer") {
			visible = append(visible, tool.Name)
		} else {
			hidden = append(hidden, tool.Name)
		}
	}

	if len(visible) == 0 {
		t.Fatal("该角色一个工具都看不到，过滤过头了")
	}
	if len(hidden) == 0 {
		t.Fatal("没有任何工具被过滤掉——按角色过滤没有生效")
	}

	// 具体到工具：成本类不该出现在这个角色的清单里
	for _, n := range visible {
		if n == "cost_overview" || n == "cost_attribution" || n == "idle_cost" {
			t.Errorf("没有成本菜单权限的角色看到了成本工具 %s", n)
		}
	}
	t.Logf("该角色可见 %d 个工具，隐藏 %d 个", len(visible), len(hidden))
}

// 不受限令牌（role 为空 = 升级前的老令牌）必须看到全部工具。
//
// 过滤写反的典型后果是"所有 AI 接入在升级这一下突然少掉一批工具"，
// 而 AI 不会说"我少了工具"，它会说"查不到"——看起来像数据没采上来。
func TestUnrestrictedTokenSeesEverything(t *testing.T) {
	h := &MCPHandler{}
	got := h.toolSchemas("") // 空角色：permsOfLocalRole 会直接返回 unrestricted，不碰 DB
	if len(got) != len(mcpTools) {
		t.Errorf("不受限令牌只看到 %d 个工具，应为全部 %d 个", len(got), len(mcpTools))
	}
}

// 内部接口返回 4xx/5xx 时，工具结果必须是错误，不能是一次"成功"。
//
// 原来 internalGet 无条件 return body, nil：403「没有操作权限」的 JSON
// 被当成正常结果交给 AI，isError 未置位。AI 多半会把它读成"查到了，是空的"，
// 再向人转述"该项没有数据"——一次权限问题被讲成了一个事实结论。
func TestInternalGetSurfacesHTTPError(t *testing.T) {
	h := &MCPHandler{port: ":1"} // :1 上没有服务，必然失败
	if _, err := h.internalGet("/api/hosts", nil, "test", ""); err == nil {
		t.Fatal("内部调用失败却返回 nil error —— 失败会被当成成功结果交给 AI")
	}
}

// 每个工具的路由都必须有权限规则覆盖。
//
// resolvePerm 返回 !ok 的工具会被 roleCanCall 隐藏（调用时它一样会被拒），
// 但那属于配置漏了，不该靠隐藏糊弄过去。
func TestEveryToolHasPermRule(t *testing.T) {
	var bad []string
	for _, tool := range mcpTools {
		if _, ok := resolvePerm("GET", tool.Path); !ok {
			bad = append(bad, tool.Name+" ("+tool.Path+")")
		}
	}
	if len(bad) > 0 {
		t.Errorf("这些工具的路由没有权限规则覆盖，会被从 tools/list 里静默隐藏：\n  %s",
			strings.Join(bad, "\n  "))
	}
}

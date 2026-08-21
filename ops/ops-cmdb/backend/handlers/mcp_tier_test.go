package handlers

import (
	"reflect"
	"strings"
	"testing"

	"ops-cmdb-backend/internal/license"
)

// 社区版这一档必须**自己就能用**，不是个残废版：
// 资产清单类工具（有哪些机器/集群/Pod/域名）要齐，
// 否则 CE 只是个演示，客户装完第一天就删。
func TestCETierIsUsableOnItsOwn(t *testing.T) {
	must := []string{"list_hosts", "list_clusters", "list_nodes", "list_pods",
		"list_workloads", "list_namespaces", "list_services",
		"list_domains", "list_certificates", "data_freshness"}
	ce := map[string]bool{}
	for _, x := range mcpTools {
		if !x.EE {
			ce[x.Name] = true
		}
	}
	for _, name := range must {
		if !ce[name] {
			t.Errorf("%s 应当在社区版里 —— 没有它，CE 连'有哪些资源'都答不了", name)
		}
	}
}

// 反过来：诊断/成本/日志/安全这些"看得出哪里不对"的能力必须在企业版。
// 划错方向的话，付费档就没有区分度了。
func TestDiagnosticToolsAreEE(t *testing.T) {
	wantEE := []string{"diagnose_pod", "cluster_health", "resource_waste", "idle_cost",
		"cost_overview", "cost_attribution", "query_loki", "query_prometheus",
		"security_audit", "cloud_iam_audit", "gke_upgrade_plan", "pod_logs"}
	byName := map[string]mcpTool{}
	for _, x := range mcpTools {
		byName[x.Name] = x
	}
	for _, name := range wantEE {
		tool, ok := byName[name]
		if !ok {
			t.Fatalf("工具 %s 不见了", name)
		}
		if !tool.EE {
			t.Errorf("%s 应当是企业版能力", name)
		}
	}
}

// 没买全量时，EE 工具**不能出现在 tools/list 里**。
//
// 列出来再拒绝是更糟的做法：AI 会照着清单调，每次撞一个"没有授权"，
// 它会把这理解成系统故障并反复重试，最后给出"系统查不到"的结论。
func TestUnlicensedToolsAreHidden(t *testing.T) {
	mgr := license.NewManager() // 未激活 → 社区版
	h := &MCPHandler{License: mgr}
	if h.fullAllowed() {
		t.Skip("默认档已包含全量工具，跳过（分档改过了就该更新这个测试）")
	}
	schemas := h.toolSchemas("")
	for _, s := range schemas {
		name, _ := s["name"].(string)
		for _, x := range mcpTools {
			if x.Name == name && x.EE {
				t.Errorf("未授权时 %s 不该出现在 tools/list 里", name)
			}
		}
	}
	if len(schemas) == 0 {
		t.Error("社区版工具清单是空的 —— CE 会完全不可用")
	}
}

// 授权加载失败（License 为 nil）时按**能用**处理。
//
// 反过来的话，一次授权读取失败会让所有 AI 接入静默少掉 68 个工具，
// 而 AI 不会说"我少了工具"，它会说"查不到" —— 看起来像数据没采上来，
// 排查方向会全跑偏。
func TestNilLicenseDoesNotSilentlyDowngrade(t *testing.T) {
	h := &MCPHandler{License: nil}
	if !h.fullAllowed() {
		t.Error("License 为 nil 时应当按全量给，不该静默降档")
	}
}

// 令牌明文不能出现在任何返回结构里。
func TestTokenNeverLeavesInList(t *testing.T) {
	var out mcpTokenOut
	// 结构体里只有 Hint（前 8 位），没有任何完整令牌字段。
	// 这条测试的意义是：以后有人图方便加一个 Token 字段时会被这里挡下
	// Tools 是逐令牌能看到的工具**数量**（一个整数），不含任何令牌内容 —— 已确认
	// ExpiresAt / Expired / ExpiringSoon 是有效期（OPSCMDB-031 P1-70），
	// 一个时刻加两个布尔，不含任何令牌内容 —— 已确认
	fields := []string{"ID", "Name", "Hint", "RoleCode", "Enabled", "Unrestricted",
		"Tools", "CreatedBy", "CreatedAt", "LastUsedAt", "LastUsedIP",
		"ExpiresAt", "Expired", "ExpiringSoon"}
	got := structFieldNames(out)
	if strings.Join(got, ",") != strings.Join(fields, ",") {
		t.Errorf("mcpTokenOut 字段变了：%v\n新增字段必须确认不含令牌明文", got)
	}
}

// structFieldNames 反射取字段名。用来锁住"不许往返回结构里加令牌明文"这条。
func structFieldNames(v any) []string {
	rt := reflect.TypeOf(v)
	out := make([]string, 0, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		out = append(out, rt.Field(i).Name)
	}
	return out
}

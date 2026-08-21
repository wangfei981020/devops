package handlers

import (
	"strings"
	"testing"
)

// 只读诊断角色的两条硬约束：
//  1. 拿得到全部 read: 档 —— 否则 diagnose_pod / pod_logs 这些 MCP 工具对它不可见，
//     「只读令牌完成发现→分析→方案」这个产品目标直接失效（OPSCMDB-024）。
//  2. 一个 cmdb:* 动作码都没有 —— 否则"只读"不再是只读。
//
// 这两条任何一条破掉，产品定位就变了，所以钉成测试而不是靠人记得。
func TestDiagnoserRoleIsReadOnlyButCanDiagnose(t *testing.T) {
	var perms []string
	for _, r := range builtinLocalRoles() {
		if r.Code == roleDiagnoser {
			perms = r.Perms
		}
	}
	if len(perms) == 0 {
		t.Fatal("找不到 cmdb_diagnoser 角色")
	}

	var reads, writes []string
	for _, c := range perms {
		switch {
		case strings.HasPrefix(c, "read:"):
			reads = append(reads, c)
		case strings.HasPrefix(c, "cmdb:"):
			writes = append(writes, c)
		}
	}
	if len(reads) != len(allReadCodes) {
		t.Errorf("read: 档应有 %d 个，实际 %d：%v", len(allReadCodes), len(reads), reads)
	}
	if len(writes) != 0 {
		t.Errorf("只读角色不该有任何动作码，实际 %d 个：%v", len(writes), writes)
	}
}

// 诊断类路由必须挂在 read: 档上，不能退回动作码 ——
// 退回去的话只读角色又会调不到（这正是 OPSCMDB-024 的成因）。
func TestDiagnosisRoutesUseReadTier(t *testing.T) {
	for _, path := range []string{"/api/k8s/diagnose", "/api/k8s/pod-logs", "/api/k8s/pod-events"} {
		code, ok := resolvePerm("GET", path)
		if !ok {
			t.Errorf("%s 没有权限规则覆盖", path)
			continue
		}
		if !strings.Contains(code, "read:") {
			t.Errorf("%s 的权限码是 %q，应含 read: 档", path, code)
		}
	}
}

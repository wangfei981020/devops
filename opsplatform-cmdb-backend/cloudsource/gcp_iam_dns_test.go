package cloudsource

import (
	"strings"
	"testing"
)

// allUsers / allAuthenticatedUsers 没有冒号，必须单独识别——
// 走通用分支的话会被拆成 type=unknown，风险判定直接失效。
func TestSplitMemberHandlesPublicPrincipals(t *testing.T) {
	cases := map[string][2]string{
		"allUsers":              {"allUsers", ""},
		"allAuthenticatedUsers": {"allAuthenticatedUsers", ""},
		"user:a@b.com":          {"user", "a@b.com"},
		"serviceAccount:x@y.iam.gserviceaccount.com": {"serviceAccount", "x@y.iam.gserviceaccount.com"},
		"group:team@b.com":                           {"group", "team@b.com"},
		"domain:b.com":                               {"domain", "b.com"},
	}
	for in, want := range cases {
		typ, id := splitMember(in)
		if typ != want[0] || id != want[1] {
			t.Errorf("splitMember(%q) = (%q,%q)，期望 (%q,%q)", in, typ, id, want[0], want[1])
		}
	}
}

// 公开授权是最高级别，且与角色无关——哪怕只给 viewer，也意味着互联网上任何人能读。
func TestJudgeBindingPublicIsCriticalRegardlessOfRole(t *testing.T) {
	for _, role := range []string{"roles/viewer", "roles/owner", "roles/storage.objectViewer"} {
		sev, issue := judgeBinding(role, "allUsers", "")
		if sev != "critical" {
			t.Errorf("allUsers + %s 应为 critical，实际 %s", role, sev)
		}
		if !strings.Contains(issue, "任何人") {
			t.Errorf("提示应说明「互联网上任何人」，实际: %s", issue)
		}
	}
	if sev, _ := judgeBinding("roles/viewer", "allAuthenticatedUsers", ""); sev != "critical" {
		t.Errorf("allAuthenticatedUsers 应为 critical，实际 %s", sev)
	}
}

// owner 判 high、editor 判 medium：editor 在真实项目里极其常见，
// 判 high 会让真正该看的 owner 被淹掉。
func TestJudgeBindingOwnerVsEditor(t *testing.T) {
	if sev, _ := judgeBinding("roles/owner", "user", "a@corp.com"); sev != "high" {
		t.Errorf("roles/owner 应为 high，实际 %s", sev)
	}
	if sev, _ := judgeBinding("roles/editor", "user", "a@corp.com"); sev != "medium" {
		t.Errorf("roles/editor 应为 medium（太常见，判高会淹掉 owner），实际 %s", sev)
	}
	// 可自行提权的角色同样是 high
	for _, r := range []string{"roles/iam.securityAdmin", "roles/resourcemanager.projectIamAdmin"} {
		if sev, _ := judgeBinding(r, "user", "a@corp.com"); sev != "high" {
			t.Errorf("%s 可自行提权，应为 high，实际 %s", r, sev)
		}
	}
}

// 外部个人邮箱：离职或账号失窃时无法随公司目录回收。
func TestJudgeBindingFlagsExternalPersonalMail(t *testing.T) {
	sev, issue := judgeBinding("roles/viewer", "user", "someone@gmail.com")
	if sev != "medium" {
		t.Errorf("外部个人邮箱应为 medium，实际 %s", sev)
	}
	if !strings.Contains(issue, "回收") {
		t.Errorf("提示应点出无法统一回收，实际: %s", issue)
	}
	// 公司域名邮箱是正常的，不该报
	if sev, _ := judgeBinding("roles/viewer", "user", "someone@corp.com"); sev != "" {
		t.Errorf("公司域名 + viewer 不该报，实际 %s", sev)
	}
	// 服务账号即使域名像外部也不按个人邮箱判
	if sev, _ := judgeBinding("roles/viewer", "serviceAccount", "svc@proj.iam.gserviceaccount.com"); sev != "" {
		t.Errorf("普通服务账号 + viewer 不该报，实际 %s", sev)
	}
}

// 常规只读角色不该产生噪音——绝大多数绑定都是这类。
func TestJudgeBindingQuietOnNormalRoles(t *testing.T) {
	for _, r := range []string{"roles/viewer", "roles/logging.viewer", "roles/monitoring.viewer"} {
		if sev, _ := judgeBinding(r, "group", "team@corp.com"); sev != "" {
			t.Errorf("只读角色 %s 不该报，实际 %s", r, sev)
		}
	}
}

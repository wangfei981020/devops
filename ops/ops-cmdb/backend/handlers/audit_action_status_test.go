package handlers

import "testing"

// 🔴 失败的动作必须记成 fail。
//
// audit_logs.status 的列默认值是 'success'，而 WriteAuditAs 原来不写这一列 ——
// 于是「auth.login.failed · 用户名或密码错误」这一行在审计页上是**绿色的 success**。
// 代价不是难看：暴力破解在审计上看起来是一串正常登录。
func TestAuditStatusOfAction(t *testing.T) {
	cases := map[string]string{
		// 实际存在的七个失败类动作，一个都不能漏
		"auth.login.failed":  "fail",
		"auth.portal.failed": "fail",
		"auth.portal.denied": "fail",
		// 成功类
		"auth.login.success":  "success",
		"auth.portal.success": "success",
		"domain.create":       "success",
		// 新增的失败类动作应当**自动**归位 —— 这正是不让调用点各自传 status 的理由
		"cert.apply.failed": "fail",
		"sync.error":        "fail",
	}
	for action, want := range cases {
		if got := auditStatusOfAction(action); got != want {
			t.Errorf("auditStatusOfAction(%q) = %q, 期望 %q", action, got, want)
		}
	}
}

// 与 HTTP 状态码那条路径的取值域必须一致：
// 两处产出不同的枚举值时，审计页「结果」筛选会漏掉其中一批 —— 而且漏的是失败那批。
func TestAuditStatusVocabularyMatches(t *testing.T) {
	allowed := map[string]bool{"success": true, "fail": true, "accepted": true}
	for _, a := range []string{"x.failed", "x.denied", "x.error", "x.success", "x.create"} {
		if !allowed[auditStatusOfAction(a)] {
			t.Errorf("auditStatusOfAction(%q) 产出了 auditStatusOf(code) 不认识的值: %q", a, auditStatusOfAction(a))
		}
	}
	for _, code := range []int{200, 201, 202, 400, 401, 403, 500} {
		if !allowed[auditStatusOf(code)] {
			t.Errorf("auditStatusOf(%d) 产出了未登记的值: %q", code, auditStatusOf(code))
		}
	}
}

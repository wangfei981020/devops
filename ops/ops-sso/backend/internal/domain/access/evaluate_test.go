package access

import "testing"

// 这组用例就是授权语义的**规格说明书**。
// 改判定顺序时先改这里、看它红，再改实现 —— 反过来做等于没有规格。

const (
	appJira    = 101
	appHarbor  = 102
	grpDevTool = 11 // 应用分组：研发工具（Jira + Harbor）
	grpOps     = 12 // 应用分组：生产运维工具

	uZhangsan = 1001
	uWaibao   = 1002

	gDev      = 201 // 用户组：研发
	gWaibao   = 202 // 用户组：外包
	rEngineer = 301 // 角色：工程师

	dCenter = 401 // 部门：研发中心   depth 1
	dBack   = 402 // 部门：后端组     depth 2
)

func zhangsan() Subject {
	return Subject{
		UserID:    uZhangsan,
		GroupIDs:  map[int64]bool{gDev: true},
		RoleIDs:   map[int64]bool{rEngineer: true},
		DeptDepth: map[int64]int{dCenter: 1, dBack: 2},
	}
}

func waibao() Subject {
	return Subject{
		UserID:   uWaibao,
		GroupIDs: map[int64]bool{gWaibao: true},
	}
}

func jira() App { return App{ID: appJira, GroupIDs: []int64{grpDevTool}} }

func rule(id int64, sc Scope, scID int64, st SubjectType, sID int64, ef Effect) Rule {
	return Rule{ID: id, Scope: sc, ScopeID: scID, SubjectType: st, SubjectID: sID, Effect: ef}
}

func TestDefaultDeny(t *testing.T) {
	// 没有任何规则 = 谁都进不去。这条错了就是全公司系统裸奔。
	d := Evaluate(zhangsan(), jira(), nil)
	if d.Allowed() {
		t.Fatalf("无规则时必须拒绝，得到 %+v", d)
	}
	if d.Reason != ReasonDefaultDeny {
		t.Fatalf("原因应为 default_deny，得到 %s", d.Reason)
	}
}

func TestUnauthenticatedDenied(t *testing.T) {
	// 认证中间件漏了 → 必须拒绝，不能因为「没有 deny 规则」就放行
	d := Evaluate(Subject{}, jira(), []Rule{rule(1, ScopeGlobal, 0, SubjectPublic, 0, Allow)})
	if d.Allowed() || d.Reason != ReasonNoSubject {
		t.Fatalf("未认证必须拒绝，得到 %+v", d)
	}
}

func TestGlobalPublicAllow(t *testing.T) {
	d := Evaluate(zhangsan(), jira(), []Rule{rule(1, ScopeGlobal, 0, SubjectPublic, 0, Allow)})
	if !d.Allowed() {
		t.Fatalf("全局 public allow 应放行，得到 %+v", d)
	}
}

// 「单独配置优先于全局」的准确含义：同一个主体，在更具体的作用域上重新配置。
func TestAppOverridesGlobal_SameSubject(t *testing.T) {
	rules := []Rule{
		rule(1, ScopeGlobal, 0, SubjectGroup, gDev, Deny),
		rule(2, ScopeApp, appJira, SubjectGroup, gDev, Allow),
	}
	d := Evaluate(zhangsan(), jira(), rules)
	if !d.Allowed() {
		t.Fatalf("应用级应盖过全局，得到 %+v", d)
	}
	if d.Rule.ID != 2 {
		t.Fatalf("应由规则 2 决定，得到 %d", d.Rule.ID)
	}
}

// 分组级也能盖全局，且被应用级盖。
func TestScopeLadder(t *testing.T) {
	base := rule(1, ScopeGlobal, 0, SubjectGroup, gDev, Deny)
	grp := rule(2, ScopeGroup, grpDevTool, SubjectGroup, gDev, Allow)
	app := rule(3, ScopeApp, appJira, SubjectGroup, gDev, Deny)

	if d := Evaluate(zhangsan(), jira(), []Rule{base, grp}); !d.Allowed() {
		t.Fatalf("分组级应盖过全局，得到 %+v", d)
	}
	if d := Evaluate(zhangsan(), jira(), []Rule{base, grp, app}); d.Allowed() {
		t.Fatalf("应用级应盖过分组级，得到 %+v", d)
	}
}

// 主体具体度优先于作用域具体度：点名某人的规则不该被按组授权盖掉。
func TestSubjectBeatsScope(t *testing.T) {
	rules := []Rule{
		rule(1, ScopeGlobal, 0, SubjectUser, uZhangsan, Deny), // 点名禁张三
		rule(2, ScopeApp, appJira, SubjectGroup, gDev, Allow), // 应用级给研发组
	}
	d := Evaluate(zhangsan(), jira(), rules)
	if d.Allowed() {
		t.Fatalf("点名 deny 不该被按组 allow 盖掉，得到 %+v", d)
	}
	if d.Rule.ID != 1 {
		t.Fatalf("应由规则 1 决定，得到 %d", d.Rule.ID)
	}
}

// 全员放行 + 对某一类人的收紧
func TestNarrowerSubjectWins(t *testing.T) {
	rules := []Rule{
		rule(1, ScopeGlobal, 0, SubjectPublic, 0, Allow),
		rule(2, ScopeApp, appJira, SubjectGroup, gWaibao, Deny),
	}
	if d := Evaluate(waibao(), jira(), rules); d.Allowed() {
		t.Fatalf("外包组应被拒，得到 %+v", d)
	}
	if d := Evaluate(zhangsan(), jira(), rules); !d.Allowed() {
		t.Fatalf("非外包应放行，得到 %+v", d)
	}
}

// 强制拒绝：安全负责人的硬保证，任何更具体的 allow 都盖不住
func TestEnforcedDenyBeatsEverything(t *testing.T) {
	enf := rule(1, ScopeGlobal, 0, SubjectGroup, gWaibao, Deny)
	enf.Enforced = true
	rules := []Rule{
		enf,
		rule(2, ScopeApp, appJira, SubjectUser, uWaibao, Allow), // 最具体的例外
	}
	d := Evaluate(waibao(), jira(), rules)
	if d.Allowed() {
		t.Fatalf("强制拒绝必须赢，得到 %+v", d)
	}
	if d.Reason != ReasonEnforcedDeny {
		t.Fatalf("原因应为 enforced_deny，得到 %s", d.Reason)
	}
	// 赢家必须排在轨迹第一条，否则界面上看起来像判错了
	if d.Trace[0].Rule.ID != 1 || d.Trace[0].Outcome != OutcomeEnforced {
		t.Fatalf("强制拒绝应排在轨迹首位，得到 %+v", d.Trace)
	}
}

// 不做「强制放行」——那是不可关闭的后门
func TestEnforcedAllowRejected(t *testing.T) {
	r := rule(1, ScopeGlobal, 0, SubjectPublic, 0, Allow)
	r.Enforced = true
	if err := r.Validate(); err == nil {
		t.Fatal("强制放行必须在校验阶段被拒")
	}
}

// 同层多组归属：一个人同时在被 allow 和被 deny 的组里 → 拒绝
func TestSameRankDenyWins(t *testing.T) {
	sub := zhangsan()
	sub.GroupIDs[gWaibao] = true
	rules := []Rule{
		rule(1, ScopeApp, appJira, SubjectGroup, gDev, Allow),
		rule(2, ScopeApp, appJira, SubjectGroup, gWaibao, Deny),
	}
	d := Evaluate(sub, jira(), rules)
	if d.Allowed() {
		t.Fatalf("同等具体度下 deny 应赢，得到 %+v", d)
	}
	// 输掉的那条要标明是输给了 deny，而不是「被更具体的盖住」——
	// 这两种说法在界面上会让人做出不同的修复动作
	var found bool
	for _, s := range d.Trace {
		if s.Rule.ID == 1 && s.Outcome == OutcomeLostToDeny {
			found = true
		}
	}
	if !found {
		t.Fatalf("被 deny 压过的 allow 应标 lost_to_deny，得到 %+v", d.Trace)
	}
}

// 部门按子树命中：规则挂在研发中心，人在后端组
func TestDeptSubtreeMatch(t *testing.T) {
	d := Evaluate(zhangsan(), jira(), []Rule{rule(1, ScopeGlobal, 0, SubjectDept, dCenter, Allow)})
	if !d.Allowed() {
		t.Fatalf("上级部门的规则应命中下级的人，得到 %+v", d)
	}
}

// 部门越深越具体：后端组 deny 盖过研发中心 allow
func TestDeeperDeptWins(t *testing.T) {
	rules := []Rule{
		rule(1, ScopeGlobal, 0, SubjectDept, dCenter, Allow),
		rule(2, ScopeGlobal, 0, SubjectDept, dBack, Deny),
	}
	d := Evaluate(zhangsan(), jira(), rules)
	if d.Allowed() {
		t.Fatalf("更深的部门规则应优先，得到 %+v", d)
	}
	if d.Rule.ID != 2 {
		t.Fatalf("应由规则 2 决定，得到 %d", d.Rule.ID)
	}
}

// 用户组比角色具体
func TestGroupBeatsRole(t *testing.T) {
	rules := []Rule{
		rule(1, ScopeGlobal, 0, SubjectRole, rEngineer, Allow),
		rule(2, ScopeGlobal, 0, SubjectGroup, gDev, Deny),
	}
	if d := Evaluate(zhangsan(), jira(), rules); d.Allowed() {
		t.Fatalf("用户组应比角色具体，得到 %+v", d)
	}
}

// 分组作用域只作用于属于该分组的应用
func TestGroupScopeDoesNotLeak(t *testing.T) {
	rules := []Rule{rule(1, ScopeGroup, grpOps, SubjectPublic, 0, Allow)}
	// Jira 属于 grpDevTool，不属于 grpOps
	if d := Evaluate(zhangsan(), jira(), rules); d.Allowed() {
		t.Fatalf("别的分组的规则不该作用到本应用，得到 %+v", d)
	}
	harbor := App{ID: appHarbor, GroupIDs: []int64{grpDevTool, grpOps}}
	if d := Evaluate(zhangsan(), harbor, rules); !d.Allowed() {
		t.Fatalf("多归属应用应命中任一所属分组的规则，得到 %+v", d)
	}
}

// 应用作用域不串台
func TestAppScopeDoesNotLeak(t *testing.T) {
	rules := []Rule{rule(1, ScopeApp, appHarbor, SubjectPublic, 0, Allow)}
	if d := Evaluate(zhangsan(), jira(), rules); d.Allowed() {
		t.Fatalf("别的应用的规则不该作用到本应用，得到 %+v", d)
	}
}

// 结果必须稳定可复现：同级规则的胜负不能取决于数据库返回顺序
func TestStableAcrossInputOrder(t *testing.T) {
	a := rule(7, ScopeApp, appJira, SubjectGroup, gDev, Allow)
	b := rule(3, ScopeApp, appJira, SubjectRole, rEngineer, Deny)
	d1 := Evaluate(zhangsan(), jira(), []Rule{a, b})
	d2 := Evaluate(zhangsan(), jira(), []Rule{b, a})
	if d1.Effect != d2.Effect || d1.Rule.ID != d2.Rule.ID {
		t.Fatalf("输入顺序改变了结论：%+v vs %+v", d1, d2)
	}
}

func TestVisibleApps(t *testing.T) {
	apps := []App{jira(), {ID: appHarbor, GroupIDs: []int64{grpOps}}}
	rules := []Rule{rule(1, ScopeGroup, grpDevTool, SubjectGroup, gDev, Allow)}
	got := VisibleApps(zhangsan(), apps, rules)
	if !got[appJira].Allowed() {
		t.Fatal("Jira 应可见")
	}
	if got[appHarbor].Allowed() {
		t.Fatal("Harbor 不该可见")
	}
}

func TestRuleValidate(t *testing.T) {
	cases := []struct {
		name string
		r    Rule
		ok   bool
	}{
		{"合法-全局public", rule(1, ScopeGlobal, 0, SubjectPublic, 0, Allow), true},
		{"全局带scope_id", rule(1, ScopeGlobal, 9, SubjectPublic, 0, Allow), false},
		{"分组缺scope_id", rule(1, ScopeGroup, 0, SubjectPublic, 0, Allow), false},
		{"public带subject_id", rule(1, ScopeGlobal, 0, SubjectPublic, 5, Allow), false},
		{"user缺subject_id", rule(1, ScopeGlobal, 0, SubjectUser, 0, Allow), false},
		{"未知effect", Rule{ID: 1, Scope: ScopeGlobal, SubjectType: SubjectPublic, Effect: "maybe"}, false},
		{"未知作用域", Rule{ID: 1, Scope: "cluster", SubjectType: SubjectPublic, Effect: Allow}, false},
	}
	for _, c := range cases {
		err := c.r.Validate()
		if (err == nil) != c.ok {
			t.Errorf("%s: 期望 ok=%v，得到 err=%v", c.name, c.ok, err)
		}
	}
}

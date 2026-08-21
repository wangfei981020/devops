package approval

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func req() Request {
	return Request{
		TenantID: 1, RequesterID: 1001, AppID: 2,
		Reason: "排查订单重复扣款 BUG-9931", Duration: 4 * time.Hour,
		Status: StatusPending, CreatedAt: time.Unix(1_800_000_000, 0),
	}
}

func TestValidate(t *testing.T) {
	if err := req().Validate(); err != nil {
		t.Fatalf("正常申请应通过：%v", err)
	}

	cases := []struct {
		name string
		mut  func(*Request)
	}{
		{"没指定应用", func(r *Request) { r.AppID = 0 }},
		{"理由太短", func(r *Request) { r.Reason = "急" }},
		{"理由是空白", func(r *Request) { r.Reason = "      " }},
		{"没时长", func(r *Request) { r.Duration = 0 }},
		{"超过 24 小时", func(r *Request) { r.Duration = 25 * time.Hour }},
		{"作用域不是路径", func(r *Request) { r.Scope = "projects" }},
	}
	for _, c := range cases {
		r := req()
		c.mut(&r)
		if err := r.Validate(); err == nil {
			t.Errorf("%s 应被拒", c.name)
		} else if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("%s 的错误类型不对：%v", c.name, err)
		}
	}
}

// ★ 理由必须写清楚 —— 它是审批人唯一能看的东西，
// 也是三个月后复核时唯一能回答"当时为什么给他开"的东西。
//
// 这条用例抓出过一个真 bug：原来用 len() 数字节，于是"见工单"（3 字 9 字节）
// 能过，而 "abcd"（4 字 4 字节）被拒 —— 同样敷衍，判定却相反。
func TestReasonCannotBeThrowaway(t *testing.T) {
	tooShort := []string{"1", "aa", "急", "见工单", "abcd", "紧急处理"}
	for _, reason := range tooShort {
		r := req()
		r.Reason = reason
		if err := r.Validate(); err == nil {
			t.Errorf("敷衍的理由 %q（%d 个字）应被拒", reason, len([]rune(reason)))
		}
	}

	// 中英文都按"字符数"算，长度判定一致
	ok := []string{"排查订单重复扣款", "debug payment issue", "查生产日志一下"}
	for _, reason := range ok {
		r := req()
		r.Reason = reason
		if err := r.Validate(); err != nil {
			t.Errorf("正常理由 %q 不该被拒：%v", reason, err)
		}
	}
}

func TestApproveSetsWindow(t *testing.T) {
	r := req()
	now := time.Unix(1_800_000_000, 0)

	if err := r.Decide(2002, true, "同意", now); err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusApproved {
		t.Fatalf("应为 approved，得到 %s", r.Status)
	}
	if r.ExpiresAt == nil || !r.ExpiresAt.Equal(now.Add(4*time.Hour)) {
		t.Fatalf("到期时间应为批准时刻 + 时长，得到 %v", r.ExpiresAt)
	}

	// 到期前 1 秒仍生效，到点立刻失效 —— "4 小时"要是能拖到 4 小时零 1 分，这个数字就没意义
	if !r.Active(now.Add(4*time.Hour - time.Second)) {
		t.Error("到期前必须仍生效")
	}
	if r.Active(now.Add(4 * time.Hour)) {
		t.Error("到点必须立刻失效")
	}
}

// ★ 自己批自己：SoD 的最基本形态，写死在代码里而不是靠配置
func TestCannotApproveOwnRequest(t *testing.T) {
	r := req()
	if err := r.Decide(r.RequesterID, true, "", time.Now()); !errors.Is(err, ErrSelfApproval) {
		t.Fatalf("自己批自己必须被拒，得到 %v", err)
	}
	if r.Status != StatusPending {
		t.Error("被拒后状态不该被改动")
	}
}

func TestCannotDecideTwice(t *testing.T) {
	r := req()
	now := time.Now()
	if err := r.Decide(2002, true, "", now); err != nil {
		t.Fatal(err)
	}
	if err := r.Decide(2003, false, "", now); !errors.Is(err, ErrNotPending) {
		t.Fatalf("重复审批必须被拒，得到 %v", err)
	}
	if r.Status != StatusApproved {
		t.Error("第二次审批不该改变已有结论")
	}
}

func TestRejectDoesNotGrant(t *testing.T) {
	r := req()
	now := time.Now()
	if err := r.Decide(2002, false, "风险太大", now); err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusRejected {
		t.Fatalf("应为 rejected，得到 %s", r.Status)
	}
	if r.ExpiresAt != nil || r.Active(now) {
		t.Error("被拒的申请绝不能有生效窗口")
	}
}

func TestRevokedIsNotActive(t *testing.T) {
	r := req()
	now := time.Unix(1_800_000_000, 0)
	_ = r.Decide(2002, true, "", now)
	rev := now.Add(time.Hour)
	r.RevokedAt = &rev

	if r.Active(now.Add(2 * time.Hour)) {
		t.Fatal("已回收的提权不该还生效 —— 权限收回了票还能用，「临时」就是假的")
	}
}

// ── 作用域 ──

// 与 mfa.Ticket.Covers 必须是同一套语义：前缀在路径分隔处对齐。
// 两处规则不一致的话，"我明明申请过"会变成一个查不清的投诉。
func TestScopeAlignsOnSeparator(t *testing.T) {
	r := req()
	r.Scope = "/api/v2/projects"
	cases := map[string]bool{
		"/api/v2/projects":        true,
		"/api/v2/projects/pay":    true,
		"/api/v2/projects-secret": false,
		"/api/v2/projectsX":       false,
		"/api/v2/repositories":    false,
	}
	for p, want := range cases {
		if got := r.Covers(p); got != want {
			t.Errorf("Covers(%q) = %v，期望 %v", p, got, want)
		}
	}
	r.Scope = ""
	if !r.Covers("/anything") {
		t.Error("空作用域应覆盖整个应用")
	}
}

// ── 职责分离 ──

func sodRules() []SoDRule {
	return []SoDRule{
		{ID: 1, Code: "SoD-03", Name: "发布执行与发布审批互斥", Enabled: true,
			CapabilityA: "release_execute", CapabilityB: "release_approve"},
		{ID: 2, Code: "SoD-07", Name: "付款发起与付款复核互斥", Enabled: true,
			CapabilityA: "payment_create", CapabilityB: "payment_review"},
		{ID: 3, Code: "SoD-09", Name: "已停用的规则", Enabled: false,
			CapabilityA: "a", CapabilityB: "b"},
	}
}

// ★ 命中即拒绝，且要说清「他已经通过什么持有了冲突的另一半」
func TestSoDBlocksAndExplains(t *testing.T) {
	held := map[string]string{"release_approve": "变更 CR-2210 的审批人"}
	c := CheckSoD(sodRules(), held, []string{"release_execute"})

	if !c.Blocked {
		t.Fatal("发布执行 + 发布审批必须被拦")
	}
	if c.Rule.Code != "SoD-03" {
		t.Fatalf("应命中 SoD-03，得到 %s", c.Rule.Code)
	}
	if c.HeldVia == "" {
		t.Fatal("必须说清他通过什么持有了冲突的另一半 —— 只说「不行」，申请人不知道怎么才能行")
	}
}

// 反方向同样要拦（A→B 与 B→A 是同一条规则）
func TestSoDIsSymmetric(t *testing.T) {
	held := map[string]string{"release_execute": "常设授权"}
	if c := CheckSoD(sodRules(), held, []string{"release_approve"}); !c.Blocked {
		t.Fatal("反方向也必须被拦")
	}
}

func TestSoDPassesWhenNoConflict(t *testing.T) {
	held := map[string]string{"payment_review": "财务岗"}
	if c := CheckSoD(sodRules(), held, []string{"release_execute"}); c.Blocked {
		t.Fatalf("不相关的能力不该被拦：%+v", c)
	}
	// 什么都没持有
	if c := CheckSoD(sodRules(), map[string]string{}, []string{"release_execute"}); c.Blocked {
		t.Fatal("没有冲突的另一半时不该被拦")
	}
}

func TestDisabledSoDRuleDoesNotBlock(t *testing.T) {
	held := map[string]string{"b": "x"}
	if c := CheckSoD(sodRules(), held, []string{"a"}); c.Blocked {
		t.Fatal("已停用的规则不该生效")
	}
}

func TestBlockRecordsWhy(t *testing.T) {
	r := req()
	c := CheckSoD(sodRules(), map[string]string{"release_approve": "CR-2210 审批人"},
		[]string{"release_execute"})
	r.Block(c, time.Now())

	if r.Status != StatusBlocked {
		t.Fatalf("应为 blocked（与人工 rejected 分开），得到 %s", r.Status)
	}
	if r.BlockedRule != "SoD-03" {
		t.Error("必须记下命中的规则编号 —— 事后审计要能回答「这条为什么没批」")
	}
	if !strings.Contains(r.BlockedNote, "CR-2210") {
		t.Errorf("说明里应包含他是怎么持有另一半的，得到 %q", r.BlockedNote)
	}
}

// ── 其他 ──

func TestNeedsTwoStep(t *testing.T) {
	r := req()
	r.Duration = 4 * time.Hour
	if r.NeedsTwoStep() {
		t.Error("4 小时不该需要二级审批")
	}
	r.Duration = 12 * time.Hour
	if !r.NeedsTwoStep() {
		t.Error("超过 8 小时应需要二级审批")
	}
}

func TestRiskOf(t *testing.T) {
	if got := RiskOf("PROD", "", 2*time.Hour); got != RiskHigh {
		t.Errorf("生产 + 整个应用应为高风险，得到 %s", got)
	}
	if got := RiskOf("PROD", "/api/v2/projects", time.Hour); got != RiskMedium {
		t.Errorf("生产 + 限定路径应为中风险，得到 %s", got)
	}
	if got := RiskOf("UAT", "/api/v2/x", time.Hour); got != RiskLow {
		t.Errorf("UAT + 限定路径应为低风险，得到 %s", got)
	}
}

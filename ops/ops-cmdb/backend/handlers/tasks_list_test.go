package handlers

import "testing"

func b(v bool) *bool { return &v }

// ★ 从没跑过的任务不能显示成"正常"。
//
// last_ok 是带默认值的列：任务从没执行过时它可能是 1。
// 只信它的话，一个从没被调度到的任务在界面上是绿的，
// 而"上次执行"写着"从没跑过" —— 同一行自相矛盾，且偏向"没问题"。
func TestTaskStateNeverRun(t *testing.T) {
	never := taskOut{Enabled: true, LastRunAt: "", LastOK: nil}
	if got := taskState(never); got != "never" {
		t.Errorf("从没跑过的任务状态 = %q，期望 never", got)
	}
	// 停用优先于一切：停用的任务本来就不该跑
	off := taskOut{Enabled: false, LastRunAt: "", LastOK: nil}
	if got := taskState(off); got != "disabled" {
		t.Errorf("停用任务状态 = %q，期望 disabled", got)
	}
	// 跑过且成功但早该再跑了 → overdue，不是 ok
	stalled := taskOut{Enabled: true, LastRunAt: "2026-01-01T00:00:00Z", LastOK: b(true), Overdue: true}
	if got := taskState(stalled); got != "overdue" {
		t.Errorf("停摆任务状态 = %q，期望 overdue", got)
	}
	ok := taskOut{Enabled: true, LastRunAt: "2026-01-01T00:00:00Z", LastOK: b(true)}
	if got := taskState(ok); got != "ok" {
		t.Errorf("正常任务状态 = %q，期望 ok", got)
	}
}

package handlers

import "errors"

// 任务执行记录（task_run_logs.status）的状态值。
//
//	## 为什么要收成常量
//
//	这套值原来是各写各的字面量，于是**同一个成功在两条路径上是两个词**：
//	  定时任务（scheduler.go）    → ok   / fail
//	  手动触发（finishManualRunLog）→ success / failed   ← 两个都不在规范里
//
//	前端「执行记录」页的筛选和标签用的是 ok/partial/fail/... 这套，
//	于是筛「✅ 成功」时**手动触发的记录一条都看不到**——生产上 host_sync
//	近 7 天 13 条里有 6 条手动记录被整个筛没了（CMDB-20260806-002）。
//	而"我刚手动跑的那次结果如何"恰恰是排障时最常看的东西。
//
//	失败态同样分裂（fail vs failed），只是筛「成功」时先暴露出来而已；
//	如果哪天有地方按 `status='fail'` 统计失败率，手动执行的失败会被系统性漏掉。
//
//	⚠️ 这些值是**前后端共享的契约**（前端 TaskRuns.vue 的 stLabel/stType 和
//	筛选下拉逐字对应）。新增状态必须两边一起加，否则新状态在界面上
//	会掉进兜底分支显示成原始英文。
const (
	taskStatusRunning     = "running"     // 进行中
	taskStatusOK          = "ok"          // 全部成功
	taskStatusPartial     = "partial"     // 部分成功（有失败明细但整体可用）
	taskStatusFail        = "fail"        // 失败
	taskStatusTimeout     = "timeout"     // 超时中止
	taskStatusCancelled   = "cancelled"   // 人工取消
	taskStatusInterrupted = "interrupted" // 进程重启/卡死自愈导致的中断
	// taskStatusSkipped 跑完了，但**没能得出任何结论**（依赖没配、没有可检查的对象）。
	//
	// ⚠️ 这一档必须独立存在，不能并进 ok 也不能并进 fail：
	//
	//	并进 ok  → 「未配置 Prometheus，跳过磁盘用量采集」显示成绿色的「正常」，
	//	           而它一次都没采到过数据。这是 OPSCMDB-006 的根因。
	//	并进 fail → 没配依赖不是故障，天天报红会让人对这个任务的告警脱敏。
	//
	// 界面上应显示成灰色的「已跳过」，并把原因原样带出来 —— 人看一眼就知道
	// 「要让它真正跑起来，我得先去配什么」。
	taskStatusSkipped = "skipped"
)

// taskStatusValues 全部合法状态。给测试用，也给以后想做校验的地方用。
var taskStatusValues = []string{
	taskStatusRunning, taskStatusOK, taskStatusPartial, taskStatusFail,
	taskStatusTimeout, taskStatusCancelled, taskStatusInterrupted, taskStatusSkipped,
}

// ErrTaskSkipped 任务用它声明「我跑完了，但没能得出任何结论」。
//
// # 为什么用哨兵错误而不是加一个返回值
//
// taskFn 的签名是 (result string, failures []TaskFailure, ok bool)。
// bool 只能表达两态，而我们需要三态：成功 / 跳过 / 失败。
//
// 加第四个返回值要改全部 20 多个任务的签名；用哨兵错误只需要
// 想声明跳过的那几个任务改一行 —— 而且**忘了用的任务行为完全不变**，
// 这对一次涉及全部定时任务的改动很重要。
//
// 用法：
//
//	if promSource == nil {
//	    return "未配置 Prometheus 数据源", []TaskFailure{{Reason: ErrTaskSkipped.Error()}}, true
//	}
//
// ⚠️ 判据放在 failures 里而不是 result 字符串里：
// 靠匹配中文字符串判状态，改一个字文案就会让状态判定失效，而且不报错。
var ErrTaskSkipped = errors.New("__task_skipped__")

// isSkipped 这批 failures 是不是「跳过」的声明。
func isSkipped(failures []TaskFailure) bool {
	return len(failures) == 1 && failures[0].Reason == ErrTaskSkipped.Error()
}

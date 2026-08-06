package handlers

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
)

// taskStatusValues 全部合法状态。给测试用，也给以后想做校验的地方用。
var taskStatusValues = []string{
	taskStatusRunning, taskStatusOK, taskStatusPartial, taskStatusFail,
	taskStatusTimeout, taskStatusCancelled, taskStatusInterrupted,
}

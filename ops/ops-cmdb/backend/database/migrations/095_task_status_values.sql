-- 订正手动触发任务写歪的状态值。
--
-- 手动触发路径（finishManualRunLog）写的是 success/failed，
-- 而定时任务路径和前端「执行记录」页的筛选、标签用的都是 ok/fail。
-- 于是筛「✅ 成功」时**手动触发的记录一条都看不到**——生产上 host_sync
-- 近 7 天 13 条里有 6 条手动记录被整个筛没了（CMDB-20260806-002）。
-- 而"我刚手动跑的那次结果如何"恰恰是排障时最常看的东西。
--
-- 失败态同样分裂（failed vs fail），只是筛「成功」时先暴露出来而已。
--
-- 代码侧已收成常量（handlers/task_status.go），这里订正存量。
-- 只动这两个歪值，其余状态不碰；重复执行安全（第二次匹配不到行）。
UPDATE task_run_logs SET status = 'ok'   WHERE status = 'success';
UPDATE task_run_logs SET status = 'fail' WHERE status = 'failed';

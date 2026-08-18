-- 任务执行进度：运行中(status='running')的记录用 progress 存 "已处理/总数"，前端实时展示进度条
ALTER TABLE task_run_logs ADD COLUMN progress VARCHAR(32) NOT NULL DEFAULT '';

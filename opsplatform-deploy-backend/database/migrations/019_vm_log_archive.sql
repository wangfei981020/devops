-- Migration 019: VM 部署日志归档
--
-- VM 部署只有 1 份日志（ansible-playbook 的 stdout+stderr），不像 K8s 那样每个 pod 一份。
-- 所以直接在 deployment 行上加 2 列就够，不开新表。
--
-- 任务进终态（success/failed/canceled）时由 pollVmTask 异步上传 MinIO，路径 vm-logs/{depID}.log
-- 跟 K8s pod 日志同 bucket 同 lifecycle，未配 MinIO 时归档跳过（不影响发布主流程）

ALTER TABLE deployment ADD COLUMN vm_log_object_key VARCHAR(512) NULL AFTER agent_task_id;
ALTER TABLE deployment ADD COLUMN vm_log_size       INT          NOT NULL DEFAULT 0 AFTER vm_log_object_key;

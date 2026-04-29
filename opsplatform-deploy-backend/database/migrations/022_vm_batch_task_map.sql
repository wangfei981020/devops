-- Migration 022: VM 批量部署支持
--
-- 一次提交可包含多个 service（每个独立版本号），后端给每个 service 创建一个 agent task。
-- 一行 deployment 仍然代表一次"用户提交"动作，但底下挂 N 个 agent_task_id。
--
-- vm_task_map 结构（JSON 数组）：
--   [
--     {"service":"G01_op_office","version":"011994...","task_id":"abc-123","status":"pending","error_msg":""},
--     {"service":"G01_anchor_web","version":"abc...",   "task_id":"def-456","status":"success","error_msg":""},
--     ...
--   ]
--
-- agent_task_id（单 task 列）保留兼容；批量时用 vm_task_map[0].task_id 填它，
-- 但实际查询走 vm_task_map 拿全列表。

ALTER TABLE deployment ADD COLUMN vm_task_map JSON NULL AFTER agent_task_id;

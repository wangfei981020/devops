-- Migration 034: project_env 加 at_lark_ids（固定艾特人，新增模块通知时自动 @ 这些人）。
--   存所选通知人的 Lark ID 列表（一行一个）；「项目参数」页配。
--   最终艾特 = 操作人 + 这里配的固定艾特人 + 新增时临时选的，去重。

ALTER TABLE project_env ADD COLUMN at_lark_ids VARCHAR(1000) NOT NULL DEFAULT '';

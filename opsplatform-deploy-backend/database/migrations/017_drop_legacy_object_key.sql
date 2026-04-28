-- Migration 017: 删除 deployment_pod_logs.object_key 旧列
--
-- 历史背景：
--   - 012 建表时只有 object_key 一列存归档对象（v88 时代）
--   - 016 加了 previous_key/current_key/events_key 拆 3 种归档，并把 object_key 数据迁到 previous_key
--   - 016 没删 object_key，导致 v93+ 的 INSERT 不写这列时触发 NOT NULL 报错：
--     `Error 1364 (HY000): Field 'object_key' doesn't have a default value`
--
-- 该列已无任何代码引用（log_archiver/queryArchivedLog 全部用新 3 列），数据已迁完，可安全删除。

ALTER TABLE deployment_pod_logs DROP COLUMN object_key;

-- Migration 033: orchestration_template.module_type 枚举加 'zkv'（z-kv-secrets 模板类型）。
--   z-kv 模板 = 整份密钥 chart，供新项目「初始化 z-kv-secrets」时复制。

ALTER TABLE orchestration_template MODIFY COLUMN module_type ENUM('frontend','backend','zkv') NOT NULL DEFAULT 'backend';

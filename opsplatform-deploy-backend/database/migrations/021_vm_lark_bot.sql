-- Migration 021: vm_project_env 加 lark_bot_id，让 VM 部署完发 Lark 卡片
--
-- 跟 K8s 侧 project_env.lark_bot_id 同字段名同语义：
--   非空 → 用此 bot 的 webhook + secret
--   NULL → fallback 到 global_config.lark_default_webhook
--
-- 不加 FK，删 lark_bot 时 vm_project_env 这边变孤儿引用，发通知时取不到回退到默认，
-- 跟 K8s 既有行为对齐（参见 handlers/lark_bots.go 的 LoadLarkBot 容错）

ALTER TABLE vm_project_env ADD COLUMN lark_bot_id BIGINT NULL AFTER project_code;

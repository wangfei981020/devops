-- Migration 010: sessions 表加 permissions JSON 字段
-- Date: 2026-04-24
-- 缓存 portal-auth 时从运维平台拉到的按钮/菜单权限码，避免每次 API 请求都回头查 portal。
-- AuthMiddleware 校验 session 时顺手读出来放进 request context，RequireButton 从 ctx 取。

ALTER TABLE sessions ADD COLUMN permissions TEXT NULL
  COMMENT 'JSON: {"deploy_center:restart": true, "menu:deploy_center_console": true, ...}';

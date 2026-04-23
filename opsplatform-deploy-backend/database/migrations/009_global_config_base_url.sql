-- Migration 009: global_config 加 deploy_center_base_url 字段
-- Date: 2026-04-23
-- 发布完成后 Lark 卡片上「查看发布详情」按钮会跳到这个 URL + /history?expand=<depID>
-- 不配置的话 Lark 按钮回落到 git commit 链接

ALTER TABLE global_config ADD COLUMN deploy_center_base_url VARCHAR(500) NOT NULL DEFAULT ''
  COMMENT '发布中心对外访问的 base URL（如 http://opsplatform-deploy.xx.com），Lark 通知按钮跳转用';

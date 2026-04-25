-- Migration 011: sessions 加 allowed_envs JSON 字段
-- Date: 2026-04-25
-- portal-auth 时从运维平台拉到的 env 白名单（来自 role_deploy_envs 表）。
-- 非 admin 用户的所有 List/操作 API 都按这个白名单过滤；
-- admin / super_admin → allowed_envs=NULL，不过滤。

ALTER TABLE sessions ADD COLUMN allowed_envs TEXT NULL
  COMMENT 'JSON 数组 ["g32-uat","g50-uat"]；NULL=admin 不过滤；空数组=没有任何 env 权限';

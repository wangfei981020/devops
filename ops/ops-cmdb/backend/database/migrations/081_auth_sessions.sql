-- 权限管理地基：会话表 + users 表补认证来源字段
--
-- 背景：CMDB 此前只有一个本地 admin，登录发一个 JWT 就完事，231 个接口无差别放行。
-- 接入运维平台（opsplatform）的 RBAC 后需要两样东西：
--   1. 一处能缓存"这个人有哪些权限"的地方 —— 否则每个请求都要回头问运维平台，
--      一是慢，二是运维平台抖一下整个 CMDB 就跟着不可用。
--   2. 区分本地账号和 portal 账号 —— 本地 admin 是运维平台挂了时的兜底通道，
--      必须无条件放行；portal 用户则严格按拉回来的权限码判。

-- 会话：一次登录一行，权限快照随会话走
CREATE TABLE IF NOT EXISTS auth_sessions (
  id          BIGINT AUTO_INCREMENT PRIMARY KEY,
  user_id     INT          NOT NULL,
  username    VARCHAR(64)  NOT NULL DEFAULT '',
  -- 存 token 的 sha256 而不是 token 本身：库被读走也无法冒充登录
  token_hash  CHAR(64)     NOT NULL,
  -- 权限快照 {"menu:cmdb":true,...}，portal-auth 时从运维平台拉好写在这
  permissions JSON         NULL,
  auth_source VARCHAR(16)  NOT NULL DEFAULT 'local',
  expires_at  DATETIME     NOT NULL,
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uk_token (token_hash),
  KEY idx_user (user_id),
  KEY idx_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- users 补字段：portal 用户首次登录时自动建号
ALTER TABLE users ADD COLUMN auth_source VARCHAR(16) NOT NULL DEFAULT 'local';
ALTER TABLE users ADD COLUMN last_login_at DATETIME NULL;
-- portal 用户没有本地密码，password_hash 允许留空
ALTER TABLE users MODIFY COLUMN password_hash VARCHAR(255) NOT NULL DEFAULT '';

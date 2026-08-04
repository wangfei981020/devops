-- 本地账号也能分配角色。
--
-- 为什么加这个：在此之前，CMDB 自己建的本地账号**必然是全权限**——
-- 中间件见到 auth_source=local 就无条件放行。想开一个只读号根本做不到，
-- 只能去运维平台建号再配角色再绑应用，跨两个系统走三步。
--
-- 边界没变：本地账号仍然是"运维平台不可用时的兜底通道"，
-- 只是这个通道现在可以按角色收窄，而不是只有"全开"一档。
--
-- ⚠️ 权限的真相依然优先在运维平台。这里是本地账号**专用**的一份角色定义，
--    SSO 用户一律不看它（他们的权限每次登录都从运维平台实时拉）。
--    两边的角色代号刻意保持一致，便于对照。

CREATE TABLE IF NOT EXISTS local_roles (
  code        VARCHAR(32)  NOT NULL PRIMARY KEY,
  name        VARCHAR(64)  NOT NULL,
  description VARCHAR(255) NOT NULL DEFAULT '',
  -- 权限码数组（JSON），与运维平台下发的码同一套：menu:cmdb_* / cmdb:*
  permissions JSON         NOT NULL,
  -- 内置角色不允许删除，但允许改权限（管理员按自己的需要收放）
  is_builtin  TINYINT      NOT NULL DEFAULT 0,
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 用户的角色。空串 = 不受限（保持兜底账号的语义）。
ALTER TABLE users ADD COLUMN role_code VARCHAR(32) NOT NULL DEFAULT '' AFTER is_admin;

-- ⚠️ 已存在的本地账号必须保持原样全权限，否则升级这一下就把人锁在门外了。
-- 空串正是"不受限"，所以这里什么都不用做——写出来是为了让人知道这是想清楚的，
-- 不是漏了。

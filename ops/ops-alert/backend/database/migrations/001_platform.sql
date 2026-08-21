-- 平台级表：不带 tenant_id，跨租户共享。
-- 新增平台表必须同时登记进 internal/store/whitelist.go，否则运行期直接拒绝访问。
--
-- 全库统一 utf8mb4_unicode_ci。曾经有系统里两张表 collation 不一致，
-- JOIN 时报 "Illegal mix of collations"，而那条查询从来没成功过一次——
-- 界面上表现为"这里一直是空的"，没人当成故障。

-- ⚠️ 下面这两张表（install_identity / licenses）在 **014_license.sql 里被重建**过：
--    当时是预留结构、从没接线，接 licensekit 时形状对不上。
--    新装的库会先按这里建一次、再被 014 改成最终形状 —— 看起来绕，
--    但改已应用的迁移比这更危险。**要改结构去 014 之后加新迁移，别动这里。**
CREATE TABLE IF NOT EXISTS install_identity (
  id          TINYINT      NOT NULL PRIMARY KEY,
  install_id  CHAR(36)     NOT NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='安装身份。存库而不取宿主机标识：多副本时每个 Pod 的宿主机不同，绑定安装的 license 会在部分副本上校验失败，表现为「重启后偶发未授权」';

CREATE TABLE IF NOT EXISTS licenses (
  id           BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  token        MEDIUMTEXT   NOT NULL,
  status       VARCHAR(32)  NOT NULL DEFAULT 'unknown',
  checked_at   DATETIME(3)  NULL,
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='授权 token。状态热加载，不缓存在内存里——改了 license 要能不重启生效';

CREATE TABLE IF NOT EXISTS tenants (
  id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  name        VARCHAR(128) NOT NULL,
  code        VARCHAR(64)  NOT NULL,
  status      VARCHAR(32)  NOT NULL DEFAULT 'active',
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at  DATETIME(3)  NULL,
  UNIQUE KEY uk_tenants_code (code, deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='租户目录。唯一索引带 deleted_at：软删后同名可以再建；⚠️MySQL 的 NULL 不参与唯一性比较，所以多条软删同名记录是允许的，这正是我们要的';

CREATE TABLE IF NOT EXISTS users (
  id            BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  username      VARCHAR(128) NOT NULL,
  display_name  VARCHAR(128) NOT NULL DEFAULT '',
  password_hash VARCHAR(255) NOT NULL DEFAULT '',
  auth_source   VARCHAR(32)  NOT NULL DEFAULT 'local',
  role_code     VARCHAR(32)  NOT NULL DEFAULT 'viewer',
  email         VARCHAR(255) NOT NULL DEFAULT '',
  phone         VARCHAR(64)  NOT NULL DEFAULT '',
  status        VARCHAR(32)  NOT NULL DEFAULT 'active',
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at    DATETIME(3)  NULL,
  UNIQUE KEY uk_users_name (username, deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='账号。role_code 空串视为受限而非全权限——曾经有系统把空角色当成管理员，迁移上来的老账号全成了超级用户';

CREATE TABLE IF NOT EXISTS user_tenants (
  user_id    BIGINT      NOT NULL,
  tenant_id  BIGINT      NOT NULL,
  role_code  VARCHAR(32) NOT NULL DEFAULT 'member',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (user_id, tenant_id),
  KEY idx_ut_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='用户 × 租户归属。登录时的租户解析与请求时的租户校验必须用同一份判据，两边不一致的后果是「登得进去但每个接口 403」';

CREATE TABLE IF NOT EXISTS auth_sessions (
  id          CHAR(36)     NOT NULL PRIMARY KEY,
  user_id     BIGINT       NOT NULL,
  tenant_id   BIGINT       NOT NULL,
  expires_at  DATETIME(3)  NOT NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_sess_user (user_id),
  KEY idx_sess_exp (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='会话存库而非纯 JWT：改角色、禁用账号必须能立刻踢掉在线会话，纯 JWT 做不到';

CREATE TABLE IF NOT EXISTS leases (
  name        VARCHAR(64)  NOT NULL PRIMARY KEY,
  holder      VARCHAR(128) NOT NULL,
  expires_at  DATETIME(3)  NOT NULL,
  updated_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='调度租约。租约而非永久锁：持有者被 kill 后不会永久卡死，检测引擎会在租约过期后由别的副本接管';

CREATE TABLE IF NOT EXISTS audit_logs (
  id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id   BIGINT       NOT NULL DEFAULT 0,
  actor       VARCHAR(128) NOT NULL,
  action      VARCHAR(64)  NOT NULL,
  target_type VARCHAR(64)  NOT NULL DEFAULT '',
  target_id   VARCHAR(128) NOT NULL DEFAULT '',
  detail      JSON         NULL,
  ip          VARCHAR(64)  NOT NULL DEFAULT '',
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_audit_tenant_time (tenant_id, created_at),
  KEY idx_audit_target (target_type, target_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='审计。平台管理员代入租户的操作对该租户可见——这是信任前提，不是可选项';

CREATE TABLE IF NOT EXISTS brand_settings (
  tenant_id    BIGINT       NOT NULL PRIMARY KEY,
  product_name VARCHAR(128) NOT NULL DEFAULT '',
  logo_url     VARCHAR(512) NOT NULL DEFAULT '',
  primary_hex  VARCHAR(16)  NOT NULL DEFAULT '',
  updated_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='白标。社区版必须在读取侧强制回落默认品牌，只拦写入是不够的——库里可能有降级前留下的数据';

INSERT IGNORE INTO tenants (id, name, code, status) VALUES (1, '默认租户', 'default', 'active');

-- ops-access-plane 初始表：租户 / 用户 / 应用 / 应用分组 / 访问授权
--
-- # 顺序上的一条铁律
--
-- tenant_id 必须在写第一张业务表之前就位。事后补 = 全库数据迁移 + 全部查询重写，
-- 成本翻倍。所以本迁移里每一张业务表从第一天起就带 tenant_id。
--
-- # 平台级 vs 租户级
--
-- 不带 tenant_id 的只有 5 张：tenants / users / user_tenants / auth_sessions /
-- idp_configs（外加 licenses、schema_migrations）。它们在 internal/store/whitelist.go
-- 里登记，改动需评审。其余一律带 tenant_id。
--
-- # 字符集
--
-- utf8mb4 + utf8mb4_0900_ai_ci：应用名、分组名会有中文与 emoji（客户自己起的名字）。

-- ══════════════════════════════════════════════════════════════════
-- 平台级
-- ══════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS tenants (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name        VARCHAR(128) NOT NULL COMMENT '显示名',
  slug        VARCHAR(64)  NOT NULL COMMENT 'URL 与标签用的短标识',
  status      VARCHAR(16)  NOT NULL DEFAULT 'active' COMMENT 'active/suspended',
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted_at  DATETIME     NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_tenants_slug (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 默认租户 id=1。0 保留给平台级数据，绝不分配给真实租户。
INSERT INTO tenants (id, name, slug) VALUES (1, '默认租户', 'default')
  ON DUPLICATE KEY UPDATE id = id;

CREATE TABLE IF NOT EXISTS users (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  username      VARCHAR(128) NOT NULL COMMENT '登录标识，来自上游或本地',
  display_name  VARCHAR(128) NOT NULL DEFAULT '',
  email         VARCHAR(255) NOT NULL DEFAULT '',
  employee_id   VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '上游员工号，审计里用它而不是用户名',
  source        VARCHAR(32)  NOT NULL DEFAULT 'local' COMMENT 'local/oidc/saml',
  idp_config_id BIGINT UNSIGNED NOT NULL DEFAULT 0,
  status        VARCHAR(16)  NOT NULL DEFAULT 'active' COMMENT 'active/disabled，离职断权置 disabled',
  is_break_glass TINYINT(1)  NOT NULL DEFAULT 0 COMMENT '应急账号：登录必发通知，且计入韧性态势',
  last_login_at DATETIME     NULL,
  created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted_at    DATETIME     NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_users_source_username (source, idp_config_id, username),
  KEY idx_users_employee (employee_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS user_tenants (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id    BIGINT UNSIGNED NOT NULL,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  role_code  VARCHAR(64) NOT NULL DEFAULT 'member' COMMENT '控制台内的角色，不是访问授权',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_user_tenant (user_id, tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ══════════════════════════════════════════════════════════════════
-- 租户级：组织与人
-- ══════════════════════════════════════════════════════════════════

-- 部门树。授权按**子树**匹配：规则挂在「研发中心」，「研发中心/后端组」的人也命中。
--
-- depth 与 path 是冗余字段，但必须有：
--   · depth 决定授权具体度（越深越优先），求值时要用，不能每次现算
--   · path 让「取某人所在部门的祖先链」变成一次字符串拆分，而不是递归查询
CREATE TABLE IF NOT EXISTS departments (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  parent_id  BIGINT UNSIGNED NOT NULL DEFAULT 0,
  name       VARCHAR(128) NOT NULL,
  path       VARCHAR(512) NOT NULL DEFAULT '' COMMENT '祖先链，形如 /1/7/23/',
  depth      INT          NOT NULL DEFAULT 1 COMMENT '根为 1；授权具体度按它排',
  ext_id     VARCHAR(128) NOT NULL DEFAULT '' COMMENT '上游部门 ID，同步用',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted_at DATETIME NULL,
  PRIMARY KEY (id),
  KEY idx_dept_tenant_parent (tenant_id, parent_id),
  KEY idx_dept_tenant_path (tenant_id, path(191))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS user_groups (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  code       VARCHAR(64)  NOT NULL,
  name       VARCHAR(128) NOT NULL,
  source     VARCHAR(32)  NOT NULL DEFAULT 'local' COMMENT 'local/synced，同步来的组不允许在本地改成员',
  ext_id     VARCHAR(128) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted_at DATETIME NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_user_groups_tenant_code (tenant_id, code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE IF NOT EXISTS user_group_members (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  group_id   BIGINT UNSIGNED NOT NULL,
  user_id    BIGINT UNSIGNED NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_ugm (tenant_id, group_id, user_id),
  KEY idx_ugm_user (tenant_id, user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 人在部门里的位置。一个人可以挂多个部门（兼岗），求值时取所有部门的祖先链并集。
CREATE TABLE IF NOT EXISTS user_departments (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  user_id    BIGINT UNSIGNED NOT NULL,
  dept_id    BIGINT UNSIGNED NOT NULL,
  is_primary TINYINT(1) NOT NULL DEFAULT 1,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_user_dept (tenant_id, user_id, dept_id),
  KEY idx_user_dept_dept (tenant_id, dept_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ══════════════════════════════════════════════════════════════════
-- 租户级：应用与自定义分组
-- ══════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS apps (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id    BIGINT UNSIGNED NOT NULL,
  code         VARCHAR(64)  NOT NULL COMMENT '稳定标识，接入配置与审计都引用它',
  name         VARCHAR(128) NOT NULL,
  description  VARCHAR(512) NOT NULL DEFAULT '',
  connect_type VARCHAR(24)  NOT NULL COMMENT 'oidc/saml/gateway/formfill —— 后两种是零改造接入',
  env          VARCHAR(16)  NOT NULL DEFAULT 'PROD' COMMENT '原样照搬客户的枚举，不加解释性后缀',
  base_url     VARCHAR(512) NOT NULL DEFAULT '',
  icon_text    VARCHAR(8)   NOT NULL DEFAULT '' COMMENT '门户卡片上的字母标，不存图省得管文件',
  icon_color   VARCHAR(16)  NOT NULL DEFAULT '',
  status       VARCHAR(16)  NOT NULL DEFAULT 'active' COMMENT 'active/disabled',
  -- 门户可见性：无权限的应用默认不显示。设为 1 则显示为「需申请」——
  -- 让人看得见才申请得了，但也等于把应用清单暴露给全员，所以是每应用可选。
  show_when_denied TINYINT(1) NOT NULL DEFAULT 0,
  created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted_at   DATETIME NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_apps_tenant_code (tenant_id, code),
  KEY idx_apps_tenant_status (tenant_id, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 应用分组：客户自定义。同时用于
--   ① 门户里的分栏显示（按 sort_order 排）
--   ② 授权的作用域（一条规则管整组，新应用进组自动继承）
CREATE TABLE IF NOT EXISTS app_groups (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id   BIGINT UNSIGNED NOT NULL,
  code        VARCHAR(64)  NOT NULL,
  name        VARCHAR(128) NOT NULL,
  description VARCHAR(512) NOT NULL DEFAULT '',
  sort_order  INT          NOT NULL DEFAULT 0 COMMENT '门户里的分栏顺序',
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted_at  DATETIME NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_app_groups_tenant_code (tenant_id, code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 应用 × 分组，多对多。
--
-- is_primary 决定门户里这个应用出现在**哪一栏** —— 多归属只影响授权，
-- 不能让同一个应用在门户里出现两次（用户会以为是两套系统）。
-- 唯一索引保证每个应用最多一个主分组。
CREATE TABLE IF NOT EXISTS app_group_members (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  group_id   BIGINT UNSIGNED NOT NULL,
  app_id     BIGINT UNSIGNED NOT NULL,
  is_primary TINYINT(1) NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_agm (tenant_id, group_id, app_id),
  KEY idx_agm_app (tenant_id, app_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ══════════════════════════════════════════════════════════════════
-- 租户级：访问授权
-- ══════════════════════════════════════════════════════════════════

-- 三层作用域 × 五类主体 × 允许/拒绝。判定语义见
-- internal/domain/access/model.go 的 package 注释（那里是唯一事实源）。
--
-- 一句话：主体越具体越优先；主体相同则作用域越具体越优先；再相同则 deny 赢；
-- 强制拒绝盖一切；一条都没命中 = 拒绝。
CREATE TABLE IF NOT EXISTS access_policies (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id    BIGINT UNSIGNED NOT NULL,

  scope        VARCHAR(16) NOT NULL COMMENT 'global/group/app',
  scope_id     BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'group→app_groups.id，app→apps.id，global→0',

  subject_type VARCHAR(16) NOT NULL COMMENT 'public/dept/role/group/user',
  subject_id   BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'public→0',

  effect       VARCHAR(8)  NOT NULL DEFAULT 'allow' COMMENT 'allow/deny',

  -- 强制：本条不可被任何更具体的规则覆盖。**只允许配在 deny 上**
  -- （强制放行等于不可关闭的后门，代码层面已拒绝，这里再加一道 CHECK）。
  enforced     TINYINT(1)  NOT NULL DEFAULT 0,

  note         VARCHAR(512) NOT NULL DEFAULT '' COMMENT '为什么加这条 —— 半年后复核时唯一能看的东西',
  created_by   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted_at   DATETIME NULL,

  PRIMARY KEY (id),
  -- 同一个主体在同一个作用域上只能有一条规则：允许两条相反的规则并存，
  -- 等于让"谁赢"取决于 ID 大小，运维永远搞不清为什么。要改就改那一条。
  UNIQUE KEY uq_policy_scope_subject (tenant_id, scope, scope_id, subject_type, subject_id, deleted_at),
  KEY idx_policy_tenant_scope (tenant_id, scope, scope_id),
  KEY idx_policy_subject (tenant_id, subject_type, subject_id),
  CONSTRAINT ck_policy_enforced_deny_only CHECK (enforced = 0 OR effect = 'deny')
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ══════════════════════════════════════════════════════════════════
-- 审计
-- ══════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS audit_logs (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id   BIGINT UNSIGNED NOT NULL COMMENT '平台操作用 0',
  actor_id    BIGINT UNSIGNED NOT NULL DEFAULT 0,
  actor_name  VARCHAR(128) NOT NULL DEFAULT '',
  action      VARCHAR(64)  NOT NULL COMMENT '如 policy.create / group.delete',
  object_type VARCHAR(64)  NOT NULL DEFAULT '',
  object_id   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  detail      JSON         NULL COMMENT '改动前后，脱敏后写入',
  request_id  VARCHAR(64)  NOT NULL DEFAULT '',
  client_ip   VARCHAR(64)  NOT NULL DEFAULT '',
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_audit_tenant_time (tenant_id, created_at),
  KEY idx_audit_object (tenant_id, object_type, object_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

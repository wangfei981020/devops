-- 单点登录（OIDC）。
--
-- # 为什么做成"什么都能填"而不是预置几个 IdP
--
-- 这套系统交付给多个子公司自建部署，各家用什么身份源事先不知道
-- （Azure AD / Keycloak / Okta / Authentik / 自研）。预置几个的后果是
-- 第四家来了要改代码重新发版 —— 而他们拿不到这条路。
--
-- 所以端点、claim 名、scope 全部可配。代价是配置项多，
-- 用「自动发现 + 配置测试」两件事把这个代价压回去。

CREATE TABLE IF NOT EXISTS oidc_config (
  id            TINYINT      NOT NULL DEFAULT 1 COMMENT '恒为 1，单行表：一套部署接一个身份源',
  enabled       TINYINT(1)   NOT NULL DEFAULT 0,
  -- 登录页按钮上显示的名字。"用 XX 登录" —— 不写的话按钮只能叫"单点登录"，
  -- 而用户认得的是自家 IdP 的名字
  display_name  VARCHAR(64)  NOT NULL DEFAULT '',

  issuer        VARCHAR(512) NOT NULL DEFAULT '',
  -- 自动发现：从 issuer 的 /.well-known/openid-configuration 拉端点。
  --
  -- ⚠️ 必须能关掉。有的 IdP 根本不提供 discovery 文档，或者提供的
  -- 与实际端点不一致（Grafana 接 MXID 时就撞到过：generic_oauth 不支持
  -- 自动发现，端点只能手填）。关掉时用下面四个手填字段。
  auto_discover TINYINT(1)   NOT NULL DEFAULT 1,
  auth_url      VARCHAR(512) NOT NULL DEFAULT '',
  token_url     VARCHAR(512) NOT NULL DEFAULT '',
  userinfo_url  VARCHAR(512) NOT NULL DEFAULT '',
  jwks_url      VARCHAR(512) NOT NULL DEFAULT '',

  client_id     VARCHAR(255) NOT NULL DEFAULT '',
  -- 加密存。与数据源凭据同一把 ALERT_AES_KEY
  client_secret_enc BLOB     NULL,
  scopes        VARCHAR(255) NOT NULL DEFAULT 'openid profile email',

  -- ── claim 映射 ────────────────────────────────────────────
  --
  -- 🔴 各家 IdP 的 claim 名完全不同，这里必须可配：
  --   Azure AD    preferred_username / upn / email / groups
  --   Keycloak    preferred_username / email / realm_access.roles
  --   自研        各写各的
  -- 写死一套的后果是接第二家就报 "Claim not found"。
  username_claim VARCHAR(64) NOT NULL DEFAULT 'preferred_username',
  email_claim    VARCHAR(64) NOT NULL DEFAULT 'email',
  name_claim     VARCHAR(64) NOT NULL DEFAULT 'name',
  groups_claim   VARCHAR(64) NOT NULL DEFAULT 'groups',

  -- 🔴 claim 从哪里取：id_token / userinfo / both。
  --
  -- 这一项是踩出来的：MXID 把 name 和 email **只放在 userinfo**，
  -- 不放进 id_token。只读 id_token 的接入方（Atlassian 那套）必然报
  -- "Claim not found"，而 IdP 侧看一切正常。
  -- 默认 both（两处都取、userinfo 覆盖 id_token）—— 这是唯一不会漏的选择。
  claim_source  VARCHAR(16)  NOT NULL DEFAULT 'both',

  -- ── 建号与授权 ────────────────────────────────────────────
  --
  -- ⚠️ JIT 会持续建账号。CE 的用户数上限已放宽到 100，
  -- 但接了 SSO 的部署仍应关注这个数 —— 到顶时新人登录会失败，
  -- 而失败现场看起来像是 SSO 配错了。
  jit_enabled   TINYINT(1)   NOT NULL DEFAULT 1,
  -- 没命中任何角色映射时给的角色。
  --
  -- 🔴 默认 viewer，**绝不能默认 admin**。默认给管理员的话，
  -- 身份源里任何一个人登录一次就成了这套系统的管理员。
  default_role  VARCHAR(64)  NOT NULL DEFAULT 'viewer',
  -- 群组 → 角色映射，形如 {"ops-admin":"rule_admin","sre":"oncall"}
  role_mapping  JSON         NULL,

  updated_by    VARCHAR(128) NOT NULL DEFAULT '',
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='单点登录配置，单行';

INSERT IGNORE INTO oidc_config (id) VALUES (1);

-- 登录流程的一次性状态（防 CSRF / 重放）。
--
-- ⚠️ 存库而不是存内存：多副本下发起登录的副本和处理回调的副本
-- 不一定是同一个，存内存会表现为"有时候登录成功、有时候提示 state 不匹配"，
-- 而副本越多失败率越高。
CREATE TABLE IF NOT EXISTS oidc_states (
  state      CHAR(43)    NOT NULL PRIMARY KEY COMMENT 'base64url(32字节)',
  nonce      CHAR(43)    NOT NULL,
  redirect   VARCHAR(512) NOT NULL DEFAULT '' COMMENT '登录后跳回哪里',
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_oidc_state_time (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='OIDC 登录态，用完即删；过期的由清理任务扫';

-- SSO 配置页的菜单权限。只给 admin（靠 unrestricted 生效）——
-- 这里能配的东西等于"谁能进这套系统"，不该给运维角色。

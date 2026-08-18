-- OIDC 单点登录接入。
--
-- CMDB 在这里扮演 **RP（依赖方）**：它去连别人的 IdP（飞书 / Okta / OneGate），
-- 而不是自己当 IdP。两个方向很容易搞反，搞反了会做出一个谁也连不上的东西。
--
-- ⚠️ 单行表（id 恒为 1）。做成多 IdP 的话，登录页就得让用户先选"从哪登"，
-- 而绝大多数客户只有一个 IdP —— 多出来的那一步选择对所有人都是负担。
-- 真有多 IdP 需求时再加表，不要现在预留。
CREATE TABLE IF NOT EXISTS idp_configs (
  id            TINYINT      NOT NULL DEFAULT 1 COMMENT '恒为 1，单行表',
  enabled       TINYINT      NOT NULL DEFAULT 0,
  display_name  VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '登录页按钮上的文字，如「用飞书登录」',

  -- OIDC discovery 的根地址，形如 https://example.okta.com
  -- 我们从 {issuer}/.well-known/openid-configuration 拉端点，不让客户逐个填
  issuer        VARCHAR(255) NOT NULL DEFAULT '',
  client_id     VARCHAR(255) NOT NULL DEFAULT '',
  -- ⚠️ 加密存储，任何接口都不回传。只暴露"配没配"
  client_secret_enc TEXT,
  scopes        VARCHAR(255) NOT NULL DEFAULT 'openid profile email',

  -- 从 id_token 的哪个 claim 取用户名/显示名。
  -- 不同 IdP 的字段名差别很大（sub / preferred_username / email），
  -- 写死一个的话换个 IdP 就登不进来，而错误信息只会说"缺少 subject"
  username_claim VARCHAR(64) NOT NULL DEFAULT 'preferred_username',
  name_claim     VARCHAR(64) NOT NULL DEFAULT 'name',

  -- JIT：首次 SSO 登录时自动建账号。
  --
  -- ⚠️ **默认关闭**。席位（seats）是授权的容量项，JIT 打开后
  -- 全公司任何人点一下登录就占一个席位 —— 客户会在毫不知情的情况下超容量。
  -- 打开它必须是个明确的决定。
  jit_enabled   TINYINT      NOT NULL DEFAULT 0,
  -- JIT 建出来的账号给什么角色。空 = 不受限，那是危险的默认，所以给只读
  jit_role_code VARCHAR(64)  NOT NULL DEFAULT 'cmdb_viewer',

  updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  updated_by    VARCHAR(255) NOT NULL DEFAULT '',
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='OIDC 接入，单行';

-- 登录流程里的一次性 state。
--
-- ⚠️ 必须落库，不能放进程内存：多副本下发起登录的 Pod 和处理回调的 Pod
-- 通常不是同一个，内存里的 state 在回调时查不到 ——
-- 表现是"登录随机失败，刷新几次就好了"，最难查的一类。
CREATE TABLE IF NOT EXISTS idp_login_states (
  state      VARCHAR(64)  NOT NULL,
  nonce      VARCHAR(64)  NOT NULL COMMENT '防重放：要与 id_token 里的 nonce 一致',
  redirect   VARCHAR(255) NOT NULL DEFAULT '' COMMENT '登录后跳回哪个页面',
  created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  -- 过期时间短：state 只在"用户点登录 → IdP 那边输密码 → 跳回来"这段时间有用。
  -- 给太长等于留一堆可被重放的凭证
  expires_at DATETIME     NOT NULL,
  PRIMARY KEY (state),
  KEY idx_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='OIDC 登录 state，一次性';

-- 用户表补一列：这个账号是不是 SSO 来的、对应 IdP 里的哪个 subject。
--
-- ⚠️ 用 subject 而不是用户名做关联键：用户名在 IdP 那边是可以改的
-- （改个花名、换个邮箱前缀），而 subject 永不变。
-- 按用户名关联的话，某人改了名字，再登录就会**变成另一个账号**，
-- 原账号的权限全都丢了，而系统不会有任何报错。
--
-- ⚠️ "没绑 SSO" 必须是 NULL，不能是空串。
-- 唯一索引下空串会互相冲突 —— 第二个本地账号就建不出来了，
-- 而报错是一句 `Duplicate entry '' for key 'uniq_idp_subject'`，
-- 完全看不出跟 SSO 有关。NULL 在 MySQL 的唯一索引里可以有任意多个，
-- 这正是"这一项不适用"该有的表示法。
ALTER TABLE users ADD COLUMN idp_subject VARCHAR(255) NULL DEFAULT NULL AFTER auth_source;
-- 上一句在列已存在时会被跳过（本迁移在开发中曾以 NOT NULL DEFAULT '' 建过），
-- 所以这里无条件把它归一成可空，再把空串换成 NULL
ALTER TABLE users MODIFY COLUMN idp_subject VARCHAR(255) NULL DEFAULT NULL;
UPDATE users SET idp_subject = NULL WHERE idp_subject = '';
CREATE UNIQUE INDEX uniq_idp_subject ON users (idp_subject);

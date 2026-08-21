-- 二次验证、上游身份源、审计哈希链。
--
-- 这一批把四个已完成的领域包接进数据层：mfa / idp / evidence / license。

-- ══════════════════════════════════════════════════════════════════
-- 二次验证（平台级：绑在人身上，不绑租户）
-- ══════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS mfa_secrets (
  user_id     BIGINT UNSIGNED NOT NULL,
  secret_enc  VARBINARY(512) NOT NULL COMMENT 'AES-GCM 密文；明文只在绑定的那一刻出现一次',
  confirmed_at DATETIME NULL COMMENT '扫码后必须先验一次才算绑定成功',
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 提权票据：一次二次验证换来的短时通行证。
--
-- **不复用会话**：会话是 8 小时的，二次验证是 30 分钟的。
-- 混在一起等于「二次验证的有效期 = 登录有效期」，也就是只在登录时验了一次。
CREATE TABLE IF NOT EXISTS step_up_tickets (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id   BIGINT UNSIGNED NOT NULL,
  user_id     BIGINT UNSIGNED NOT NULL,
  app_id      BIGINT UNSIGNED NOT NULL,
  scope       VARCHAR(255) NOT NULL DEFAULT '' COMMENT '空=该应用全部需验证操作；否则为路径前缀',
  ticket_ref  VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '绑定的工单号，事后要能回答"这次删除依据哪张工单"',
  issued_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at  DATETIME NOT NULL,
  used_count  INT NOT NULL DEFAULT 0,
  revoked_at  DATETIME NULL,
  PRIMARY KEY (id),
  -- 网关每个请求都要查它，索引必须覆盖查询条件
  KEY idx_ticket_lookup (tenant_id, user_id, app_id, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ══════════════════════════════════════════════════════════════════
-- 上游身份源
-- ══════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS idp_configs (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name          VARCHAR(128) NOT NULL COMMENT '显示在登录页按钮上',
  protocol      VARCHAR(16)  NOT NULL DEFAULT 'oidc',
  issuer        VARCHAR(255) NOT NULL DEFAULT '',
  client_id     VARCHAR(255) NOT NULL DEFAULT '',
  client_secret_enc VARBINARY(1024) NOT NULL COMMENT 'AES-GCM 密文，任何接口都不回显',
  auth_url      VARCHAR(512) NOT NULL DEFAULT '',
  token_url     VARCHAR(512) NOT NULL DEFAULT '',
  jwks_url      VARCHAR(512) NOT NULL DEFAULT '',
  redirect_uri  VARCHAR(512) NOT NULL DEFAULT '',
  scopes        VARCHAR(255) NOT NULL DEFAULT 'openid profile email',

  -- 身份映射。subject_claim 默认给 sub，但**强烈建议**配成员工号：
  -- 用 email 当稳定标识时，人改邮箱就变成另一个人，历史审计全对不上。
  subject_claim VARCHAR(64) NOT NULL DEFAULT 'sub',
  name_claim    VARCHAR(64) NOT NULL DEFAULT 'name',
  email_claim   VARCHAR(64) NOT NULL DEFAULT 'email',
  groups_claim  VARCHAR(64) NOT NULL DEFAULT '',
  dept_claim    VARCHAR(64) NOT NULL DEFAULT '',

  -- JIT 默认关：上游能登录 ≠ 该给他访问权。开着的话，上游目录里的每个人
  -- （含外包、离职未清理、测试账号）第一次点进来就会在这边长出账号。
  jit_create    TINYINT(1) NOT NULL DEFAULT 0,
  jit_groups    VARCHAR(255) NOT NULL DEFAULT '' COMMENT '逗号分隔的用户组 ID；空=不进任何组=默认拒绝',

  enabled       TINYINT(1) NOT NULL DEFAULT 1,
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted_at    DATETIME NULL,
  PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 登录流程的中间状态。存服务端而不是 cookie：放 cookie 就得防篡改，
-- 防篡改要签名，签名要密钥轮换 —— 绕一大圈还不如直接存。
CREATE TABLE IF NOT EXISTS idp_auth_states (
  state         VARCHAR(64) NOT NULL,
  nonce         VARCHAR(64) NOT NULL,
  code_verifier VARCHAR(128) NOT NULL,
  config_id     BIGINT UNSIGNED NOT NULL,
  next_url      VARCHAR(512) NOT NULL DEFAULT '',
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  consumed_at   DATETIME NULL COMMENT '用过即废：同一个 state 不能换两次 token',
  PRIMARY KEY (state),
  KEY idx_state_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ══════════════════════════════════════════════════════════════════
-- 审计哈希链
-- ══════════════════════════════════════════════════════════════════

-- 给两张审计表都加链字段。
--
-- 为什么不新建一张"链表"：链必须和记录同生共死。分开存的话，
-- 删掉一条记录而链还在，或者反过来，都会让"校验通过"失去意义。
ALTER TABLE audit_logs
  ADD COLUMN chain_seq  BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '链上序号，租户内单调递增',
  ADD COLUMN prev_hash  CHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN entry_hash CHAR(64) NOT NULL DEFAULT '',
  ADD KEY idx_audit_chain (tenant_id, chain_seq);

-- 链头。每个租户一条，写审计时用它取上一条哈希并推进序号。
--
-- 单独一张表而不是每次 MAX(chain_seq)：并发写时 MAX 会读到同一个值，
-- 两条记录拿到同一个 prev，链就叉了。这里用行锁串行化。
CREATE TABLE IF NOT EXISTS audit_chain_heads (
  tenant_id  BIGINT UNSIGNED NOT NULL,
  last_seq   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  last_hash  CHAR(64) NOT NULL DEFAULT '',
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 外部锚点：定期把链头签名后送出系统。
-- 锚点之前的部分就再也改不动了 —— 这是哈希链唯一防得住"整链重算"的手段。
CREATE TABLE IF NOT EXISTS audit_anchors (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  seq        BIGINT UNSIGNED NOT NULL,
  hash       CHAR(64) NOT NULL,
  signature  VARCHAR(255) NOT NULL,
  public_key VARCHAR(128) NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  exported_to VARCHAR(255) NOT NULL DEFAULT '' COMMENT '送到了哪（对象存储/邮件/第三方）',
  PRIMARY KEY (id),
  KEY idx_anchor_tenant (tenant_id, seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ══════════════════════════════════════════════════════════════════
-- 产品授权
-- ══════════════════════════════════════════════════════════════════

-- 平台级：授权是给整套系统的，不是给某个租户的。
CREATE TABLE IF NOT EXISTS licenses (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  code       TEXT NOT NULL COMMENT '完整激活码（已签名，本身不是秘密）',
  license_id VARCHAR(64) NOT NULL DEFAULT '',
  active     TINYINT(1) NOT NULL DEFAULT 1,
  installed_by BIGINT UNSIGNED NOT NULL DEFAULT 0,
  installed_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_license_active (active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

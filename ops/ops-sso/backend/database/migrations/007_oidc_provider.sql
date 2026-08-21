-- OIDC Provider：让下游应用（CMDB、告警平台…）用标准协议接进关口。
--
-- # 方向说明（很容易搞反）
--
--   idp_configs        我们是 **RP**：去连飞书 / Entra ID 拿身份
--   oidc_clients（本表）我们是 **OP**：CMDB 等应用来连我们
--
-- 两个方向都要有，SSO 才是闭环：员工用飞书登进关口，再由关口签发身份给 CMDB。
--
-- # 关口的 OP 与普通 IdP 的差别
--
-- 授权时**先过访问判定**：没被授权用这个应用的人，走到 authorize 就被拒，
-- 拿不到 code。普通 IdP 只管"你是谁"，下游拿到 token 之后还得自己判权限 ——
-- 那正是每个系统各写一套权限、各写错一遍的由来。

-- 下游客户端。
CREATE TABLE IF NOT EXISTS oidc_clients (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id     BIGINT UNSIGNED NOT NULL,
  -- 绑定到一个应用：授权判定、审计、门户卡片都用它。
  -- 不绑应用的 client 等于一个绕过授权的后门。
  app_id        BIGINT UNSIGNED NOT NULL,

  client_id     VARCHAR(64)  NOT NULL,
  -- bcrypt。**不是可解密的密文** —— 我们永远不需要读回明文，
  -- 只需要验证下游发来的是否正确。存密文等于给自己留一个泄露点。
  client_secret_hash VARBINARY(255) NOT NULL,

  -- 回调地址白名单，换行分隔。**精确匹配**，不支持通配与前缀。
  -- 允许通配是 OIDC 最经典的漏洞：`https://app.example.com/*` 会被
  -- `https://app.example.com/../evil` 之类的构造绕开，token 直接送到攻击者手上。
  -- 用 VARCHAR 而不是 TEXT：MySQL 的 TEXT 列不允许有默认值，
  -- 而"没有登出跳转地址"是常态，让它必填只会逼出一堆空字符串的特判。
  -- 2048 够放十几个地址；真需要更多的客户端，说明它的部署方式该重新想想了。
  redirect_uris    VARCHAR(2048) NOT NULL,
  post_logout_uris VARCHAR(2048) NOT NULL DEFAULT '',

  scopes        VARCHAR(255) NOT NULL DEFAULT 'openid profile email groups',
  -- 公开客户端（SPA / 移动端）没有 secret，必须强制 PKCE
  public_client TINYINT(1) NOT NULL DEFAULT 0,
  require_pkce  TINYINT(1) NOT NULL DEFAULT 1,

  id_token_ttl_sec INT NOT NULL DEFAULT 3600,
  -- 下发哪些 claim。默认不给手机号等敏感信息 —— 应用要不到就泄不了
  claims_profile VARCHAR(255) NOT NULL DEFAULT 'sub,name,email,groups,employee_id',

  enabled       TINYINT(1) NOT NULL DEFAULT 1,
  created_by    BIGINT UNSIGNED NOT NULL DEFAULT 0,
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  deleted_at    DATETIME NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_oidc_client (client_id),
  KEY idx_oidc_client_app (tenant_id, app_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 授权码。
--
-- 只存哈希、只活 60 秒、用过即废 —— 三条都不能省：
--   · 存明文：库泄露 = 任何未用的 code 都能换 token
--   · 活太久：code 在浏览器历史/日志/Referer 里留存，窗口越长越危险
--   · 可重用：截获一次就能反复换 token
CREATE TABLE IF NOT EXISTS oidc_auth_codes (
  code_hash     CHAR(64) NOT NULL,
  tenant_id     BIGINT UNSIGNED NOT NULL,
  client_id     VARCHAR(64)  NOT NULL,
  user_id       BIGINT UNSIGNED NOT NULL,
  redirect_uri  VARCHAR(512) NOT NULL COMMENT '换 token 时必须与发码时完全一致',
  nonce         VARCHAR(128) NOT NULL DEFAULT '',
  scope         VARCHAR(255) NOT NULL DEFAULT '',
  code_challenge VARCHAR(128) NOT NULL DEFAULT '',
  code_challenge_method VARCHAR(8) NOT NULL DEFAULT '',
  auth_time     DATETIME NOT NULL COMMENT '真正完成认证的时刻，写进 id_token 的 auth_time',
  session_id    BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '绑定关口会话，用于全局登出',
  expires_at    DATETIME NOT NULL,
  used_at       DATETIME NULL,
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (code_hash),
  KEY idx_code_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 签名密钥（平台级：整套系统一套，与租户无关）。
--
-- 支持多把并存：轮换时先发布新公钥（下游缓存 JWKS 需要时间），
-- 一段时间后再切换签名用的那把，最后才撤下旧公钥。
-- 一步到位地换会让所有下游在缓存过期前全部验签失败。
CREATE TABLE IF NOT EXISTS oidc_signing_keys (
  kid         VARCHAR(64) NOT NULL,
  algorithm   VARCHAR(8)  NOT NULL DEFAULT 'RS256',
  private_pem_enc VARBINARY(4096) NOT NULL COMMENT 'AES-GCM 密文',
  public_jwk  TEXT NOT NULL COMMENT '直接对外发布的 JWK JSON',
  active      TINYINT(1) NOT NULL DEFAULT 0 COMMENT '当前用于签名的那把，全局只有一把',
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  retired_at  DATETIME NULL,
  PRIMARY KEY (kid),
  KEY idx_key_active (active)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 下游会话，用于全局单点登出。
--
-- 关口登出时，要能把所有下游的会话一起注销 ——
-- 只清自己的 cookie 而下游还登着，"单点登出"就是假的。
CREATE TABLE IF NOT EXISTS oidc_sessions (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id    BIGINT UNSIGNED NOT NULL,
  gate_session_id BIGINT UNSIGNED NOT NULL COMMENT '关口这边的会话',
  client_id    VARCHAR(64)  NOT NULL,
  user_id      BIGINT UNSIGNED NOT NULL,
  sid          VARCHAR(64)  NOT NULL COMMENT '写进 id_token 的 sid，登出时按它通知下游',
  created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  revoked_at   DATETIME NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_oidc_sid (sid),
  KEY idx_oidc_sess_gate (gate_session_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

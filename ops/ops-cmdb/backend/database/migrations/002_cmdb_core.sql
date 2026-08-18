-- CMDB 核心表：CI 通用 + 标签 + 关系 + 域名/证书专属 + 注册商/ACME/设置/审计

-- CI 类型（预置 domain/certificate，可扩展）
CREATE TABLE IF NOT EXISTS ci_types (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  code       VARCHAR(64)  NOT NULL UNIQUE,   -- domain / certificate
  name       VARCHAR(64)  NOT NULL,
  icon       VARCHAR(64)  NOT NULL DEFAULT '',
  sort_order INT          NOT NULL DEFAULT 0,
  created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- CI 通用表（所有资产共有的元数据）
CREATE TABLE IF NOT EXISTS cis (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  type       VARCHAR(64)  NOT NULL,          -- = ci_types.code
  name       VARCHAR(255) NOT NULL,
  project    VARCHAR(128) NOT NULL DEFAULT '',
  env        VARCHAR(32)  NOT NULL DEFAULT '',
  module     VARCHAR(128) NOT NULL DEFAULT '',
  owner      VARCHAR(128) NOT NULL DEFAULT '',
  status     VARCHAR(32)  NOT NULL DEFAULT 'active', -- active/idle/offline/retired
  remark     VARCHAR(512) NOT NULL DEFAULT '',
  created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_type (type),
  KEY idx_env (env)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- CI 自定义键值标签（配合 settings 的导出白名单决定哪些进 Prometheus）
CREATE TABLE IF NOT EXISTS ci_labels (
  id     BIGINT AUTO_INCREMENT PRIMARY KEY,
  ci_id  BIGINT       NOT NULL,
  k      VARCHAR(64)  NOT NULL,
  v      VARCHAR(255) NOT NULL DEFAULT '',
  UNIQUE KEY uniq_ci_k (ci_id, k),
  KEY idx_ci (ci_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- CI 关系（如 certificate -protects-> domain）
CREATE TABLE IF NOT EXISTS ci_relations (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  src_ci_id  BIGINT      NOT NULL,
  dst_ci_id  BIGINT      NOT NULL,
  rel_type   VARCHAR(64) NOT NULL,           -- protects / deployed_on / resolves_to ...
  created_at DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_rel (src_ci_id, dst_ci_id, rel_type),
  KEY idx_src (src_ci_id),
  KEY idx_dst (dst_ci_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 注册商 / DNS 凭据（credential AES 加密）
CREATE TABLE IF NOT EXISTS registrars (
  id             INT AUTO_INCREMENT PRIMARY KEY,
  name           VARCHAR(128) NOT NULL,
  provider       VARCHAR(64)  NOT NULL,       -- dnspod/aliyun/cloudflare/tencent...
  credential_enc TEXT,                        -- AES 加密的 JSON 凭据
  enabled        TINYINT      NOT NULL DEFAULT 1,
  created_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 域名专属（一行对应一个 domain 类型 CI）
CREATE TABLE IF NOT EXISTS domains (
  ci_id          BIGINT       NOT NULL PRIMARY KEY,
  registrar_id   INT          NULL,
  dns_provider   VARCHAR(64)  NOT NULL DEFAULT '',
  expiry_at      DATETIME     NULL,           -- 域名注册到期
  resolve_status VARCHAR(32)  NOT NULL DEFAULT '',
  KEY idx_expiry (expiry_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 证书专属（一行对应一个 certificate 类型 CI）
CREATE TABLE IF NOT EXISTS certificates (
  ci_id        BIGINT       NOT NULL PRIMARY KEY,
  cn           VARCHAR(255) NOT NULL,
  sans         TEXT,                           -- JSON 数组
  ca           VARCHAR(32)  NOT NULL DEFAULT 'letsencrypt', -- letsencrypt/zerossl
  challenge    VARCHAR(16)  NOT NULL DEFAULT 'dns-01',       -- dns-01/http-01
  status       VARCHAR(32)  NOT NULL DEFAULT 'pending',      -- pending/active/expiring/expired/error
  issued_at    DATETIME     NULL,
  expiry_at    DATETIME     NULL,
  auto_renew   TINYINT      NOT NULL DEFAULT 1,
  renew_days   INT          NOT NULL DEFAULT 30,             -- 到期前 N 天续
  cert_pem     TEXT,
  chain_pem    TEXT,
  key_pem_enc  TEXT,                                          -- AES 加密的私钥
  deploy_token VARCHAR(64)  NOT NULL DEFAULT '',              -- A+ 拉取令牌
  version      INT          NOT NULL DEFAULT 0,               -- 每次签发/续期 +1（拉取端比对）
  last_error   VARCHAR(512) NOT NULL DEFAULT '',
  created_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_expiry (expiry_at),
  KEY idx_token (deploy_token)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 证书签发/续期历史
CREATE TABLE IF NOT EXISTS cert_history (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  cert_ci_id BIGINT       NOT NULL,
  action     VARCHAR(32)  NOT NULL,           -- issue/renew/revoke
  result     VARCHAR(16)  NOT NULL,           -- success/fail
  detail     VARCHAR(512) NOT NULL DEFAULT '',
  at         DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_cert (cert_ci_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ACME 账户（account key AES 加密）
CREATE TABLE IF NOT EXISTS acme_accounts (
  id              INT AUTO_INCREMENT PRIMARY KEY,
  email           VARCHAR(255) NOT NULL,
  ca              VARCHAR(32)  NOT NULL DEFAULT 'letsencrypt',
  account_key_enc TEXT,
  status          VARCHAR(32)  NOT NULL DEFAULT 'active',
  created_at      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 设置（key-value）
CREATE TABLE IF NOT EXISTS settings (
  k          VARCHAR(64)  NOT NULL PRIMARY KEY,
  v          TEXT,
  updated_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 审计
CREATE TABLE IF NOT EXISTS audit_logs (
  id       BIGINT AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(64)  NOT NULL DEFAULT '',
  action   VARCHAR(64)  NOT NULL,
  target   VARCHAR(255) NOT NULL DEFAULT '',
  ip       VARCHAR(64)  NOT NULL DEFAULT '',
  at       DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_at (at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 预置 CI 类型
INSERT IGNORE INTO ci_types (code, name, icon, sort_order) VALUES ('domain', '域名', 'Connection', 1);
INSERT IGNORE INTO ci_types (code, name, icon, sort_order) VALUES ('certificate', '证书', 'Lock', 2);

-- 默认设置
INSERT IGNORE INTO settings (k, v) VALUES ('feishu_webhook', '');
INSERT IGNORE INTO settings (k, v) VALUES ('remind_days', '30,15,7,1');
INSERT IGNORE INTO settings (k, v) VALUES ('export_label_whitelist', 'project,env,module,name,ca,registrar');
INSERT IGNORE INTO settings (k, v) VALUES ('default_acme_account_id', '');

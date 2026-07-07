-- 云资源一期：GCP 主机只读台账 + 磁盘 + 成本估算（真实账单 BigQuery 预留字段）
-- 云账号（GCP service account 等；凭据 AES 加密）
CREATE TABLE IF NOT EXISTS cloud_accounts (
  id                     INT AUTO_INCREMENT PRIMARY KEY,
  name                   VARCHAR(128) NOT NULL,
  provider               VARCHAR(32)  NOT NULL DEFAULT 'gcp',
  cred_enc               TEXT,                                   -- AES 加密的 service account JSON
  projects               VARCHAR(512) NOT NULL DEFAULT '',       -- 逗号分隔要同步的 project
  billing_export_dataset VARCHAR(255) NOT NULL DEFAULT '',       -- 预留：BigQuery 账单导出 dataset（真实账单二期用）
  last_sync_at           DATETIME     NULL,
  last_result            VARCHAR(255) NOT NULL DEFAULT '',
  created_at             DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 主机（cis type='host' 的专属表）
CREATE TABLE IF NOT EXISTS hosts (
  ci_id            BIGINT       NOT NULL PRIMARY KEY,            -- cis.id
  instance_id      VARCHAR(128) NOT NULL DEFAULT '',
  cloud_account_id INT          NULL,
  provider         VARCHAR(32)  NOT NULL DEFAULT 'gcp',
  project          VARCHAR(128) NOT NULL DEFAULT '',
  zone             VARCHAR(64)  NOT NULL DEFAULT '',
  region           VARCHAR(64)  NOT NULL DEFAULT '',
  machine_type     VARCHAR(64)  NOT NULL DEFAULT '',
  vcpu             INT          NOT NULL DEFAULT 0,
  mem_mb           INT          NOT NULL DEFAULT 0,
  disk_total_gb    INT          NOT NULL DEFAULT 0,
  internal_ip      VARCHAR(64)  NOT NULL DEFAULT '',
  external_ip      VARCHAR(64)  NOT NULL DEFAULT '',
  status           VARCHAR(32)  NOT NULL DEFAULT '',             -- RUNNING/TERMINATED/STOPPING...
  os               VARCHAR(128) NOT NULL DEFAULT '',
  labels           TEXT,                                         -- JSON
  self_link        VARCHAR(512) NOT NULL DEFAULT '',
  gcp_created_at   DATETIME     NULL,
  stale            TINYINT      NOT NULL DEFAULT 0,              -- GCP 已删=1
  synced_at        DATETIME     NULL,
  updated_at       DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_account (cloud_account_id),
  KEY idx_intip (internal_ip),
  KEY idx_extip (external_ip)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 主机磁盘（逐块）
CREATE TABLE IF NOT EXISTS host_disks (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  host_ci_id BIGINT       NOT NULL,
  name       VARCHAR(128) NOT NULL DEFAULT '',
  size_gb    INT          NOT NULL DEFAULT 0,
  type       VARCHAR(32)  NOT NULL DEFAULT '',                  -- pd-ssd / pd-standard / pd-balanced
  is_boot    TINYINT      NOT NULL DEFAULT 0,
  KEY idx_host (host_ci_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 云价格费率（估算用，可在基础配置编辑；按 provider+region+机型族，default 兜底）
CREATE TABLE IF NOT EXISTS cloud_price_rates (
  id                   INT AUTO_INCREMENT PRIMARY KEY,
  provider             VARCHAR(32)   NOT NULL DEFAULT 'gcp',
  region               VARCHAR(64)   NOT NULL DEFAULT 'default',
  machine_family       VARCHAR(32)   NOT NULL DEFAULT 'default',
  vcpu_hour_usd        DECIMAL(12,6) NOT NULL DEFAULT 0,
  ram_gb_hour_usd      DECIMAL(12,6) NOT NULL DEFAULT 0,
  disk_ssd_gb_month    DECIMAL(12,6) NOT NULL DEFAULT 0,
  disk_std_gb_month    DECIMAL(12,6) NOT NULL DEFAULT 0,
  updated_at           DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uq_rate (provider, region, machine_family)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 默认费率（GCP 目录价近似，USD；管理员可在基础配置改。e2 常规 + pd 磁盘）
INSERT IGNORE INTO cloud_price_rates (provider, region, machine_family, vcpu_hour_usd, ram_gb_hour_usd, disk_ssd_gb_month, disk_std_gb_month)
VALUES ('gcp', 'default', 'default', 0.021811, 0.002923, 0.170000, 0.040000);

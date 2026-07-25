-- 域名续费记录台账：每次续费落一条，防超付可查（平台报价 + GoDaddy 订单号 + 到期前后 + 操作人）。
-- 精确扣费以 GoDaddy 账单为准，凭 order_id 核对；quoted_amount 是本平台下单前的挂牌估算。
CREATE TABLE IF NOT EXISTS domain_renewals (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  domain_ci_id BIGINT NOT NULL,
  domain VARCHAR(255) NOT NULL,
  period INT NOT NULL,
  quoted_currency VARCHAR(8) NOT NULL DEFAULT '',
  quoted_amount DECIMAL(12,2) NOT NULL DEFAULT 0,
  order_id VARCHAR(64) NOT NULL DEFAULT '',
  expiry_before DATE NULL,
  expiry_after DATE NULL,
  operator VARCHAR(128) NOT NULL DEFAULT '',
  env VARCHAR(128) NOT NULL DEFAULT '',
  dry_run TINYINT NOT NULL DEFAULT 0,
  raw_resp VARCHAR(1024) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_domain_ci (domain_ci_id),
  INDEX idx_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 域名管理升级：CDN 厂商列表 + 解析层(domain_records) + 厂商原始 DNS 记录缓存(dns_records)
-- 数据源复用 registrars 表（provider+加密凭据），domains.registrar_id 标来源

-- CDN 厂商（基础配置维护，解析录入时下拉）
CREATE TABLE IF NOT EXISTS cdns (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  name       VARCHAR(128) NOT NULL UNIQUE,
  sort_order INT          NOT NULL DEFAULT 0,
  created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 解析层（一个域名 CI 下挂多条解析；业务视角：每条对外服务）
CREATE TABLE IF NOT EXISTS domain_records (
  id             BIGINT AUTO_INCREMENT PRIMARY KEY,
  domain_ci_id   BIGINT       NOT NULL,             -- 属于哪个域名(cis.id, type=domain)
  host           VARCHAR(255) NOT NULL,             -- 主机名(全名或子域)
  record_type    VARCHAR(16)  NOT NULL DEFAULT 'A', -- A/CNAME...
  cdn_id         INT          NULL,                 -- CDN 厂商(cdns.id)，可空
  cname          VARCHAR(255) NOT NULL DEFAULT '',  -- 回源 CNAME，可空
  origin_ip      VARCHAR(255) NOT NULL DEFAULT '',  -- 源站 IP(可多个逗号分隔)，可空
  cert_expiry_at DATETIME     NULL,                 -- 线上证书到期(TLS 探测)
  cert_check_at  DATETIME     NULL,
  cert_check_msg VARCHAR(255) NOT NULL DEFAULT '',
  project        VARCHAR(128) NOT NULL DEFAULT '',
  env            VARCHAR(32)  NOT NULL DEFAULT '',
  module         VARCHAR(128) NOT NULL DEFAULT '',
  operator       VARCHAR(64)  NOT NULL DEFAULT '',  -- 最后操作人(登录账号，自动写)
  source_id      INT          NULL,                 -- 数据源(registrars.id)，标来源
  protected      TINYINT      NOT NULL DEFAULT 0,
  created_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_domain (domain_ci_id),
  KEY idx_host (host)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 厂商原始 DNS 记录缓存（DNS 记录页展示用，同步时全量刷新）
CREATE TABLE IF NOT EXISTS dns_records (
  id           BIGINT AUTO_INCREMENT PRIMARY KEY,
  domain_ci_id BIGINT       NOT NULL,
  type         VARCHAR(16)  NOT NULL,
  name         VARCHAR(255) NOT NULL,
  data         VARCHAR(1024) NOT NULL DEFAULT '',
  ttl          INT          NOT NULL DEFAULT 3600,
  priority     INT          NULL,
  protected    TINYINT      NOT NULL DEFAULT 0,     -- _acme-challenge / NS 等受保护
  source_id    INT          NULL,
  synced_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_domain (domain_ci_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 预置常见 CDN（用户可在基础配置增删）
INSERT IGNORE INTO cdns (name, sort_order) VALUES
  ('阿里云CDN', 1), ('腾讯云CDN', 2), ('Cloudflare', 3), ('AWS CloudFront', 4), ('七牛', 5);

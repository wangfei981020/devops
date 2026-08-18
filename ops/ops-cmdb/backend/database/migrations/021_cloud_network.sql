-- 云网络资源（多云预留 provider；当前只 GCP）：VPC / 子网 / 防火墙 / 静态IP / 负载均衡
CREATE TABLE IF NOT EXISTS cloud_networks (
  id               INT AUTO_INCREMENT PRIMARY KEY,
  provider         VARCHAR(32)  NOT NULL DEFAULT 'gcp',
  cloud_account_id INT          NOT NULL,
  project          VARCHAR(128) NOT NULL,
  name             VARCHAR(128) NOT NULL,
  mode             VARCHAR(16)  NOT NULL DEFAULT '',   -- auto/custom
  self_link        VARCHAR(512) NOT NULL DEFAULT '',
  stale            TINYINT      NOT NULL DEFAULT 0,
  synced_at        DATETIME     NULL,
  UNIQUE KEY uk_net (cloud_account_id, project, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cloud_subnets (
  id               INT AUTO_INCREMENT PRIMARY KEY,
  provider         VARCHAR(32)  NOT NULL DEFAULT 'gcp',
  cloud_account_id INT          NOT NULL,
  project          VARCHAR(128) NOT NULL,
  name             VARCHAR(128) NOT NULL,
  network          VARCHAR(128) NOT NULL DEFAULT '',   -- 所属 VPC 名
  region           VARCHAR(64)  NOT NULL DEFAULT '',
  cidr             VARCHAR(64)  NOT NULL DEFAULT '',
  gateway          VARCHAR(64)  NOT NULL DEFAULT '',
  self_link        VARCHAR(512) NOT NULL DEFAULT '',
  stale            TINYINT      NOT NULL DEFAULT 0,
  synced_at        DATETIME     NULL,
  UNIQUE KEY uk_subnet (cloud_account_id, project, region, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cloud_firewalls (
  id               INT AUTO_INCREMENT PRIMARY KEY,
  provider         VARCHAR(32)  NOT NULL DEFAULT 'gcp',
  cloud_account_id INT          NOT NULL,
  project          VARCHAR(128) NOT NULL,
  name             VARCHAR(128) NOT NULL,
  network          VARCHAR(128) NOT NULL DEFAULT '',
  direction        VARCHAR(16)  NOT NULL DEFAULT '',   -- INGRESS/EGRESS
  priority         INT          NOT NULL DEFAULT 1000,
  action           VARCHAR(16)  NOT NULL DEFAULT '',   -- allow/deny
  protocols        VARCHAR(512) NOT NULL DEFAULT '',   -- "tcp:22,80;udp:53"
  source_ranges    VARCHAR(512) NOT NULL DEFAULT '',
  target_tags      VARCHAR(512) NOT NULL DEFAULT '',
  disabled         TINYINT      NOT NULL DEFAULT 0,
  high_risk        TINYINT      NOT NULL DEFAULT 0,     -- 0.0.0.0/0 放行敏感端口
  self_link        VARCHAR(512) NOT NULL DEFAULT '',
  stale            TINYINT      NOT NULL DEFAULT 0,
  synced_at        DATETIME     NULL,
  UNIQUE KEY uk_fw (cloud_account_id, project, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cloud_addresses (
  id               INT AUTO_INCREMENT PRIMARY KEY,
  provider         VARCHAR(32)  NOT NULL DEFAULT 'gcp',
  cloud_account_id INT          NOT NULL,
  project          VARCHAR(128) NOT NULL,
  name             VARCHAR(128) NOT NULL,
  address          VARCHAR(64)  NOT NULL DEFAULT '',
  addr_type        VARCHAR(16)  NOT NULL DEFAULT '',   -- EXTERNAL/INTERNAL
  status           VARCHAR(32)  NOT NULL DEFAULT '',   -- IN_USE/RESERVED
  region           VARCHAR(64)  NOT NULL DEFAULT '',   -- 'global' 或区域
  users            VARCHAR(512) NOT NULL DEFAULT '',   -- 绑定的资源（逗号）
  self_link        VARCHAR(512) NOT NULL DEFAULT '',
  stale            TINYINT      NOT NULL DEFAULT 0,
  synced_at        DATETIME     NULL,
  UNIQUE KEY uk_addr (cloud_account_id, project, region, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cloud_loadbalancers (
  id               INT AUTO_INCREMENT PRIMARY KEY,
  provider         VARCHAR(32)  NOT NULL DEFAULT 'gcp',
  cloud_account_id INT          NOT NULL,
  project          VARCHAR(128) NOT NULL,
  name             VARCHAR(128) NOT NULL,
  scheme           VARCHAR(32)  NOT NULL DEFAULT '',   -- EXTERNAL/INTERNAL/...
  vip              VARCHAR(64)  NOT NULL DEFAULT '',
  port_range       VARCHAR(64)  NOT NULL DEFAULT '',
  protocol         VARCHAR(32)  NOT NULL DEFAULT '',
  target           VARCHAR(256) NOT NULL DEFAULT '',   -- 目标代理/池
  region           VARCHAR(64)  NOT NULL DEFAULT '',   -- 'global' 或区域
  self_link        VARCHAR(512) NOT NULL DEFAULT '',
  stale            TINYINT      NOT NULL DEFAULT 0,
  synced_at        DATETIME     NULL,
  UNIQUE KEY uk_lb (cloud_account_id, project, region, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

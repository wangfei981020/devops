-- CDN（Cloudflare）只读接入。
--
-- 在此之前 CDN 是整条链路上唯一的黑洞：cdns 表只是厂商字典、domain_records.cdn_id
-- 只是人工标注「这个域名走 CF」，至于 CF 上到底怎么配的、解析到哪、SSL 什么模式，
-- 一概查不到，只能登录 Cloudflare 控制台看。域名类故障排查因此永远缺最前面一跳。
--
-- 只读：这里存的是采集回来的镜像，CMDB 不回写 Cloudflare。

-- CDN 账号（API Token 加密存储，与 registrars/cloud_accounts 同一套做法）
CREATE TABLE IF NOT EXISTS cdn_accounts (
  id           INT PRIMARY KEY AUTO_INCREMENT,
  cdn_id       INT NOT NULL,                       -- 关联 cdns 厂商字典
  name         VARCHAR(128) NOT NULL,
  cred_enc     TEXT,                               -- API Token（只读权限即可）
  account_tag  VARCHAR(64) NOT NULL DEFAULT '',    -- Cloudflare Account ID，可留空
  enabled      TINYINT NOT NULL DEFAULT 1,
  last_sync_at DATETIME NULL,
  last_result  VARCHAR(255) NOT NULL DEFAULT '',
  created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_cdn_acc (cdn_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Zone（一个 zone 对应一个根域名）
CREATE TABLE IF NOT EXISTS cdn_zones (
  id           INT PRIMARY KEY AUTO_INCREMENT,
  account_id   INT NOT NULL,
  zone_id      VARCHAR(64) NOT NULL,
  name         VARCHAR(255) NOT NULL,              -- 根域名，用于关联 domains.name
  status       VARCHAR(32) NOT NULL DEFAULT '',    -- active/pending/moved…
  paused       TINYINT NOT NULL DEFAULT 0,
  plan         VARCHAR(64) NOT NULL DEFAULT '',
  name_servers VARCHAR(512) NOT NULL DEFAULT '',   -- CF 分配的 NS，与注册商实际 NS 比对可发现"配了没生效"
  synced_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_zone (account_id, zone_id),
  KEY idx_zone_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- DNS 记录。proxied=1 即「橙云」，流量经 CF 转发；=0 是「灰云」，直连源站。
-- 这个字段决定了 CDN 到底有没有生效，也是排查"改了 CF 配置却没效果"的第一落点。
CREATE TABLE IF NOT EXISTS cdn_dns_records (
  id         INT PRIMARY KEY AUTO_INCREMENT,
  account_id INT NOT NULL,
  zone_id    VARCHAR(64) NOT NULL,
  zone_name  VARCHAR(255) NOT NULL,
  record_id  VARCHAR(64) NOT NULL,
  type       VARCHAR(16) NOT NULL DEFAULT '',
  name       VARCHAR(255) NOT NULL DEFAULT '',     -- 完整 FQDN
  content    VARCHAR(512) NOT NULL DEFAULT '',     -- 解析目标(IP/CNAME)
  proxied    TINYINT NOT NULL DEFAULT 0,
  ttl        INT NOT NULL DEFAULT 0,
  synced_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_rec (account_id, record_id),
  KEY idx_rec_zone (account_id, zone_id),
  KEY idx_rec_name (name),
  KEY idx_rec_content (content)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- Zone 级设置（KV 存，CF 的 settings 接口本身就是列表形态）。
-- 重点关注 ssl（flexible 表示 CF 到源站是明文，是常见的错误配置）、
-- always_use_https、min_tls_version。
CREATE TABLE IF NOT EXISTS cdn_zone_settings (
  id         INT PRIMARY KEY AUTO_INCREMENT,
  account_id INT NOT NULL,
  zone_id    VARCHAR(64) NOT NULL,
  zone_name  VARCHAR(255) NOT NULL,
  name       VARCHAR(64) NOT NULL,
  value      VARCHAR(512) NOT NULL DEFAULT '',
  synced_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_setting (account_id, zone_id, name),
  KEY idx_setting_zone (account_id, zone_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

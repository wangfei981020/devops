-- GCP IAM 权限绑定 + Cloud DNS
--
-- 补 CMDB-003 第 5 项里剩下的两块：
--   IAM      —— 「谁对这个项目有什么权限」此前完全不可见，权限审计全靠登控制台
--   Cloud DNS —— 与 Cloudflare 的解析并存时，两边不一致会导致「改了没生效」，
--                此前只能人工两头对
--
-- 两者都是只读 API（getIamPolicy / managedZones.list），不需要写权限。

CREATE TABLE IF NOT EXISTS cloud_iam_bindings (
  id               BIGINT AUTO_INCREMENT PRIMARY KEY,
  cloud_account_id INT          NOT NULL,
  project          VARCHAR(128) NOT NULL,
  role             VARCHAR(255) NOT NULL COMMENT '如 roles/owner、roles/viewer',
  member_type      VARCHAR(32)  NOT NULL COMMENT 'user/serviceAccount/group/domain/allUsers/...',
  member           VARCHAR(512) NOT NULL COMMENT '成员标识，已去掉类型前缀',
  severity         VARCHAR(16)  NOT NULL DEFAULT '' COMMENT '风险等级，空=无风险',
  issue            VARCHAR(512) NOT NULL DEFAULT '',
  synced_at        DATETIME     NOT NULL,
  KEY idx_iam_acct (cloud_account_id, project),
  KEY idx_iam_sev (severity)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='GCP 项目 IAM 权限绑定（只读）';

CREATE TABLE IF NOT EXISTS cloud_dns_zones (
  id               BIGINT AUTO_INCREMENT PRIMARY KEY,
  cloud_account_id INT          NOT NULL,
  project          VARCHAR(128) NOT NULL,
  zone_name        VARCHAR(128) NOT NULL COMMENT 'GCP 里的托管区名',
  dns_name         VARCHAR(255) NOT NULL COMMENT '根域名，带尾点',
  visibility       VARCHAR(16)  NOT NULL DEFAULT '' COMMENT 'public/private',
  name_servers     TEXT,
  record_count     INT          NOT NULL DEFAULT 0,
  synced_at        DATETIME     NOT NULL,
  KEY idx_dnsz_acct (cloud_account_id, project)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='GCP Cloud DNS 托管区';

CREATE TABLE IF NOT EXISTS cloud_dns_records (
  id               BIGINT AUTO_INCREMENT PRIMARY KEY,
  cloud_account_id INT          NOT NULL,
  project          VARCHAR(128) NOT NULL,
  zone_name        VARCHAR(128) NOT NULL,
  name             VARCHAR(255) NOT NULL,
  type             VARCHAR(16)  NOT NULL,
  ttl              INT          NOT NULL DEFAULT 0,
  rrdatas          TEXT         COMMENT '解析目标，多值用逗号分隔',
  synced_at        DATETIME     NOT NULL,
  KEY idx_dnsr_acct (cloud_account_id, project),
  KEY idx_dnsr_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='GCP Cloud DNS 解析记录';

-- 项目 / 环境 独立维护（录入域名/证书时下拉选）
CREATE TABLE IF NOT EXISTS projects (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  name       VARCHAR(128) NOT NULL UNIQUE,
  remark     VARCHAR(255) NOT NULL DEFAULT '',
  sort_order INT          NOT NULL DEFAULT 0,
  created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS environments (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  code       VARCHAR(32)  NOT NULL UNIQUE,
  name       VARCHAR(64)  NOT NULL DEFAULT '',
  tag_type   VARCHAR(16)  NOT NULL DEFAULT 'info',
  sort_order INT          NOT NULL DEFAULT 0,
  created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO environments (code, name, tag_type, sort_order) VALUES
  ('PROD', '生产', 'danger', 1),
  ('UAT', 'UAT', 'warning', 2),
  ('TEST', '测试', 'primary', 3),
  ('DEV', '开发', 'success', 4);

-- 域名上记录「线上 HTTPS 证书」到期（与域名注册到期 expiry_at 区分）
ALTER TABLE domains ADD COLUMN cert_expiry_at DATETIME NULL;
ALTER TABLE domains ADD COLUMN cert_check_at  DATETIME NULL;
ALTER TABLE domains ADD COLUMN cert_check_msg VARCHAR(255) NOT NULL DEFAULT '';

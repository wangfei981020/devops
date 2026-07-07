-- 凭据下沉到 project 级：一个云账号(分组) 下多个 project，每 project 自己的自定义名 + 独立 SA 凭据。
CREATE TABLE IF NOT EXISTS cloud_account_projects (
  id           INT AUTO_INCREMENT PRIMARY KEY,
  account_id   INT          NOT NULL,
  name         VARCHAR(128) NOT NULL DEFAULT '',   -- 自定义业务名（生产/测试），主机列表显示这个
  project_id   VARCHAR(128) NOT NULL,              -- GCP project id（不可变）
  cred_enc     TEXT,                               -- 该 project 独立的 service account JSON（AES 加密）
  last_sync_at DATETIME     NULL,
  last_result  VARCHAR(255) NOT NULL DEFAULT '',
  created_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_account (account_id),
  UNIQUE KEY uk_account_project (account_id, project_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 迁移旧数据：把 cloud_accounts.projects(逗号 project id) 拆成 project 行，各沿用账号原凭据、名默认=project id。
INSERT IGNORE INTO cloud_account_projects (account_id, name, project_id, cred_enc)
SELECT a.id, n.p, n.p, a.cred_enc
FROM cloud_accounts a
JOIN (
  SELECT DISTINCT ca.id AS aid,
    TRIM(SUBSTRING_INDEX(SUBSTRING_INDEX(ca.projects, ',', num.n), ',', -1)) AS p
  FROM cloud_accounts ca
  JOIN (SELECT 1 n UNION SELECT 2 UNION SELECT 3 UNION SELECT 4 UNION SELECT 5
        UNION SELECT 6 UNION SELECT 7 UNION SELECT 8 UNION SELECT 9 UNION SELECT 10) num
    ON num.n <= 1 + LENGTH(ca.projects) - LENGTH(REPLACE(ca.projects, ',', ''))
) n ON n.aid = a.id
WHERE n.p <> '';

-- 主机同步接入定时任务（默认每天 03:00 一次）
INSERT IGNORE INTO scheduled_tasks (task_key, name, enabled, schedule)
VALUES ('host_sync', '主机同步（云资源）', 1, '0 3 * * *');

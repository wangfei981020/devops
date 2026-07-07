-- 定时任务重做：每任务可选 Lark 群 + @人 + 通知时机；单 webhook 升级为多群
CREATE TABLE IF NOT EXISTS lark_groups (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  name       VARCHAR(128)  NOT NULL,
  webhook    VARCHAR(512)  NOT NULL,
  created_at DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 任务↔@人 关联（多对多）
CREATE TABLE IF NOT EXISTS task_notify_users (
  task_key VARCHAR(64) NOT NULL,
  user_id  INT         NOT NULL,
  PRIMARY KEY (task_key, user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE scheduled_tasks ADD COLUMN notify_enabled TINYINT NOT NULL DEFAULT 1;
ALTER TABLE scheduled_tasks ADD COLUMN lark_group_id INT NULL;
ALTER TABLE scheduled_tasks ADD COLUMN notify_when VARCHAR(16) NOT NULL DEFAULT 'always';
ALTER TABLE scheduled_tasks ADD COLUMN last_ok TINYINT NOT NULL DEFAULT 1;

-- 任务改清晰名（域名 / 证书 区分开，去证书重叠）
UPDATE scheduled_tasks SET name='域名注册到期刷新（WHOIS）' WHERE task_key='refresh_expiry';
UPDATE scheduled_tasks SET name='证书到期检测（443）' WHERE task_key='inspect';
UPDATE scheduled_tasks SET name='证书自动续期（ACME）' WHERE task_key='auto_renew';
UPDATE scheduled_tasks SET name='到期提醒推送（证书/域名）' WHERE task_key='remind';

-- 新增 DNS 记录同步任务（默认每天一次）
INSERT IGNORE INTO scheduled_tasks (task_key, name, enabled, schedule) VALUES ('dns_sync', 'DNS 记录同步（数据源）', 1, '0 3 * * *');

-- 迁移：原单 feishu_webhook 存为默认群，旧任务全指向它
INSERT INTO lark_groups (name, webhook) SELECT '默认群', v FROM settings WHERE k='feishu_webhook' AND v<>'' LIMIT 1;
UPDATE scheduled_tasks SET lark_group_id=(SELECT id FROM lark_groups ORDER BY id LIMIT 1) WHERE lark_group_id IS NULL;

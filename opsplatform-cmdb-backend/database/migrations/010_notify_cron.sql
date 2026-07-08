-- 可配置定时任务（cron 调度，前端可开关 / 改频率 / 立即运行）
CREATE TABLE IF NOT EXISTS scheduled_tasks (
  task_key    VARCHAR(64)  NOT NULL PRIMARY KEY,
  name        VARCHAR(128) NOT NULL,
  enabled     TINYINT      NOT NULL DEFAULT 1,
  schedule    VARCHAR(64)  NOT NULL,
  last_run_at DATETIME     NULL,
  last_result VARCHAR(255) NOT NULL DEFAULT '',
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 初始 4 个任务（cron 5 字段：分 时 日 月 周）。巡检默认关（逐条 443 较重）。
INSERT IGNORE INTO scheduled_tasks (task_key, name, enabled, schedule) VALUES
  ('refresh_expiry', '刷新到期时间', 1, '0 3,15 * * *'),
  ('auto_renew',     '证书自动续期', 1, '0 3 * * *'),
  ('remind',         '到期提醒推送', 1, '0 9 * * *'),
  ('inspect',        '检测所有解析证书', 0, '0 4 * * *');

-- 通知人（@ 特定责任人，存飞书 open_id）
CREATE TABLE IF NOT EXISTS notify_users (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  name       VARCHAR(64)  NOT NULL,
  open_id    VARCHAR(128) NOT NULL,
  enabled    TINYINT      NOT NULL DEFAULT 1,
  created_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

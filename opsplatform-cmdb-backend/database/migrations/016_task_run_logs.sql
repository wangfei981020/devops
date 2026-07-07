-- 定时任务执行历史（每次跑都存一条，不覆盖 scheduled_tasks.last_*）：三态 + 失败明细 + 通知投递
CREATE TABLE IF NOT EXISTS task_run_logs (
  id           BIGINT       AUTO_INCREMENT PRIMARY KEY,
  task_key     VARCHAR(64)  NOT NULL,
  name         VARCHAR(128) NOT NULL,
  status       VARCHAR(16)  NOT NULL DEFAULT 'ok',   -- ok=成功 / partial=部分成功 / fail=整体失败
  summary      VARCHAR(255) NOT NULL DEFAULT '',     -- 结果摘要（照搬 Lark 那句话）
  failures     TEXT         NULL,                    -- 失败明细，JSON 数组：[{"target":"x.cn","reason":"WHOIS超时"}]
  trigger_by   VARCHAR(16)  NOT NULL DEFAULT 'cron', -- cron=定时 / manual=手动立即运行
  duration_ms  INT          NOT NULL DEFAULT 0,
  notify_state VARCHAR(16)  NOT NULL DEFAULT 'none', -- sent=已送达 / failed=Lark报错 / skipped=按配置不发 / none=未配置
  notify_group VARCHAR(128) NOT NULL DEFAULT '',     -- 推送到的 Lark 群名
  notify_at    VARCHAR(255) NOT NULL DEFAULT '',     -- @的人（名字拼接）
  started_at   DATETIME     NOT NULL,
  finished_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_task (task_key, id),
  INDEX idx_finished (finished_at),
  INDEX idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

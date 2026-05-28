CREATE TABLE IF NOT EXISTS notify_users (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  name       VARCHAR(64) NOT NULL,
  lark_id    VARCHAR(128) NOT NULL,
  remark     VARCHAR(255),
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_lark_id (lark_id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS lark_webhooks (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  name       VARCHAR(64) NOT NULL,
  url        TEXT NOT NULL,
  remark     VARCHAR(255),
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS alert_rules (
  id                         INT AUTO_INCREMENT PRIMARY KEY,
  name                       VARCHAR(128) NOT NULL,
  target                     ENUM('cluster','nodepool') NOT NULL,
  versions_behind_threshold  INT NOT NULL,
  eol_days_threshold         INT NULL,
  cluster_ids                JSON,
  webhook_id                 INT NOT NULL,
  mention_user_ids           JSON,
  interval_minutes           INT NOT NULL DEFAULT 60,
  enabled                    TINYINT DEFAULT 1,
  created_at                 DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at                 DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (webhook_id) REFERENCES lark_webhooks(id)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS alert_history (
  id              INT AUTO_INCREMENT PRIMARY KEY,
  rule_id         INT NOT NULL,
  cluster_id      INT NOT NULL,
  nodepool_name   VARCHAR(64),
  versions_behind INT,
  trigger_time    DATETIME DEFAULT CURRENT_TIMESTAMP,
  status          ENUM('sent','failed') DEFAULT 'sent',
  lark_response   TEXT,
  INDEX idx_dedup (rule_id, cluster_id, nodepool_name, trigger_time)
) ENGINE=InnoDB;

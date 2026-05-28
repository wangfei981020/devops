CREATE TABLE IF NOT EXISTS version_history (
  id            INT AUTO_INCREMENT PRIMARY KEY,
  cluster_id    INT NOT NULL,
  nodepool_name VARCHAR(64),
  version       VARCHAR(64) NOT NULL,
  started_at    DATETIME NOT NULL,
  ended_at      DATETIME NULL,
  INDEX idx_lookup (cluster_id, nodepool_name, started_at),
  FOREIGN KEY (cluster_id) REFERENCES clusters(id) ON DELETE CASCADE
) ENGINE=InnoDB;

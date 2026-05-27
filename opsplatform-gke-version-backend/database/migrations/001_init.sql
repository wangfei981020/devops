CREATE DATABASE IF NOT EXISTS gke_version_monitor DEFAULT CHARSET utf8mb4;
USE gke_version_monitor;

CREATE TABLE IF NOT EXISTS clusters (
  id          INT AUTO_INCREMENT PRIMARY KEY,
  project_id  VARCHAR(64) NOT NULL,
  location    VARCHAR(32) NOT NULL,
  name        VARCHAR(64) NOT NULL,
  enabled     TINYINT DEFAULT 1,
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_cluster (project_id, location, name)
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS settings (
  k VARCHAR(64) PRIMARY KEY,
  v VARCHAR(255) NOT NULL,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS cluster_snapshots (
  cluster_id                          INT PRIMARY KEY,
  current_version                     VARCHAR(64),
  max_upgradable_version              VARCHAR(64),
  latest_available_version            VARCHAR(64),
  current_to_max_versions_behind      INT,
  current_to_max_version_diff         DECIMAL(20,7),
  max_to_latest_versions_behind       INT,
  max_to_latest_version_diff          DECIMAL(20,7),
  current_to_latest_versions_behind   INT,
  current_to_latest_version_diff      DECIMAL(20,7),
  std_support_end                     DATE,
  ext_support_end                     DATE,
  nodepools_json                      JSON,
  last_refreshed_at                   DATETIME,
  last_error                          TEXT,
  FOREIGN KEY (cluster_id) REFERENCES clusters(id) ON DELETE CASCADE
) ENGINE=InnoDB;

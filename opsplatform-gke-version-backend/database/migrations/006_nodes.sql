-- nodes 表：存每个集群下每个节点池的 VM 实例明细
-- 数据来源：GCP Compute API (Instances.Get) -> 取 creationTimestamp
-- 写入策略：每次 scrape 该集群时，事务内 DELETE + INSERT 全量覆盖
-- 目的：能在前端展示 kubectl get node 那种 AGE 列（节点级 + 节点池聚合）

CREATE TABLE IF NOT EXISTS nodes (
  id              INT AUTO_INCREMENT PRIMARY KEY,
  cluster_id      INT NOT NULL,
  nodepool_name   VARCHAR(128) NOT NULL,
  node_name       VARCHAR(255) NOT NULL,
  zone            VARCHAR(64)  NOT NULL,
  version         VARCHAR(64)  NOT NULL DEFAULT '',
  -- GCP VM 真实创建时间（不是我们 scrape 的时间）
  gcp_created_at  DATETIME     NOT NULL,
  -- 我们最后看到这个 VM 的时间，scraper 写入时刷新
  last_seen_at    DATETIME     NOT NULL,
  UNIQUE KEY uniq_cluster_node (cluster_id, node_name),
  INDEX idx_nodepool (cluster_id, nodepool_name),
  FOREIGN KEY (cluster_id) REFERENCES clusters(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

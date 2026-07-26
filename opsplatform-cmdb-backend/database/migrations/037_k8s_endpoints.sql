-- K8s 阶段4：Endpoints 打通 Service→Pod→Node，用于全链路关系与影响分析。
CREATE TABLE IF NOT EXISTS k8s_endpoints (
  id           BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id   INT          NOT NULL,
  namespace    VARCHAR(255) NOT NULL,
  service_name VARCHAR(255) NOT NULL,
  pod_name     VARCHAR(255) NOT NULL DEFAULT '',
  node_name    VARCHAR(255) NOT NULL DEFAULT '',
  synced_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_cluster (cluster_id),
  KEY idx_svc (cluster_id, namespace, service_name),
  KEY idx_node (cluster_id, node_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

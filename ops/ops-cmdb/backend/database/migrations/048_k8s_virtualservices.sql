-- Istio VirtualService（networking.istio.io）——用户入口用 Istio，不是标准 Ingress，单独采集展示。
CREATE TABLE IF NOT EXISTS k8s_virtualservices (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id INT          NOT NULL,
  namespace  VARCHAR(128) NOT NULL DEFAULT '',
  name       VARCHAR(255) NOT NULL DEFAULT '',
  hosts      VARCHAR(1024) NOT NULL DEFAULT '',
  gateways   VARCHAR(512) NOT NULL DEFAULT '',
  backends   VARCHAR(512) NOT NULL DEFAULT '',
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

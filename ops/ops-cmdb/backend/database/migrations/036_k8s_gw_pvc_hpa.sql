-- K8s 阶段2 补全：Gateway API(CRD, dynamic) + PVC + HPA。（Secret/ConfigMap 用户暂不做）

CREATE TABLE IF NOT EXISTS k8s_gateways (
  id            BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id    INT           NOT NULL,
  namespace     VARCHAR(255)  NOT NULL,
  name          VARCHAR(255)  NOT NULL,
  gateway_class VARCHAR(128)  NOT NULL DEFAULT '',
  listeners     VARCHAR(512)  NOT NULL DEFAULT '',   -- name:port/protocol 摘要
  addresses     VARCHAR(512)  NOT NULL DEFAULT '',
  synced_at     DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_gw (cluster_id, namespace, name),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS k8s_httproutes (
  id          BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id  INT           NOT NULL,
  namespace   VARCHAR(255)  NOT NULL,
  name        VARCHAR(255)  NOT NULL,
  hostnames   VARCHAR(1024) NOT NULL DEFAULT '',
  parents     VARCHAR(512)  NOT NULL DEFAULT '',      -- 挂到哪些 Gateway
  backends    VARCHAR(512)  NOT NULL DEFAULT '',      -- 后端 service
  synced_at   DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_route (cluster_id, namespace, name),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS k8s_pvcs (
  id            BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id    INT          NOT NULL,
  namespace     VARCHAR(255) NOT NULL,
  name          VARCHAR(255) NOT NULL,
  status        VARCHAR(32)  NOT NULL DEFAULT '',
  capacity      VARCHAR(32)  NOT NULL DEFAULT '',
  storage_class VARCHAR(128) NOT NULL DEFAULT '',
  volume_name   VARCHAR(255) NOT NULL DEFAULT '',
  synced_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_pvc (cluster_id, namespace, name),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS k8s_hpas (
  id               BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id       INT          NOT NULL,
  namespace        VARCHAR(255) NOT NULL,
  name             VARCHAR(255) NOT NULL,
  target_kind      VARCHAR(32)  NOT NULL DEFAULT '',
  target_name      VARCHAR(255) NOT NULL DEFAULT '',
  min_replicas     INT          NOT NULL DEFAULT 0,
  max_replicas     INT          NOT NULL DEFAULT 0,
  current_replicas INT          NOT NULL DEFAULT 0,
  synced_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_hpa (cluster_id, namespace, name),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

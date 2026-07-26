-- K8s 阶段2：资源纳管（只读镜像，每次同步全量 delete+insert 该集群该资源）。

-- 节点池标签 key：GKE 默认 cloud.google.com/gke-nodepool；IDC 自管填自定义 label；空=按角色/default 兜底
ALTER TABLE k8s_clusters ADD COLUMN nodepool_label VARCHAR(128) NOT NULL DEFAULT '' AFTER endpoint;

CREATE TABLE IF NOT EXISTS k8s_node_pools (
  id           INT AUTO_INCREMENT PRIMARY KEY,
  cluster_id   INT          NOT NULL,
  name         VARCHAR(128) NOT NULL,
  machine_type VARCHAR(64)  NOT NULL DEFAULT '',
  node_count   INT          NOT NULL DEFAULT 0,
  version      VARCHAR(32)  NOT NULL DEFAULT '',
  updated_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_pool (cluster_id, name),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS k8s_nodes (
  id              BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id      INT          NOT NULL,
  name            VARCHAR(255) NOT NULL,
  pool            VARCHAR(128) NOT NULL DEFAULT '',
  internal_ip     VARCHAR(64)  NOT NULL DEFAULT '',
  roles           VARCHAR(128) NOT NULL DEFAULT '',
  machine_type    VARCHAR(64)  NOT NULL DEFAULT '',
  cpu_cap         VARCHAR(32)  NOT NULL DEFAULT '',
  mem_cap         VARCHAR(32)  NOT NULL DEFAULT '',
  os_image        VARCHAR(128) NOT NULL DEFAULT '',
  kubelet_version VARCHAR(32)  NOT NULL DEFAULT '',
  ready_status    VARCHAR(16)  NOT NULL DEFAULT '',   -- Ready/NotReady/Unknown
  last_heartbeat  DATETIME     NULL,
  conditions      VARCHAR(255) NOT NULL DEFAULT '',   -- 压力位摘要(MemoryPressure 等)
  pod_count       INT          NOT NULL DEFAULT 0,
  stuck           TINYINT      NOT NULL DEFAULT 0,     -- 卡死(阶段3 判定用，阶段2 先留)
  synced_at       DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_node (cluster_id, name),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS k8s_namespaces (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id INT          NOT NULL,
  name       VARCHAR(255) NOT NULL,
  phase      VARCHAR(32)  NOT NULL DEFAULT '',
  synced_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_ns (cluster_id, name),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS k8s_workloads (
  id               BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id       INT          NOT NULL,
  namespace        VARCHAR(255) NOT NULL,
  kind             VARCHAR(24)  NOT NULL,            -- Deployment/StatefulSet/DaemonSet/CronJob/Job
  name             VARCHAR(255) NOT NULL,
  replicas_desired INT          NOT NULL DEFAULT 0,
  replicas_ready   INT          NOT NULL DEFAULT 0,
  image            VARCHAR(512) NOT NULL DEFAULT '',
  image_tag        VARCHAR(128) NOT NULL DEFAULT '',
  status           VARCHAR(32)  NOT NULL DEFAULT '',
  synced_at        DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_wl (cluster_id, namespace, kind, name),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS k8s_pods (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id INT          NOT NULL,
  namespace  VARCHAR(255) NOT NULL,
  name       VARCHAR(255) NOT NULL,
  node_name  VARCHAR(255) NOT NULL DEFAULT '',
  workload   VARCHAR(255) NOT NULL DEFAULT '',
  phase      VARCHAR(24)  NOT NULL DEFAULT '',
  restarts   INT          NOT NULL DEFAULT 0,
  pod_ip     VARCHAR(64)  NOT NULL DEFAULT '',
  start_time DATETIME     NULL,
  synced_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_pod (cluster_id, namespace, name),
  KEY idx_cluster (cluster_id),
  KEY idx_node (cluster_id, node_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS k8s_services (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id INT          NOT NULL,
  namespace  VARCHAR(255) NOT NULL,
  name       VARCHAR(255) NOT NULL,
  type       VARCHAR(32)  NOT NULL DEFAULT '',
  cluster_ip VARCHAR(64)  NOT NULL DEFAULT '',
  ports      VARCHAR(255) NOT NULL DEFAULT '',
  synced_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_svc (cluster_id, namespace, name),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS k8s_ingresses (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id INT           NOT NULL,
  namespace  VARCHAR(255)  NOT NULL,
  name       VARCHAR(255)  NOT NULL,
  hosts      VARCHAR(1024) NOT NULL DEFAULT '',
  tls        VARCHAR(512)  NOT NULL DEFAULT '',
  svc_names  VARCHAR(512)  NOT NULL DEFAULT '',   -- 后端 service 名(建关系用)
  synced_at  DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_ing (cluster_id, namespace, name),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

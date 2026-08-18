-- K8s 模块阶段1：集群纳管（多集群只读）+ 采集健康自监控
-- 凭据(kubeconfig / GCP SA key)一律 AES 加密存(复用 CMDB crypto)，不同于 k8sinsight 的明文存。

CREATE TABLE IF NOT EXISTS k8s_clusters (
  id             INT AUTO_INCREMENT PRIMARY KEY,
  name           VARCHAR(128) NOT NULL,                       -- 集群名(GKE 集群名 或 自定义标识)
  display_name   VARCHAR(128) NOT NULL DEFAULT '',            -- 展示名
  environment    VARCHAR(16)  NOT NULL DEFAULT 'DEV',         -- PROD/UAT/TEST/DEV(原样，不加修饰)
  provider       VARCHAR(16)  NOT NULL DEFAULT 'gke',         -- gke / generic / in-cluster
  project_id     VARCHAR(128) NOT NULL DEFAULT '',            -- GKE 云项目(接主机/成本模块)
  location       VARCHAR(64)  NOT NULL DEFAULT '',            -- region/zone
  endpoint       VARCHAR(255) NOT NULL DEFAULT '',            -- apiserver 地址(展示用，只连它不连节点)
  sa_key_enc     TEXT,                                        -- GCP 只读 SA key(container.viewer)，AES 加密
  kubeconfig_enc MEDIUMTEXT,                                  -- client-go 只读 kubeconfig(view SA)，AES 加密
  enabled        TINYINT      NOT NULL DEFAULT 1,
  created_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_name_env (name, environment)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS k8s_sync_state (
  id          BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id  INT          NOT NULL,
  resource    VARCHAR(32)  NOT NULL,                          -- nodes/pods/workloads/services/ingresses...
  last_sync   DATETIME,
  ok          TINYINT      NOT NULL DEFAULT 0,
  err         VARCHAR(512) NOT NULL DEFAULT '',
  duration_ms INT          NOT NULL DEFAULT 0,
  count       INT          NOT NULL DEFAULT 0,                -- 本次采集到的对象数
  UNIQUE KEY uniq_cluster_res (cluster_id, resource),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- K8s 阶段5：工作负载变更追溯（镜像/副本变化，同步时 diff 检出）。
-- 注：CMDB 经 list 采集，看不到"谁"改的(集群侧审计另说)，source 记为 sync-detected。
CREATE TABLE IF NOT EXISTS k8s_changes (
  id          BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id  INT          NOT NULL,
  namespace   VARCHAR(255) NOT NULL,
  kind        VARCHAR(24)  NOT NULL,
  name        VARCHAR(255) NOT NULL,
  field       VARCHAR(32)  NOT NULL,          -- image / replicas
  old_value   VARCHAR(512) NOT NULL DEFAULT '',
  new_value   VARCHAR(512) NOT NULL DEFAULT '',
  source      VARCHAR(32)  NOT NULL DEFAULT 'sync-detected',
  changed_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_cluster (cluster_id),
  KEY idx_wl (cluster_id, namespace, kind, name),
  KEY idx_at (changed_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

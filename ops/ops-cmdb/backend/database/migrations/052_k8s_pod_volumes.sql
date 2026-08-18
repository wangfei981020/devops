-- Pod 与 PVC 的挂载关系。没有它就无法回答「这块盘还有人用吗」——
-- 之前只能靠「Prometheus 取不到使用率指标」反推未挂载，逻辑脆弱且依赖外部监控。
-- 有了这张表，孤儿 PVC（在计费但无人挂载）可以纯 SQL 查出来。
CREATE TABLE IF NOT EXISTS k8s_pod_volumes (
  id         BIGINT PRIMARY KEY AUTO_INCREMENT,
  cluster_id INT NOT NULL,
  namespace  VARCHAR(255) NOT NULL,
  pod_name   VARCHAR(255) NOT NULL,
  pvc_name   VARCHAR(255) NOT NULL,
  synced_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_pv_cluster (cluster_id),
  KEY idx_pv_pvc (cluster_id, namespace, pvc_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

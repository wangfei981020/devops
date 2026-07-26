-- 成本阶段1：Pod 资源 request/limit（展示 + 后续成本分摊基准）+ 命名空间→项目映射。
-- CPU 存毫核(millicores)，内存存 MiB，便于聚合；展示时前端换算。
ALTER TABLE k8s_pods ADD COLUMN cpu_req_m   INT NOT NULL DEFAULT 0 AFTER phase;
ALTER TABLE k8s_pods ADD COLUMN mem_req_mi  INT NOT NULL DEFAULT 0 AFTER cpu_req_m;
ALTER TABLE k8s_pods ADD COLUMN cpu_lim_m   INT NOT NULL DEFAULT 0 AFTER mem_req_mi;
ALTER TABLE k8s_pods ADD COLUMN mem_lim_mi  INT NOT NULL DEFAULT 0 AFTER cpu_lim_m;

-- 命名空间 → 项目 映射（一个项目可挂多个命名空间；多项目共享集群时按此拆费用）
CREATE TABLE IF NOT EXISTS k8s_ns_project (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  cluster_id INT          NOT NULL,
  namespace  VARCHAR(255) NOT NULL,
  project    VARCHAR(255) NOT NULL DEFAULT '',
  updated_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_ns (cluster_id, namespace),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

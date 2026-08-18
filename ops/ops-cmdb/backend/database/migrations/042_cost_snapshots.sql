-- 成本阶段3：月度成本快照（工作负载/主机/PVC 粒度，留存以支持环比归因 + 月/季/年报告）。
CREATE TABLE IF NOT EXISTS cost_snapshots (
  id           BIGINT AUTO_INCREMENT PRIMARY KEY,
  month        CHAR(7)      NOT NULL,              -- YYYY-MM
  cluster      VARCHAR(128) NOT NULL DEFAULT '',
  mode         VARCHAR(16)  NOT NULL DEFAULT '',   -- cloud/idc
  gcp_project  VARCHAR(128) NOT NULL DEFAULT '',
  biz_project  VARCHAR(255) NOT NULL DEFAULT '',
  env          VARCHAR(32)  NOT NULL DEFAULT '',
  type         VARCHAR(24)  NOT NULL DEFAULT '',   -- k8s_compute/k8s_storage/traditional
  resource_key VARCHAR(300) NOT NULL DEFAULT '',   -- 命名空间/工作负载 或 /主机名 或 命名空间/pvc
  spec         VARCHAR(255) NOT NULL DEFAULT '',   -- 规格摘要(副本/镜像tag/机型/容量)，归因用
  cost         DECIMAL(12,2) NOT NULL DEFAULT 0,
  taken_at     DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq (month, cluster, type, resource_key),
  KEY idx_month (month)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 成本费率分档：按「区域 × 机型族」定 vCPU/内存单价，按「区域 × 磁盘类型」定磁盘单价。
-- note 标注来源：official=官方确认，estimate=保守溢价估算待核对，fallback=兜底。
CREATE TABLE IF NOT EXISTS cloud_compute_rates (
  id              INT AUTO_INCREMENT PRIMARY KEY,
  provider        VARCHAR(32)  NOT NULL DEFAULT 'gcp',
  region          VARCHAR(64)  NOT NULL,           -- 区域，或 'default' 兜底
  machine_family  VARCHAR(32)  NOT NULL,           -- e2/n2/c2/custom...，或 'default' 兜底
  vcpu_hour_usd   DECIMAL(12,6) NOT NULL DEFAULT 0,
  ram_gb_hour_usd DECIMAL(12,6) NOT NULL DEFAULT 0,
  note            VARCHAR(32)  NOT NULL DEFAULT '',
  UNIQUE KEY uk_compute (provider, region, machine_family)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS cloud_disk_rates (
  id           INT AUTO_INCREMENT PRIMARY KEY,
  provider     VARCHAR(32)  NOT NULL DEFAULT 'gcp',
  region       VARCHAR(64)  NOT NULL,              -- 区域，或 'default'
  disk_type    VARCHAR(32)  NOT NULL,              -- pd-ssd/pd-balanced/pd-standard，或 'default'
  gb_month_usd DECIMAL(12,6) NOT NULL DEFAULT 0,
  note         VARCHAR(32)  NOT NULL DEFAULT '',
  UNIQUE KEY uk_disk (provider, region, disk_type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 计算费率种子（USD/小时，on-demand 目录价）
-- us-central1：官方确认（e2/n2 已用实例价反算校验；c2/custom 官方目录价）
-- asia-east1 ×1.15、asia-east2 ×1.25：保守溢价估算，待从 GCP 控制台核对
INSERT IGNORE INTO cloud_compute_rates (provider,region,machine_family,vcpu_hour_usd,ram_gb_hour_usd,note) VALUES
('gcp','default','default',    0.031611,0.004237,'fallback'),
('gcp','us-central1','e2',     0.021811,0.002923,'official'),
('gcp','us-central1','n2',     0.031611,0.004237,'official'),
('gcp','us-central1','c2',     0.033908,0.004539,'official'),
('gcp','us-central1','custom', 0.033174,0.004446,'official'),
('gcp','asia-east1','e2',      0.025083,0.003361,'estimate'),
('gcp','asia-east1','n2',      0.036353,0.004873,'estimate'),
('gcp','asia-east1','c2',      0.038994,0.005220,'estimate'),
('gcp','asia-east1','custom',  0.038150,0.005113,'estimate'),
('gcp','asia-east2','e2',      0.027264,0.003654,'estimate'),
('gcp','asia-east2','n2',      0.039514,0.005296,'estimate'),
('gcp','asia-east2','c2',      0.042385,0.005674,'estimate'),
('gcp','asia-east2','custom',  0.041468,0.005558,'estimate');

-- 磁盘费率种子（USD/GB/月）
INSERT IGNORE INTO cloud_disk_rates (provider,region,disk_type,gb_month_usd,note) VALUES
('gcp','default','default',      0.100000,'fallback'),
('gcp','us-central1','pd-ssd',   0.170000,'official'),
('gcp','us-central1','pd-balanced',0.100000,'official'),
('gcp','us-central1','pd-standard',0.040000,'official'),
('gcp','asia-east1','pd-ssd',    0.195500,'estimate'),
('gcp','asia-east1','pd-balanced',0.115000,'estimate'),
('gcp','asia-east1','pd-standard',0.046000,'estimate'),
('gcp','asia-east2','pd-ssd',    0.212500,'estimate'),
('gcp','asia-east2','pd-balanced',0.125000,'estimate'),
('gcp','asia-east2','pd-standard',0.050000,'estimate');

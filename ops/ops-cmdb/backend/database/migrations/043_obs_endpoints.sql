-- 数据源接入：Prometheus/VM、Loki、KubeSphere 等外部只读地址，支持多条(按环境/集群区分)。
-- token 加密存；本地不存历史数据，只保存"怎么连"，查询时实时打这些地址。
CREATE TABLE IF NOT EXISTS obs_endpoints (
  id          INT AUTO_INCREMENT PRIMARY KEY,
  name        VARCHAR(128) NOT NULL,
  type        VARCHAR(24)  NOT NULL,               -- prometheus / loki / kubesphere
  url         VARCHAR(512) NOT NULL,               -- 查询地址(如 vmselect/prometheus/loki gateway/ks-apiserver)
  env         VARCHAR(32)  NOT NULL DEFAULT '',    -- 适用环境(PROD/UAT/...，空=通用)
  cluster_id  INT          NOT NULL DEFAULT 0,     -- 适用集群(0=通用)
  token_enc   TEXT,                                -- 认证 token(AES 加密，可空)
  enabled     TINYINT      NOT NULL DEFAULT 1,
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_type (type)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

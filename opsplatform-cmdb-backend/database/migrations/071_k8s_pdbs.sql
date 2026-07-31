-- PodDisruptionBudget 采集（升级/drain 阻塞风险的唯一权威依据）
--
-- 背景：2026-07-31 排 UAT→生产的 GKE 升级窗口时发现，CMDB 采了 15 类 K8s 资源，
-- 唯独没有 PDB。而 PDB 恰恰是「节点升级要多久」里最不可控的一项：
--
--   status.disruptionsAllowed = 0 时，kubelet 驱逐请求会被 API Server 一直拒绝，
--   节点 drain 卡到 GKE 的超时上限才强杀 Pod。单节点可能因此多花一小时。
--
-- 生产 g32 已知有 3 个余量为 0 的 PDB（dolphinscheduler 的 master/postgresql/zookeeper），
-- 而 UAT 没有配这些 PDB——意味着拿 UAT 的实测耗时直接外推生产必然偏小，
-- 且这个偏差在升级当晚之前完全看不见。采了才能在预案里提前标出来。
--
-- 字段取自 policy/v1 PodDisruptionBudget：
--   spec.minAvailable / spec.maxUnavailable  IntOrString，可能是 "2" 也可能是 "50%"，按原样存字符串
--   status.disruptionsAllowed                ← 核心：0 = 现在驱逐任何一个 Pod 都会被拒
--   status.currentHealthy / desiredHealthy / expectedPods   用于解释「为什么是 0」
--
-- 注意 disruptionsAllowed 是随 Pod 健康状况实时变化的，不是静态配置：
-- 同一个 PDB 平时余量为 1，有 Pod 正在重启时就变 0。所以判断升级风险要看趋势，
-- 采集频率跟其他资源一致（120s）足够。

CREATE TABLE IF NOT EXISTS k8s_pdbs (
  id                  BIGINT       AUTO_INCREMENT PRIMARY KEY,
  cluster_id          INT          NOT NULL,
  namespace           VARCHAR(253) NOT NULL,
  name                VARCHAR(253) NOT NULL,
  -- 二者互斥，只会配其中一个；未配的那个存空串
  min_available       VARCHAR(16)  NOT NULL DEFAULT ''  COMMENT '可能是绝对数 "2" 或百分比 "50%"',
  max_unavailable     VARCHAR(16)  NOT NULL DEFAULT ''  COMMENT '同上，与 min_available 互斥',
  selector            VARCHAR(512) NOT NULL DEFAULT ''  COMMENT 'spec.selector 的 label 形式，用于关联到工作负载',
  current_healthy     INT          NOT NULL DEFAULT 0,
  desired_healthy     INT          NOT NULL DEFAULT 0,
  expected_pods       INT          NOT NULL DEFAULT 0,
  -- 0 = 此刻驱逐任何一个 Pod 都会被拒绝 → drain 会卡住 → 节点升级超时
  disruptions_allowed INT          NOT NULL DEFAULT 0   COMMENT '0=drain 会被阻塞，升级预案的红线',
  synced_at           DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_pdb (cluster_id, namespace, name),
  KEY idx_cluster (cluster_id),
  KEY idx_blocking (cluster_id, disruptions_allowed)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

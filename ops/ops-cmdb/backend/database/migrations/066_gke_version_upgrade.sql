-- GKE 版本控制与升级管理
--
-- 解决的盲区：GKE 会在我们不知道的时候自动升级集群、自动重建节点，出事后只能靠人翻控制台倒推。
-- 目标是把「自动升级」变成日程表上的计划项（提前 30 天预警、可主动挡住），
-- 把「节点自动修复」变成有记录可追溯的事件。
--
-- 数据来源分两路（2026-07-31 已逐项查证，见 docs/gke-version-upgrade-plan.md）：
--   1. GKE 官网版本排期表（无 API，解析 HTML）—— 远期「Auto Upgrade 日期」，能提前一个月知道
--   2. container.googleapis.com API —— 临期精确时刻(autoUpgradeStartTime)、暂停原因、升级/修复历史
--
-- 首个真实发现（PROD-013/UAT-016/INFRA-004）：3 个集群 55 个节点跑在 1.33，
-- 而 1.33 标准支持 2026-08-03 到期，此前无人知晓。

-- ---------------------------------------------------------------------------
-- 1) 官网版本排期表镜像
-- ---------------------------------------------------------------------------
-- ⚠️ 关键设计：日期不能只存 DATE。官网原文明确：
--   "Dates with only a month (for example, 2025-03) or quarter year (for example, 2025-Q3)
--    are approximations that will be updated with a date when it is known."
-- 实测 1.35 的 Stable 自动升级日期就是 `2026-09`（月粒度），1.36 是 `2026-Q4`（季度粒度）。
-- 若只存 DATE，前端会把近似值显示成精确日期，提醒也会报错日子。
-- 因此每个日期都存三列：raw(原文) + at(归一化到当月/当季首日，供排序和倒计时) + precision(粒度)。
-- 前端与飞书卡片在 precision<>'day' 时必须注明「官网仅给到月/季度粒度，日期会变」。
CREATE TABLE IF NOT EXISTS gke_version_schedule (
  id                      INT AUTO_INCREMENT PRIMARY KEY,
  minor_version           VARCHAR(16)  NOT NULL,                  -- 1.33
  channel                 VARCHAR(16)  NOT NULL,                  -- RAPID/REGULAR/STABLE/EXTENDED

  available_raw           VARCHAR(32)  NOT NULL DEFAULT '',
  available_at            DATE         NULL,
  available_precision     VARCHAR(8)   NOT NULL DEFAULT 'unknown', -- day/month/quarter/unknown

  auto_upgrade_raw        VARCHAR(32)  NOT NULL DEFAULT '',
  auto_upgrade_at         DATE         NULL,                       -- 「预计不早于此日自动升级」
  auto_upgrade_precision  VARCHAR(8)   NOT NULL DEFAULT 'unknown',

  -- EOS 是版本级属性（与通道无关），四行冗余存同值，换取单表查询不用 JOIN
  eos_standard_raw        VARCHAR(32)  NOT NULL DEFAULT '',
  eos_standard_at         DATE         NULL,
  eos_standard_precision  VARCHAR(8)   NOT NULL DEFAULT 'unknown',
  eos_extended_raw        VARCHAR(32)  NOT NULL DEFAULT '',
  eos_extended_at         DATE         NULL,
  eos_extended_precision  VARCHAR(8)   NOT NULL DEFAULT 'unknown',

  is_manual               TINYINT      NOT NULL DEFAULT 0,        -- 1=手工覆盖，同步时不被抓取结果冲掉
  synced_at               DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_ver_channel (minor_version, channel),
  KEY idx_auto_upgrade (auto_upgrade_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---------------------------------------------------------------------------
-- 2) 集群级升级信息（fetchClusterUpgradeInfo + clusters.get）
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS gke_cluster_upgrade (
  cluster_id              INT          NOT NULL PRIMARY KEY,
  release_channel         VARCHAR(24)  NOT NULL DEFAULT '',       -- RAPID/REGULAR/STABLE/EXTENDED/UNSPECIFIED
  current_master_version  VARCHAR(48)  NOT NULL DEFAULT '',
  minor_target_version    VARCHAR(48)  NOT NULL DEFAULT '',       -- 下次小版本升级目标
  patch_target_version    VARCHAR(48)  NOT NULL DEFAULT '',
  auto_upgrade_status     VARCHAR(255) NOT NULL DEFAULT '',       -- ACTIVE/MINOR_UPGRADE_PAUSED/... 可多值逗号分隔
  paused_reason           VARCHAR(512) NOT NULL DEFAULT '',       -- MAINTENANCE_EXCLUSION_NO_MINOR_UPGRADES 等，回答「为什么没升」
  eos_standard_at         DATE         NULL,                      -- API 直给，比排期表更权威
  eos_extended_at         DATE         NULL,
  maintenance_policy_json MEDIUMTEXT   NULL,                      -- 维护窗口 + 维护排除原样存，前端展开
  -- 算出来的「预计自动升级日」。落库而非每次算：前端要排序/筛选/倒计时，也便于「日期变了」的变更审计
  predicted_upgrade_at    DATE         NULL,
  predicted_precision     VARCHAR(8)   NOT NULL DEFAULT 'unknown',
  predicted_source        VARCHAR(32)  NOT NULL DEFAULT '',       -- autoUpgradeStartTime / schedule_table / none
  last_error              VARCHAR(512) NOT NULL DEFAULT '',       -- 采集失败原因（权限不足等），前端标黄
  synced_at               DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_predicted (predicted_upgrade_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---------------------------------------------------------------------------
-- 3) 节点池（nodePools.list + fetchNodePoolUpgradeInfo）
-- ---------------------------------------------------------------------------
-- 实际风险在节点池而非控制面：节点池自动升级通常晚于控制面，且 maxUnavailable 决定升级时同时挂几个节点。
-- 2026-07-31 实测确认 NodePoolUpgradeInfo 与集群级同构（官方文档未写全），
-- 所以节点池能独立拿到自己的暂停原因和升级历史，不用从集群级推断。
CREATE TABLE IF NOT EXISTS gke_node_pools (
  id                      BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id              INT          NOT NULL,
  name                    VARCHAR(255) NOT NULL,
  node_count              INT          NOT NULL DEFAULT 0,
  version                 VARCHAR(48)  NOT NULL DEFAULT '',
  status                  VARCHAR(32)  NOT NULL DEFAULT '',       -- RUNNING/RECONCILING/ERROR/...
  auto_upgrade            TINYINT      NOT NULL DEFAULT 0,        -- management.autoUpgrade，生产开着=风险源
  auto_repair             TINYINT      NOT NULL DEFAULT 0,        -- management.autoRepair
  -- 官方：仅在「升级即将开始」时才有值，精确到小时。远期靠排期表，临期靠这个做最后拦截
  auto_upgrade_start_time DATETIME     NULL,
  upgrade_description     VARCHAR(512) NOT NULL DEFAULT '',
  max_surge               INT          NOT NULL DEFAULT 0,
  max_unavailable         INT          NOT NULL DEFAULT 0,
  strategy                VARCHAR(24)  NOT NULL DEFAULT '',       -- SURGE/BLUE_GREEN
  bg_phase                VARCHAR(32)  NOT NULL DEFAULT '',       -- blueGreenInfo.phase，升级进行中的实时阶段
  upgrade_risk            VARCHAR(8)   NOT NULL DEFAULT '',       -- red/yellow/green，见计划 §四 评分规则
  auto_upgrade_status     VARCHAR(255) NOT NULL DEFAULT '',
  paused_reason           VARCHAR(512) NOT NULL DEFAULT '',
  minor_target_version    VARCHAR(48)  NOT NULL DEFAULT '',
  eos_standard_at         DATE         NULL,
  synced_at               DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_pool (cluster_id, name),
  KEY idx_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---------------------------------------------------------------------------
-- 4) 升级历史（upgradeDetails[] + operations.list 两个来源）
-- ---------------------------------------------------------------------------
-- start_type 是核心字段：区分「被 Google 自动升的」还是「我们手动升的」，此前完全查不到。
-- 两个来源的去重键不同，用合成 dedup_key 统一：
--   operations 来源   → op:<operation name>（操作 ID 天然唯一）
--   upgradeDetails 来源 → ud:<scope>:<pool>:<startTime>（该结构没有操作 ID）
-- 同一次升级可能被两个来源各记一条，用 source 区分；upgradeDetails 有 start_type 更权威，前端优先它。
CREATE TABLE IF NOT EXISTS gke_upgrade_history (
  id              BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id      INT          NOT NULL,
  dedup_key       VARCHAR(255) NOT NULL,
  scope           VARCHAR(16)  NOT NULL DEFAULT '',              -- control_plane / nodepool
  pool            VARCHAR(255) NOT NULL DEFAULT '',
  start_type      VARCHAR(16)  NOT NULL DEFAULT '',              -- AUTOMATIC / MANUAL / ''(operations 来源没有)
  state           VARCHAR(16)  NOT NULL DEFAULT '',              -- SUCCEEDED/FAILED/CANCELED/RUNNING/UNKNOWN
  initial_version VARCHAR(48)  NOT NULL DEFAULT '',
  target_version  VARCHAR(48)  NOT NULL DEFAULT '',
  started_at      DATETIME     NULL,
  ended_at        DATETIME     NULL,
  detail          VARCHAR(1024) NOT NULL DEFAULT '',
  source          VARCHAR(24)  NOT NULL DEFAULT '',              -- upgradeDetails / operations
  synced_at       DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_dedup (cluster_id, dedup_key),
  KEY idx_cluster_time (cluster_id, started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---------------------------------------------------------------------------
-- 5) 节点自动修复历史（operations.list 里 operationType=AUTO_REPAIR_NODES）
-- ---------------------------------------------------------------------------
-- GKE 的 node auto-repair 默认开启，触发后 drain 节点并重建；drain 一小时未完成则强制关机。
-- 整个过程静默，此前收不到任何通知——这是唯一能拿到修复记录的地方。
--
-- ⚠️ 2026-07-31 修正：node-auto-repair 文档提到的 operationReason（AUTO_REPAIR_LONG_UNHEALTHY）
-- 在 REST v1/v1beta1 的 Operation 结构里都不存在（Go 库 v0.290.0 实测 0 命中），那是 gcloud CLI 的字段。
-- 所以 repair_reason 需要从 detail/status_message 文本解析，解析不出时留空并打 WARN，不要瞎猜。
CREATE TABLE IF NOT EXISTS gke_repair_history (
  id             BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id     INT          NOT NULL,
  op_name        VARCHAR(255) NOT NULL,
  pool           VARCHAR(255) NOT NULL DEFAULT '',
  node_name      VARCHAR(255) NOT NULL DEFAULT '',
  repair_reason  VARCHAR(128) NOT NULL DEFAULT '',              -- 从 detail 解析，解析不出留空
  status         VARCHAR(16)  NOT NULL DEFAULT '',              -- PENDING/RUNNING/DONE/ABORTING
  started_at     DATETIME     NULL,
  ended_at       DATETIME     NULL,
  detail         VARCHAR(1024) NOT NULL DEFAULT '',             -- 原文保留，供人工判读和后续改进解析规则
  status_message VARCHAR(512) NOT NULL DEFAULT '',
  synced_at      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_op (cluster_id, op_name),
  KEY idx_cluster_time (cluster_id, started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---------------------------------------------------------------------------
-- 6) 节点健康告警状态机
-- ---------------------------------------------------------------------------
-- 不存全量采样（50 节点 × 90 秒 = 每天 4.8 万行，没价值），只存「首次异常时间」和「上次告警时间」：
--   not_ready_since —— 用于「连续 N 分钟」判定。GKE auto-repair 阈值是 ~10 分钟，
--                      我们 3 分钟告警，提前 5~8 分钟，够运维手动 drain 保住有状态服务
--   last_alert_at   —— 告警去重（NotReady 30 分钟内不重复，磁盘 6 小时内不重复）
CREATE TABLE IF NOT EXISTS k8s_node_alert_state (
  id              BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id      INT          NOT NULL,
  node_name       VARCHAR(255) NOT NULL,
  not_ready_since DATETIME     NULL,
  disk_pct        DECIMAL(5,2) NOT NULL DEFAULT 0,
  disk_full_eta   DATETIME     NULL,                            -- 趋势外推的满盘时刻（infra 两集群无 Prometheus，恒为 NULL）
  alert_level     VARCHAR(8)   NOT NULL DEFAULT '',             -- red/yellow/green
  alert_kind      VARCHAR(24)  NOT NULL DEFAULT '',             -- not_ready / disk_full / version_skew
  last_alert_at   DATETIME     NULL,
  updated_at      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_node (cluster_id, node_name),
  KEY idx_level (alert_level)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ---------------------------------------------------------------------------
-- 定时任务种子
-- ---------------------------------------------------------------------------
-- gke_schedule_sync 不依赖 GCP 凭据（只抓官网页面），默认开；
-- gke_upgrade_sync 需要 SA key，本地无凭据时会失败并记 last_error，故默认开但失败可见。
INSERT IGNORE INTO scheduled_tasks (task_key, name, enabled, schedule) VALUES
  ('gke_schedule_sync', 'GKE 官网版本排期同步', 1, '0 8 * * *'),
  ('gke_upgrade_sync',  'GKE 集群升级信息采集', 1, '0 */6 * * *');

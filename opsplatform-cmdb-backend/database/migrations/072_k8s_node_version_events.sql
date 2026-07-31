-- 节点版本变更事件（逐节点升级耗时的唯一来源）
--
-- 背景：k8s_nodes 是覆盖式的当前状态表，节点从 1.33 变成 1.35 的那一刻不留任何痕迹。
-- 结果是「这个池升了多久」只能拿到 GCP 给的整池时长，拿不到逐节点分布——
-- 而外推生产窗口恰恰需要单节点耗时的中位数和最慢值（整池时长除以节点数是错的，
-- 因为节点是分批并行升的，除数应该是批次数不是节点数）。
--
-- ⚠️ GKE 升级不是原地改 kubelet 版本，而是**销毁旧节点、创建新节点**（SURGE 和
-- BLUE_GREEN 都是），节点名会变。所以要记的不只是 version_changed，更主要是
-- added / removed 这两类：
--
--   一次节点池升级 = 一串 added(新版本) + 一串 removed(旧版本) 交替出现，
--   把它们按时间排开，就还原出了批次大小、批次间隔和整池节奏。
--
-- version_changed 保留是因为理论上存在原地升级路径（手工改 kubelet、某些第三方发行版），
-- k3s 集群就可能走这条路——不能假设所有集群都是 GKE。
--
-- ⚠️ detected_at 是**采集轮次时间**不是事件真实时间。采集间隔 120s，
-- 所以任何据此算出的耗时都有 ±2 分钟的粒度。做耗时统计时必须把这个误差说出来，
-- 否则「单节点 8 分钟」会被当成精确值拿去排生产窗口。

CREATE TABLE IF NOT EXISTS k8s_node_version_events (
  id           BIGINT       AUTO_INCREMENT PRIMARY KEY,
  cluster_id   INT          NOT NULL,
  node_name    VARCHAR(253) NOT NULL,
  pool         VARCHAR(255) NOT NULL DEFAULT '',
  -- added=新节点出现  removed=节点消失  version_changed=同名节点版本变了(原地升级)
  event        VARCHAR(16)  NOT NULL,
  from_version VARCHAR(48)  NOT NULL DEFAULT ''  COMMENT 'removed/version_changed 时为变更前版本；added 时为空',
  to_version   VARCHAR(48)  NOT NULL DEFAULT ''  COMMENT 'added/version_changed 时为当前版本；removed 时为空',
  detected_at  DATETIME     NOT NULL             COMMENT '采集到的时刻，非事件真实时刻，粒度=采集间隔(120s)',
  KEY idx_cluster_time (cluster_id, detected_at),
  KEY idx_node (cluster_id, node_name),
  KEY idx_pool_time (cluster_id, pool, detected_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

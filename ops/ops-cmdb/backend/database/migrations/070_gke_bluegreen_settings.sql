-- 节点池补 BLUE_GREEN 升级参数（升级耗时预估的决定性输入）
--
-- 背景：2026-07-31 排 UAT 升级时发现，4 个集群全部 15 个节点池的 strategy 都是 BLUE_GREEN。
-- 一开始怀疑是采集取了枚举默认值，查证后确认是真值——而且 max_surge=0/max_unavailable=0
-- 与 BLUE_GREEN 完全自洽：那两个参数是 SURGE 策略专用的，BLUE_GREEN 根本不读它们，
-- 它读的是另一组 blueGreenSettings。
--
-- 问题在于 blueGreenSettings 此前一个字段都没采，导致「这次升级要多久」无从算起：
--
--   BLUE_GREEN 总时长 ≈ (节点数 ÷ 每批节点数) × (每批重建时长 + 每批 soak) + 整池 soak
--
-- 缺了每批节点数和两个 soak 时长，只能靠拍脑袋估「5~10 小时」，
-- 更没法把 UAT 的实测值外推到生产 35 节点。这正是做版本升级管理要回答的核心问题。
--
-- 字段来自 nodePools.list 的 upgradeSettings.blueGreenSettings：
--   nodePoolSoakDuration              整池排空后的观察期，过了才清理旧池
--   standardRolloutPolicy.batchNodeCount / batchPercentage   每批取几个节点（二选一）
--   standardRolloutPolicy.batchSoakDuration                  每批之间的观察期
--   autoscaledRolloutPolicy                                  开了 autoscaler 时的替代策略，无参数
--
-- 两个 duration 在 API 里是 "3600s" 这样的字符串，统一解析成秒存整数；
-- 解析不出时存 NULL 并打 WARN，绝不能默默存 0——0 会让预估时长凭空少掉一大截。

ALTER TABLE gke_node_pools
  ADD COLUMN bg_rollout_policy     VARCHAR(16)   NOT NULL DEFAULT ''
    COMMENT 'BLUE_GREEN 的批次策略：STANDARD/AUTOSCALED/空=未配(GKE 用默认值)' AFTER bg_phase,
  ADD COLUMN bg_batch_node_count   INT           NULL
    COMMENT '每批排空的节点数；与 bg_batch_percentage 二选一' AFTER bg_rollout_policy,
  ADD COLUMN bg_batch_percentage   DECIMAL(6,4)  NULL
    COMMENT '每批排空的节点占比 (0,1]；与 bg_batch_node_count 二选一' AFTER bg_batch_node_count,
  ADD COLUMN bg_batch_soak_sec     INT           NULL
    COMMENT '每批排空后的观察期(秒)；NULL=API 没给或解析失败，不是 0' AFTER bg_batch_percentage,
  ADD COLUMN bg_node_pool_soak_sec INT           NULL
    COMMENT '整池排空后的观察期(秒)，过后才清理旧池；NULL 同上' AFTER bg_batch_soak_sec;

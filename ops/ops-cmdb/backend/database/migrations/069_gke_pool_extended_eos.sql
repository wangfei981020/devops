-- 节点池补扩展支持截止（修 P1-4：EffectiveEOS 没考虑 EXTENDED 通道）
--
-- 背景：GKE 的「硬期限」取决于集群订的是哪个通道——
--   非 EXTENDED 通道 → 标准支持结束(end of standard support) 时会被强制升级
--   EXTENDED 通道    → 标准支持结束后仍可继续用，真正的硬期限是扩展支持结束
--
-- 原先 applyEffectiveEOS 一律用 standard，集群级的 eos_extended_at 采了却不参与计算，
-- 节点池级压根没这一列。当前 4 个集群都「未入通道」（按 STABLE 走）所以没暴露，
-- 但只要切到 EXTENDED，standard 到期而 extended 未到的那段时间里，
-- GKE 根本不会强制升级，看板和飞书却会红着报「强制升级：还有 N 天」——最高优先级的误报。
--
-- fetchNodePoolUpgradeInfo 本来就返回 EndOfExtendedSupportTimestamp，只是之前没存。

ALTER TABLE gke_node_pools
  ADD COLUMN eos_extended_at DATE NULL
  COMMENT '该池版本的扩展支持截止；EXTENDED 通道下这才是硬期限' AFTER eos_standard_at;

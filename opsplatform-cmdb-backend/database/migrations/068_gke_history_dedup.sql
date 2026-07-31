-- 升级历史跨来源去重（修 P1-3：同一升级事件被记两条，历史条数虚高一倍）
--
-- 问题：一次升级会被两个来源各记一条——
--   upgradeDetails 来源 → dedup_key = 'ud:<scope>:<pool>:<startTime>'（有 startType，权威）
--   operations     来源 → dedup_key = 'op:<operation name>'
-- 两个 key 天然不冲突，所以 ON DUPLICATE KEY 合并不了。实测远端 14 条实为 7 个真实事件 ×2，
-- 「自动 7 / 来源无方式 7」这种完美对称就是铁证。用户会以为升级了 14 次。
--
-- 解法：dedup_key 改成与来源无关的「事件键」——<scope>:<pool>:<起始时刻截到分钟>。
-- 两个来源落到同一行：upgradeDetails 负责 start_type/版本/state，operations 只补 op_name/detail
-- 且不覆盖前者（见 gke_sync.go 的 saveOperations）。
-- 截到分钟而非秒：两个来源的 startTime 可能有秒级抖动；同一节点池同一分钟内不可能有两次升级。

-- 存量清理：先删掉 operations 来源里能和 upgradeDetails 对上的重复行，
-- 再把剩余行的 dedup_key 重算成新格式。留 upgradeDetails 那条（它有 start_type）。
DELETE o FROM gke_upgrade_history o
  JOIN gke_upgrade_history u
    ON u.cluster_id = o.cluster_id
   AND u.scope      = o.scope
   AND u.pool       = o.pool
   AND DATE_FORMAT(u.started_at, '%Y-%m-%d %H:%i') = DATE_FORMAT(o.started_at, '%Y-%m-%d %H:%i')
   AND u.source = 'upgradeDetails'
 WHERE o.source = 'operations';

-- 剩余行重算 dedup_key（同 key 的只可能留一条，上一步已保证）
UPDATE gke_upgrade_history
   SET dedup_key = CONCAT(scope, ':', pool, ':', DATE_FORMAT(started_at, '%Y-%m-%d %H:%i'))
 WHERE started_at IS NOT NULL;

-- op_name 单独存一列：合并后 operations 的操作 ID 不能再塞进 dedup_key，但排查时有用
-- 注意列定义顺序：COMMENT 属于列定义，必须写在位置修饰符 AFTER 之前，写反了会 1064 语法错
ALTER TABLE gke_upgrade_history
  ADD COLUMN op_name VARCHAR(255) NOT NULL DEFAULT ''
  COMMENT 'operations 来源的操作 ID；upgradeDetails 来源为空' AFTER source;

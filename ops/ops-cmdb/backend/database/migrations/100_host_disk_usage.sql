-- 磁盘用量。
--
-- 为什么必须是每块盘一个百分比，而不是主机级的一个总数：
-- 一台 boot 盘 96%、数据盘 71% 的机器，加权算下来才 79%，看着很健康，
-- 但系统盘马上要写满了 —— 而 kubelet 的 DiskPressure 恰恰是按单块盘判的。
-- 加总会把真正要出事的机器藏起来。
--
-- ⚠️ used_percent 允许 NULL，且 NULL ≠ 0。
--    NULL = 没采到（集群没接 Prometheus、node_exporter 没跑、设备名对不上）
--    0    = 真的是空盘
-- 前端对这两者的渲染完全不同：NULL 显示「未接入」或不显示，0 显示 0%。
-- 用 0 或 -1 当哨兵值会让「没采到」被统计进平均值，把整体用量算低。

ALTER TABLE host_disks
  ADD COLUMN device_name  VARCHAR(64)   NOT NULL DEFAULT '' COMMENT '云上的设备名，如 persistent-disk-1；用于和 node_exporter 的 device 标签对齐',
  ADD COLUMN mount_point  VARCHAR(255)  NOT NULL DEFAULT '' COMMENT '挂载点，如 /var/lib/kafka；来自节点采集',
  ADD COLUMN used_percent DECIMAL(5,2)  NULL                COMMENT '用量百分比；NULL=没采到，与 0 含义不同',
  ADD COLUMN used_at      DATETIME      NULL                COMMENT '用量采集时刻；配合 used_percent 判断数据新鲜度';

-- 按主机查用量是列表页的热路径（每次列表都要 JOIN 一次）
ALTER TABLE host_disks ADD INDEX idx_host_used (host_ci_id, used_percent);

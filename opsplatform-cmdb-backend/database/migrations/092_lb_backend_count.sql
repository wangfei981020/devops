-- 记录采集时追溯到的后端实例数。
--
-- 现象：backend_state='ok'（只在 len(backends)>0 时才写）但接口返回的
-- backends 是空的。也就是**采集当时确实追溯到了实例，读出来却没有**——
-- 数据丢在写入或读取某一环，而界面只能显示成"没有后端"，
-- 比修复前更误导：原来 81 个统一显示 0，人知道这块不可信；
-- 现在标着"追溯成功"却给不出实例，排障会以为"重新同步就有了"，
-- 而同步不会改变结果。
--
-- 存下采集时的计数，读取时一比就知道是"真没有"还是"存/读丢了"，
-- 而不是让人对着一个空列表猜。
ALTER TABLE cloud_loadbalancers
  ADD COLUMN backend_count INT NOT NULL DEFAULT 0 AFTER backend_state;

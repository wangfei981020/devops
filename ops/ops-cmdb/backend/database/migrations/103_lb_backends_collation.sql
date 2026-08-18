-- 统一 cloud_lb_backends 的排序规则。
--
-- 全库 93 张表是 utf8mb4_0900_ai_ci，只有这一张是 utf8mb4_unicode_ci。
-- 跨这两组表做字符串等值比较（`b.project = l.project`）会抛 **Error 1267**，
-- 而这种失败极其隐蔽：
--
--   · 查询报错 → 上层拿到 err → 计数返回 nil / 空
--   · 空被渲染成"没有后端"
--   · 于是**一批正在服务的负载均衡显示成"后端全空"**
--
-- 生产上就是这么误报过 8 条 LB 的，排查了很久才定位到排序规则。
-- 启动自检 CheckCollations 会把这类不一致打进日志，这条迁移把它修掉。
--
-- ⚠️ CONVERT TO 会重建表。这张表是关联表、行数小（云上 LB 的后端总数），
-- 秒级完成。若将来某张大表要做同样的事，必须先评估锁表时间。
ALTER TABLE cloud_lb_backends
  CONVERT TO CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;

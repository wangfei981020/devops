-- 负载均衡的后端追溯状态。
--
-- 生产上 81 个 LB **全部**显示"后端为空"。看到这个数字的第一反应是
-- "这些 LB 都挂空了"，但实际上分不清是哪种情况：
--   · 真的没有后端（LB 挂空 —— 这本身是个要修的问题）
--   · 追溯过程出错（拉 targetPools/backendServices 的 API 被拒）
--   · 服务型 NEG（GKE Service/Ingress 直连 Pod，当前追溯不了）
--
-- 三种情况的处置完全不同，而界面把它们渲染成同一个"空"。
-- 「没查到」被显示成「没有」，是这套系统最危险的失效模式。
ALTER TABLE cloud_loadbalancers
  ADD COLUMN backend_state VARCHAR(16) NOT NULL DEFAULT '' AFTER target;

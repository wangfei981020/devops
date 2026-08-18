-- Gateway 表区分 API 组
--
-- k8s_gateways 此前只采 gateway.networking.k8s.io（Gateway API）那一套。
-- 而生产用的是 Istio 的 networking.istio.io/Gateway——完全不同的资源，
-- 结果生产上查出来 count=0，12 个 VirtualService 引用的 Gateway 名对不上任何东西，
-- 只能靠 istiod 日志反推（CMDB-006 附带记录的坑之一）。
--
-- 两种放同一张表而不是分两个页面：排查时问的是「这个 VS 引用的 Gateway 在哪」，
-- 没人关心它属于哪个 API 组。用 api_group 列区分即可。
--
-- 只读 RBAC 里 networking.istio.io 的 gateways 权限本来就有，不需要改集群。

ALTER TABLE k8s_gateways
  ADD COLUMN api_group VARCHAR(48) NOT NULL DEFAULT 'gateway.networking.k8s.io'
  COMMENT 'gateway.networking.k8s.io | networking.istio.io';

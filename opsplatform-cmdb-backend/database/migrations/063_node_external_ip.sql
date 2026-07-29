-- 节点公网 IP + 集群网络位置
--
-- 解 CMDB-009：expose_surface 在 k3s 上 500/502 条判为 unknown（全是 NodePort）。
-- GKE 上能判是因为有 cloud_loadbalancers.scheme 这个权威依据，k3s 没有云 LB，
-- CMDB 就完全没有判定依据。
--
-- 两条依据，优先级从高到低：
--   1. 节点是否有公网 IP —— K8s Node 的 status.addresses 里就有 ExternalIP，
--      之前只采了 InternalIP，白白丢掉了这个权威信息
--   2. 集群网络位置人工属性 —— 节点无公网 IP 时用它兜底（存在 NAT/端口转发的情况）

ALTER TABLE k8s_nodes
  ADD COLUMN external_ip VARCHAR(64) NOT NULL DEFAULT ''
  COMMENT '节点公网 IP（来自 Node.status.addresses 的 ExternalIP），空=无';

ALTER TABLE k8s_clusters
  ADD COLUMN network_exposure VARCHAR(16) NOT NULL DEFAULT ''
  COMMENT '集群网络位置：public=节点可从公网访问 / private=仅内网 / 空=按节点公网IP自动推断';

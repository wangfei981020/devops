-- Service 的对外暴露信息：LoadBalancer 分配到的 IP + GKE 内外网类型注解。
-- 没有这两列就无法判断一个 LoadBalancer Service 到底暴露在公网还是内网——
-- 之前排查 UAT 时只能看到 type=LoadBalancer 和端口，ZooKeeper 2181 / Redis 6379 是不是挂在公网上无从判断。
-- external_ip 同时是与 cloud_loadbalancers.vip 精确关联的连接键。
ALTER TABLE k8s_services ADD COLUMN external_ip VARCHAR(255) NOT NULL DEFAULT '' AFTER cluster_ip;
ALTER TABLE k8s_services ADD COLUMN lb_type VARCHAR(32) NOT NULL DEFAULT '' AFTER external_ip;
ALTER TABLE k8s_services ADD INDEX idx_svc_extip (external_ip);

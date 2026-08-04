-- 集群在指标里的 cluster 标签取值。
--
-- 背景：054 给数据源加了 cluster_label（用哪个标签名区分集群），但标签的**取值**一直是
-- 直接拿 k8s_clusters.name 拼的。这在 UAT 上恰好对得上（CMDB 里叫 uat-k8s-cluster-01，
-- 指标里也是 uat-k8s-cluster-01），于是这个隐含假设一直没暴露。
--
-- g32 生产踩中了：CMDB 里集群名是 g32-prod-cluster，而 VictoriaMetrics 里的标签值是
-- prod-k8s-cluster-01。于是每一条带 $CLUSTER 的查询都被拼成 cluster="g32-prod-cluster"，
-- Prometheus 老老实实返回空——而空结果在排障语境里会被读成「该组件正常」。
-- 实测后果：kafka 的 broker/lag/磁盘/内存全查不到，pvc_usage 直接返回 {}，
-- 但接口全都 ok:true，表面看不出任何异常。这比报错危险得多。
--
-- 集群名是 CMDB 的台账命名，标签值由对方集群的 Prometheus 采集配置决定，两者本就没有
-- 必须一致的理由。这里把它显式存下来；留空 = 回落到 name（保持既有行为，UAT/DEV 不受影响）。
ALTER TABLE k8s_clusters ADD COLUMN prom_cluster_value VARCHAR(128) NOT NULL DEFAULT '' AFTER name;

-- g32 生产：CMDB 名与指标标签值不一致，回填实测值（来自 prom_labels(cluster) 的可选值列表）。
UPDATE k8s_clusters SET prom_cluster_value = 'prod-k8s-cluster-01'
 WHERE name = 'g32-prod-cluster' AND prom_cluster_value = '';

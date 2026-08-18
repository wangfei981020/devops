-- 集群在指标里的 cluster 标签取值。
--
-- 背景：054 给数据源加了 cluster_label（用哪个标签名区分集群），但标签的**取值**一直是
-- 直接拿 k8s_clusters.name 拼的。在部分环境上恰好对得上（登记名与指标标签值相同），
-- 于是这个隐含假设一直没暴露。
--
-- 生产上踩中过：系统里登记的集群名与 Prometheus/VictoriaMetrics 里的标签值不一致，
-- 于是每一条带 $CLUSTER 的查询都被拼成了系统里登记的那个名字，
-- Prometheus 老老实实返回空——而空结果在排障语境里会被读成「该组件正常」。
-- 实测后果：kafka 的 broker/lag/磁盘/内存全查不到，pvc_usage 直接返回 {}，
-- 但接口全都 ok:true，表面看不出任何异常。这比报错危险得多。
--
-- 集群名是 CMDB 的台账命名，标签值由对方集群的 Prometheus 采集配置决定，两者本就没有
-- 必须一致的理由。这里把它显式存下来；留空 = 回落到 name（保持既有行为，UAT/DEV 不受影响）。
ALTER TABLE k8s_clusters ADD COLUMN prom_cluster_value VARCHAR(128) NOT NULL DEFAULT '' AFTER name;

-- 不在此处回填任何具体取值：标签值由各家自己的 Prometheus 采集配置决定，
-- 没有任何一个值是可以预设的。留空即回落到 name，与升级前行为一致；
-- 确实不一致的集群，在「集群管理」里填这一项（可选值可从 prom_labels(cluster) 拉取）。

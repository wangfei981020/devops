-- 主机标记 GKE 节点：GKE 节点=GCE 实例会同步进主机，需区分并从"传统主机成本"里剔除(避免与 K8s 计算成本重复算)。
ALTER TABLE hosts ADD COLUMN is_k8s_node TINYINT NOT NULL DEFAULT 0 AFTER stale;
ALTER TABLE hosts ADD COLUMN k8s_pool VARCHAR(128) NOT NULL DEFAULT '' AFTER is_k8s_node;
-- 回填存量：GKE 节点名以 gke- 开头(主机名在 cis 表,join 回填)
UPDATE hosts h JOIN cis c ON c.id=h.ci_id SET h.is_k8s_node=1 WHERE c.name LIKE 'gke-%';

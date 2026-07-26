-- 成本阶段2修正：集群计费模式。cloud=真实云支出 / idc=迁云估算(IT管实际) / none=不计费(本地排除)。
-- 空值时代码按 provider 推断：gke→cloud、in-cluster→none、generic→idc。
ALTER TABLE k8s_clusters ADD COLUMN cost_mode VARCHAR(16) NOT NULL DEFAULT '' AFTER nodepool_label;

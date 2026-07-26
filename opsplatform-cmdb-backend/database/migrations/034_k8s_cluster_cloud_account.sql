-- K8s 集群不再单独存 GCP SA key（与主机模块 cloud_accounts 重复）。
-- 改为引用已有云账号：GKE 集群选一个 cloud_account，复用其加密凭据（将来拉 GKE 控制面/节点池用）。
ALTER TABLE k8s_clusters DROP COLUMN sa_key_enc;
ALTER TABLE k8s_clusters ADD COLUMN cloud_account_id INT NOT NULL DEFAULT 0 AFTER project_id;

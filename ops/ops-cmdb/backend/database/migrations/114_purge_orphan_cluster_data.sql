-- 清理「集群已删除、资源还留着」的孤儿数据。
--
-- # 问题
--
-- `K8sClusterHandler.Delete` 原来只删 `k8s_clusters` 和 `k8s_sync_state`，
-- 采集来的节点/Pod/工作负载等全部留在库里。它们的 `cluster_id` 指向一个
-- 不存在的集群，于是：
--
--   * 列表里集群名 JOIN 不到 → 显示空白
--   * 心跳停在删除那一刻 → 永远显示「失联」
--   * 重新纳管同一个集群会拿到**新的 cluster_id** → 每个节点出现两行，
--     一行 Ready（新）、一行失联（旧）
--
-- 生产实测：uat-k8s-cluster-01 删掉重建后，32 个节点里 16 个是这种重影，
-- 界面上报「20 个节点失联，状态不可信」——而真实情况是集群完全健康。
--
-- 删除侧已在 v0.65.0 修好（连带清理）。本迁移收拾**存量**。
--
-- # 判据
--
-- 只删「cluster_id 在 k8s_clusters 里找不到」的行。用 NOT EXISTS 而不是
-- LEFT JOIN IS NULL：后者在 cluster_id=0 这类脏值上行为不直观。
--
-- ⚠️ 不动这四类（人工产物，删了找不回来）：
--   harbor_registries / obs_endpoints —— 接入配置，本来就独立于集群生命周期
--   k8s_ns_project                    —— 人工填的命名空间归属
--   gke_upgrade_baselines             —— 刻意存下的升级前基线，是历史证据
-- 它们即使暂时"孤儿"，等同名集群重新纳管后仍然有用。

DELETE FROM k8s_nodes               WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_nodes.cluster_id);
DELETE FROM k8s_pods                WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_pods.cluster_id);
DELETE FROM k8s_workloads           WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_workloads.cluster_id);
DELETE FROM k8s_namespaces          WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_namespaces.cluster_id);
DELETE FROM k8s_services            WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_services.cluster_id);
DELETE FROM k8s_pvcs                WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_pvcs.cluster_id);
DELETE FROM k8s_events              WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_events.cluster_id);
DELETE FROM k8s_ingresses           WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_ingresses.cluster_id);
DELETE FROM k8s_gateways            WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_gateways.cluster_id);
DELETE FROM k8s_httproutes          WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_httproutes.cluster_id);
DELETE FROM k8s_virtualservices     WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_virtualservices.cluster_id);
DELETE FROM k8s_hpas                WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_hpas.cluster_id);
DELETE FROM k8s_pdbs                WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_pdbs.cluster_id);
DELETE FROM k8s_configmaps          WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_configmaps.cluster_id);
DELETE FROM k8s_endpoints           WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_endpoints.cluster_id);
DELETE FROM k8s_secrets             WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_secrets.cluster_id);
DELETE FROM k8s_pod_config_refs     WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_pod_config_refs.cluster_id);
DELETE FROM k8s_pod_security        WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_pod_security.cluster_id);
DELETE FROM k8s_pod_volumes         WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_pod_volumes.cluster_id);
DELETE FROM k8s_node_pools          WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_node_pools.cluster_id);
DELETE FROM k8s_changes             WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_changes.cluster_id);
DELETE FROM k8s_node_version_events WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_node_version_events.cluster_id);
DELETE FROM k8s_node_alert_state    WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_node_alert_state.cluster_id);
DELETE FROM k8s_sync_state          WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = k8s_sync_state.cluster_id);
DELETE FROM gke_cluster_upgrade     WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = gke_cluster_upgrade.cluster_id);
DELETE FROM gke_node_pools          WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = gke_node_pools.cluster_id);
DELETE FROM gke_repair_history      WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = gke_repair_history.cluster_id);
DELETE FROM gke_upgrade_history     WHERE NOT EXISTS (SELECT 1 FROM k8s_clusters c WHERE c.id = gke_upgrade_history.cluster_id);

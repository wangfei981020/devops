-- 多租户地基：全部租户级表加 tenant_id。
--
-- # 为什么现在做
--
-- tenant_id 必须在写第一张新业务表之前就位。事后补 = 全库数据迁移 + 全部查询重写，
-- 成本翻倍。即使产品上先只开一个租户，字段也要先埋进去。
--
-- # expand–contract 的 expand 阶段
--
-- 本迁移**只加列、只加索引，不删任何东西**，因此：
--   · 旧版本代码在新库上照常跑（它不认识这个列，但列有默认值）
--   · 滚动升级期间新旧副本共用一个库，两边都能工作
-- 收掉旧索引要等到下一个 MINOR 版本（contract 阶段），且确认没有旧副本在跑。
--
-- # 为什么默认值是 1 而不是 0
--
-- 0 保留给平台级数据。存量数据全部属于默认租户，其 id 为 1。
-- 用 NOT NULL DEFAULT 1 而不是允许 NULL：NULL 在 `!=` 比较里会漏行，
-- `WHERE tenant_id != 5` 不会返回 NULL 行 —— 那是个安全陷阱。

-- ── 租户表本身 ──
CREATE TABLE IF NOT EXISTS tenants (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  name          VARCHAR(128) NOT NULL COMMENT '显示名，术语可配（部门/客户/业务线）',
  slug          VARCHAR(64)  NOT NULL COMMENT 'URL 与标签用的短标识',
  status        VARCHAR(16)  NOT NULL DEFAULT 'active' COMMENT 'active/suspended/deleted',
  deleted_at    DATETIME     NULL COMMENT '软删时间。租户删除一律软删+保留期，不给硬删按钮',
  created_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at    DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uk_tenants_slug (slug)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 默认租户：存量数据的归属。名称在安装向导里由客户填写，这里先占位。
INSERT INTO tenants (id, name, slug)
SELECT 1, 'Default', 'default'
 WHERE NOT EXISTS (SELECT 1 FROM tenants WHERE id = 1);

-- ── 用户 × 租户 × 角色 ──
CREATE TABLE IF NOT EXISTS user_tenants (
  user_id    BIGINT UNSIGNED NOT NULL,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  role_code  VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '该租户内的角色',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id, tenant_id),
  KEY idx_ut_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 存量用户全部归入默认租户，保持升级前行为。
INSERT INTO user_tenants (user_id, tenant_id, role_code)
SELECT u.id, 1, '' FROM users u
 WHERE NOT EXISTS (SELECT 1 FROM user_tenants ut WHERE ut.user_id = u.id AND ut.tenant_id = 1);
-- ── 租户级表加列 ──
-- 复合索引把 tenant_id 放第一列：所有查询都会带它，放后面等于没有。
--
-- ⚠️ 表清单取自真实库的 information_schema，不是 grep 迁移文件。
--    grep 会漏：含数字的表名（k8s_* 这类）、动态建表、以及正则没覆盖的写法。
--    第一版就是这么漏掉 25 张 k8s_* 表的 —— 当时列表里出现一个孤零零的 "k"，
--    被当成解析噪声过滤掉了，实际那是被字符类 [a-z_]+ 截断的表名。

ALTER TABLE acme_accounts ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE acme_accounts ADD INDEX idx_acme_accounts_tenant (tenant_id);
ALTER TABLE audit_changes ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE audit_changes ADD INDEX idx_audit_changes_tenant (tenant_id);
ALTER TABLE audit_logs ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE audit_logs ADD INDEX idx_audit_logs_tenant (tenant_id);
ALTER TABLE cdn_accounts ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cdn_accounts ADD INDEX idx_cdn_accounts_tenant (tenant_id);
ALTER TABLE cdn_certificates ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cdn_certificates ADD INDEX idx_cdn_certificates_tenant (tenant_id);
ALTER TABLE cdn_dns_records ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cdn_dns_records ADD INDEX idx_cdn_dns_records_tenant (tenant_id);
ALTER TABLE cdn_rules ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cdn_rules ADD INDEX idx_cdn_rules_tenant (tenant_id);
ALTER TABLE cdn_zone_settings ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cdn_zone_settings ADD INDEX idx_cdn_zone_settings_tenant (tenant_id);
ALTER TABLE cdn_zones ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cdn_zones ADD INDEX idx_cdn_zones_tenant (tenant_id);
ALTER TABLE cdns ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cdns ADD INDEX idx_cdns_tenant (tenant_id);
ALTER TABLE cert_history ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cert_history ADD INDEX idx_cert_history_tenant (tenant_id);
ALTER TABLE certificates ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE certificates ADD INDEX idx_certificates_tenant (tenant_id);
ALTER TABLE ci_labels ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE ci_labels ADD INDEX idx_ci_labels_tenant (tenant_id);
ALTER TABLE ci_relations ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE ci_relations ADD INDEX idx_ci_relations_tenant (tenant_id);
ALTER TABLE cis ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cis ADD INDEX idx_cis_tenant (tenant_id);
ALTER TABLE cloud_account_projects ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_account_projects ADD INDEX idx_cloud_account_projects_tenant (tenant_id);
ALTER TABLE cloud_accounts ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_accounts ADD INDEX idx_cloud_accounts_tenant (tenant_id);
ALTER TABLE cloud_addresses ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_addresses ADD INDEX idx_cloud_addresses_tenant (tenant_id);
ALTER TABLE cloud_compute_rates ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_compute_rates ADD INDEX idx_cloud_compute_rates_tenant (tenant_id);
ALTER TABLE cloud_disk_rates ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_disk_rates ADD INDEX idx_cloud_disk_rates_tenant (tenant_id);
ALTER TABLE cloud_dns_records ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_dns_records ADD INDEX idx_cloud_dns_records_tenant (tenant_id);
ALTER TABLE cloud_dns_zones ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_dns_zones ADD INDEX idx_cloud_dns_zones_tenant (tenant_id);
ALTER TABLE cloud_firewalls ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_firewalls ADD INDEX idx_cloud_firewalls_tenant (tenant_id);
ALTER TABLE cloud_iam_bindings ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_iam_bindings ADD INDEX idx_cloud_iam_bindings_tenant (tenant_id);
ALTER TABLE cloud_lb_backends ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_lb_backends ADD INDEX idx_cloud_lb_backends_tenant (tenant_id);
ALTER TABLE cloud_loadbalancers ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_loadbalancers ADD INDEX idx_cloud_loadbalancers_tenant (tenant_id);
ALTER TABLE cloud_networks ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_networks ADD INDEX idx_cloud_networks_tenant (tenant_id);
ALTER TABLE cloud_price_rates ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_price_rates ADD INDEX idx_cloud_price_rates_tenant (tenant_id);
ALTER TABLE cloud_subnets ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cloud_subnets ADD INDEX idx_cloud_subnets_tenant (tenant_id);
ALTER TABLE cost_snapshots ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE cost_snapshots ADD INDEX idx_cost_snapshots_tenant (tenant_id);
ALTER TABLE disk_alert_state ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE disk_alert_state ADD INDEX idx_disk_alert_state_tenant (tenant_id);
ALTER TABLE dns_records ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE dns_records ADD INDEX idx_dns_records_tenant (tenant_id);
ALTER TABLE domain_records ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE domain_records ADD INDEX idx_domain_records_tenant (tenant_id);
ALTER TABLE domain_renewals ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE domain_renewals ADD INDEX idx_domain_renewals_tenant (tenant_id);
ALTER TABLE domains ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE domains ADD INDEX idx_domains_tenant (tenant_id);
ALTER TABLE environments ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE environments ADD INDEX idx_environments_tenant (tenant_id);
ALTER TABLE gke_available_versions ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE gke_available_versions ADD INDEX idx_gke_available_versions_tenant (tenant_id);
ALTER TABLE gke_cluster_upgrade ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE gke_cluster_upgrade ADD INDEX idx_gke_cluster_upgrade_tenant (tenant_id);
ALTER TABLE gke_node_pools ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE gke_node_pools ADD INDEX idx_gke_node_pools_tenant (tenant_id);
ALTER TABLE gke_repair_history ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE gke_repair_history ADD INDEX idx_gke_repair_history_tenant (tenant_id);
ALTER TABLE gke_upgrade_baselines ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE gke_upgrade_baselines ADD INDEX idx_gke_upgrade_baselines_tenant (tenant_id);
ALTER TABLE gke_upgrade_history ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE gke_upgrade_history ADD INDEX idx_gke_upgrade_history_tenant (tenant_id);
ALTER TABLE gke_version_schedule ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE gke_version_schedule ADD INDEX idx_gke_version_schedule_tenant (tenant_id);
ALTER TABLE harbor_registries ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE harbor_registries ADD INDEX idx_harbor_registries_tenant (tenant_id);
ALTER TABLE host_disks ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE host_disks ADD INDEX idx_host_disks_tenant (tenant_id);
ALTER TABLE hosts ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE hosts ADD INDEX idx_hosts_tenant (tenant_id);
ALTER TABLE k8s_changes ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_changes ADD INDEX idx_k8s_changes_tenant (tenant_id);
ALTER TABLE k8s_clusters ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_clusters ADD INDEX idx_k8s_clusters_tenant (tenant_id);
ALTER TABLE k8s_configmaps ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_configmaps ADD INDEX idx_k8s_configmaps_tenant (tenant_id);
ALTER TABLE k8s_endpoints ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_endpoints ADD INDEX idx_k8s_endpoints_tenant (tenant_id);
ALTER TABLE k8s_gateways ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_gateways ADD INDEX idx_k8s_gateways_tenant (tenant_id);
ALTER TABLE k8s_hpas ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_hpas ADD INDEX idx_k8s_hpas_tenant (tenant_id);
ALTER TABLE k8s_httproutes ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_httproutes ADD INDEX idx_k8s_httproutes_tenant (tenant_id);
ALTER TABLE k8s_ingresses ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_ingresses ADD INDEX idx_k8s_ingresses_tenant (tenant_id);
ALTER TABLE k8s_namespaces ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_namespaces ADD INDEX idx_k8s_namespaces_tenant (tenant_id);
ALTER TABLE k8s_node_alert_state ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_node_alert_state ADD INDEX idx_k8s_node_alert_state_tenant (tenant_id);
ALTER TABLE k8s_node_pools ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_node_pools ADD INDEX idx_k8s_node_pools_tenant (tenant_id);
ALTER TABLE k8s_node_version_events ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_node_version_events ADD INDEX idx_k8s_node_version_events_tenant (tenant_id);
ALTER TABLE k8s_nodes ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_nodes ADD INDEX idx_k8s_nodes_tenant (tenant_id);
ALTER TABLE k8s_ns_project ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_ns_project ADD INDEX idx_k8s_ns_project_tenant (tenant_id);
ALTER TABLE k8s_pdbs ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_pdbs ADD INDEX idx_k8s_pdbs_tenant (tenant_id);
ALTER TABLE k8s_pod_config_refs ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_pod_config_refs ADD INDEX idx_k8s_pod_config_refs_tenant (tenant_id);
ALTER TABLE k8s_pod_security ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_pod_security ADD INDEX idx_k8s_pod_security_tenant (tenant_id);
ALTER TABLE k8s_pod_volumes ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_pod_volumes ADD INDEX idx_k8s_pod_volumes_tenant (tenant_id);
ALTER TABLE k8s_pods ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_pods ADD INDEX idx_k8s_pods_tenant (tenant_id);
ALTER TABLE k8s_pvcs ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_pvcs ADD INDEX idx_k8s_pvcs_tenant (tenant_id);
ALTER TABLE k8s_secrets ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_secrets ADD INDEX idx_k8s_secrets_tenant (tenant_id);
ALTER TABLE k8s_services ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_services ADD INDEX idx_k8s_services_tenant (tenant_id);
ALTER TABLE k8s_sync_state ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_sync_state ADD INDEX idx_k8s_sync_state_tenant (tenant_id);
ALTER TABLE k8s_virtualservices ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_virtualservices ADD INDEX idx_k8s_virtualservices_tenant (tenant_id);
ALTER TABLE k8s_workloads ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE k8s_workloads ADD INDEX idx_k8s_workloads_tenant (tenant_id);
ALTER TABLE lark_groups ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE lark_groups ADD INDEX idx_lark_groups_tenant (tenant_id);
ALTER TABLE lifecycle_statuses ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE lifecycle_statuses ADD INDEX idx_lifecycle_statuses_tenant (tenant_id);
ALTER TABLE local_roles ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE local_roles ADD INDEX idx_local_roles_tenant (tenant_id);
ALTER TABLE mcp_config ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE mcp_config ADD INDEX idx_mcp_config_tenant (tenant_id);
ALTER TABLE notify_users ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE notify_users ADD INDEX idx_notify_users_tenant (tenant_id);
ALTER TABLE obs_endpoints ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE obs_endpoints ADD INDEX idx_obs_endpoints_tenant (tenant_id);
ALTER TABLE origin_ip_rules ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE origin_ip_rules ADD INDEX idx_origin_ip_rules_tenant (tenant_id);
ALTER TABLE projects ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE projects ADD INDEX idx_projects_tenant (tenant_id);
ALTER TABLE registrars ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE registrars ADD INDEX idx_registrars_tenant (tenant_id);
ALTER TABLE scheduled_tasks ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE scheduled_tasks ADD INDEX idx_scheduled_tasks_tenant (tenant_id);
ALTER TABLE settings ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE settings ADD INDEX idx_settings_tenant (tenant_id);
ALTER TABLE task_notify_users ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE task_notify_users ADD INDEX idx_task_notify_users_tenant (tenant_id);
ALTER TABLE task_run_logs ADD COLUMN tenant_id BIGINT UNSIGNED NOT NULL DEFAULT 1;
ALTER TABLE task_run_logs ADD INDEX idx_task_run_logs_tenant (tenant_id);

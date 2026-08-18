-- ConfigMap 名录 + Pod 配置引用关系
--
-- 目的：回答「这个 Pod 起不来，到底缺哪个配置」。此前 CMDB 只能看到
-- CreateContainerConfigError 这个结论，缺哪个 ConfigMap/Secret 必须登集群才知道。
--
-- 安全约束（重要）：
--   1. k8s_configmaps 只存 **键名**，绝不存 value。ConfigMap 的 value 里常有
--      数据库连接串、第三方地址等敏感信息，落库等于把它们复制到 CMDB。
--   2. Secret 一律不建名录表。只读 RBAC 里**没有** secrets 权限，也不打算加——
--      K8s 的 list secrets 会连 data 一起返回，给了这个权限就等于 CMDB
--      能读全集群所有密码。Secret 只通过 Pod spec 记录**引用关系**（谁引用了什么名字），
--      引用关系来自 pod spec，不需要任何额外权限。

CREATE TABLE IF NOT EXISTS k8s_configmaps (
  id          BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id  INT          NOT NULL,
  namespace   VARCHAR(128) NOT NULL,
  name        VARCHAR(253) NOT NULL,
  key_names   TEXT         COMMENT '键名列表，逗号分隔。只存键名，不存 value',
  key_count   INT          NOT NULL DEFAULT 0,
  KEY idx_cm_lookup (cluster_id, namespace, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='ConfigMap 名录（仅键名）';

-- Pod 对 ConfigMap/Secret 的引用关系，全部来自 pod spec。
-- optional=1 的引用缺失不会导致容器起不来，判定时必须排除，否则又是一批误报。
CREATE TABLE IF NOT EXISTS k8s_pod_config_refs (
  id          BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id  INT          NOT NULL,
  namespace   VARCHAR(128) NOT NULL,
  pod_name    VARCHAR(253) NOT NULL,
  container   VARCHAR(253) NOT NULL DEFAULT '' COMMENT '容器名；卷和 imagePullSecret 为空（属于 Pod 级）',
  ref_kind    VARCHAR(16)  NOT NULL COMMENT 'configmap | secret',
  ref_name    VARCHAR(253) NOT NULL,
  ref_key     VARCHAR(253) NOT NULL DEFAULT '' COMMENT '具体键名；空表示整体引用（envFrom/卷/拉取密钥）',
  source      VARCHAR(24)  NOT NULL COMMENT 'env | envFrom | volume | imagePullSecret',
  optional    TINYINT      NOT NULL DEFAULT 0 COMMENT '1=缺失也不影响启动',
  KEY idx_ref_target (cluster_id, namespace, ref_kind, ref_name),
  KEY idx_ref_pod (cluster_id, namespace, pod_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Pod 配置引用关系（来自 pod spec，不含任何配置内容）';

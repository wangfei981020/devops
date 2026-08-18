-- Pod 安全上下文
--
-- 补 CMDB-003 第 5 项里的「容器安全上下文完全查不到，安全审计盲区」：
-- privileged / hostNetwork / hostPID / hostPath / runAsRoot / 提权 / 额外 capabilities
-- 此前一律不可见，出了容器逃逸类问题连「哪些 Pod 有这个能力」都答不上来。
--
-- 数据全部来自 pod spec，采 Pod 时复用同一次 List，不需要任何新权限。

CREATE TABLE IF NOT EXISTS k8s_pod_security (
  id           BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id   INT          NOT NULL,
  namespace    VARCHAR(128) NOT NULL,
  pod_name     VARCHAR(253) NOT NULL,
  workload     VARCHAR(253) NOT NULL DEFAULT '',
  host_network TINYINT      NOT NULL DEFAULT 0,
  host_pid     TINYINT      NOT NULL DEFAULT 0,
  host_ipc     TINYINT      NOT NULL DEFAULT 0,
  privileged   TEXT         COMMENT '特权容器名，逗号分隔',
  run_as_root  TINYINT      NOT NULL DEFAULT 0 COMMENT '1=以 root 运行（显式 runAsUser=0 或未设 runAsNonRoot）',
  priv_esc     TINYINT      NOT NULL DEFAULT 0 COMMENT '1=允许提权 allowPrivilegeEscalation',
  added_caps   TEXT         COMMENT '额外授予的 Linux capabilities，逗号分隔',
  host_paths   TEXT         COMMENT '挂载的宿主机路径，逗号分隔',
  KEY idx_sec_cluster (cluster_id, namespace),
  KEY idx_sec_pod (cluster_id, namespace, pod_name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Pod 安全上下文（来自 pod spec）';

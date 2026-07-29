-- Secret 名录（按集群开关，默认关闭）
--
-- 背景：CMDB-004/DEV-006 里刻意没做 Secret 名录，因为 K8s 的 list secrets 会连 data
-- 一并返回，等于让 CMDB 能读全集群所有密码。但代价是「Pod 起不来缺哪个 Secret」
-- 只能靠事件佐证——从未启动过的 Pod（事件已过 TTL）就查不出来了。
--
-- 用户决定：**DEV 环境放开，UAT/生产不放**。所以做成集群级开关，默认关闭。
--
-- 三层控制，缺一不可：
--   1. 集群 RBAC —— 只给 DEV 的只读 SA 加 secrets:[list]，其它集群不加（最硬的一层）
--   2. 本开关   —— 即使有权限，没开开关也不采
--   3. 代码     —— 用 metadata-only 客户端，APIServer 只返回 name/namespace/type，
--                  **根本不把 data 发过来**，CMDB 进程从不接触 Secret 内容
--
-- 因此 k8s_secrets 表里没有、也不可能有键名或值：metadata 里根本没有这些。
-- 对 Secret 只能判「存不存在」，判不了「键对不对」——这是有意的取舍。

ALTER TABLE k8s_clusters
  ADD COLUMN allow_secret_inventory TINYINT NOT NULL DEFAULT 0
  COMMENT '1=允许采集该集群的 Secret 名录（仅名字，不含内容）。需该集群只读 SA 具备 secrets:list 权限';

CREATE TABLE IF NOT EXISTS k8s_secrets (
  id         BIGINT AUTO_INCREMENT PRIMARY KEY,
  cluster_id INT          NOT NULL,
  namespace  VARCHAR(128) NOT NULL,
  name       VARCHAR(253) NOT NULL,
  type       VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'kubernetes.io/dockerconfigjson 等',
  KEY idx_secret_lookup (cluster_id, namespace, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
  COMMENT='Secret 名录：只有名字/命名空间/类型，绝无键名与内容（metadata-only 采集）';

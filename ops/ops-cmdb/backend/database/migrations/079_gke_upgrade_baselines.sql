-- 升级前基线快照持久化（CMDB-030）
--
-- 缺口：升级预案里的「升级前基线」（Pod 总数、Running/Failed/Pending、已存在的异常清单）
-- 是每次生成预案时**实时查库**得出的，不是快照。
-- 后果是升完再生成一次预案，「基线」就是升级后的状态，**没有任何东西可以对比**——
-- 而预案自己的验证清单里写着「升完重新生成一次，用升级前基线逐项比对」，这条根本做不到。
--
-- 基线的全部价值就是事后区分「新坏的」和「本来就坏的」。
-- 2026-07-31 UAT 升级实测了这个价值：升级前有 32 条已知异常
-- （gitlab-runner 重启 2400+、filebeat 9 个 OOM、skywalking 全崩），
-- 升级后凭基线一眼挑出唯一真正新增的问题（istio-k8s-fluentd 9 副本 CrashLoop，见 UAT-017）。
-- 那次是靠人工把预案 JSON 手动落盘到 docs 才保住的，不该依赖人记得做这一步。
--
-- payload_json 存整份基线而不是拆成列：基线的字段会随预案演进（后来加了 pods_collected），
-- 拆列意味着每次加字段都要迁移，而这张表只做「原样存、原样取回来比对」，不需要按字段查询。

CREATE TABLE IF NOT EXISTS gke_upgrade_baselines (
  id           BIGINT       AUTO_INCREMENT PRIMARY KEY,
  cluster_id   INT          NOT NULL,
  -- 生成该基线时预案的目标版本，用于回答「这份基线是为哪次升级留的」
  target_version VARCHAR(64) NOT NULL DEFAULT '',
  taken_at     DATETIME     NOT NULL,
  -- 整份 baseline 结构的 JSON（规模数字 + known_bad 清单 + pods_collected 标志）
  payload_json MEDIUMTEXT   NOT NULL,
  created_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_cluster_time (cluster_id, taken_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- GKE 各区域可用版本清单（升级目标版本的合法取值来源）
--
-- 背景：升级预案的目标版本此前只能手输。CMDB 不知道哪些版本合法，
-- 输错一个字符照样把预案算出来，要到 GCP 控制台下拉框里才发现选不到——
-- 而那时候人已经在升级窗口里了。
--
-- 权威来源是 GKE 的 getServerConfig，按区域返回该区域全部可用的
-- 控制面版本(validMasterVersions)与节点版本(validNodeVersions)，官方按降序给。
-- 采到之后前端就能给下拉框，且预案能校验「你填的这个版本这个区域到底有没有」。
--
-- 按 (project_id, location) 存而不是按集群：同一区域的所有集群看到的可用版本相同，
-- 按集群存会把同一份列表重复 N 遍。

CREATE TABLE IF NOT EXISTS gke_available_versions (
  id         BIGINT      AUTO_INCREMENT PRIMARY KEY,
  project_id VARCHAR(128) NOT NULL,
  location   VARCHAR(64)  NOT NULL,
  -- master=控制面可选版本  node=节点池可选版本。两者不一定完全一致
  kind       VARCHAR(8)   NOT NULL,
  version    VARCHAR(48)  NOT NULL,
  -- 官方返回顺序（降序，0 最新）。前端按这个排，别自己按字符串排——
  -- "1.35.6-gke.1127000" 和 "1.35.6-gke.98000" 字符串比大小是错的
  sort_order INT          NOT NULL DEFAULT 0,
  synced_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_ver (project_id, location, kind, version),
  KEY idx_loc (project_id, location, kind, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

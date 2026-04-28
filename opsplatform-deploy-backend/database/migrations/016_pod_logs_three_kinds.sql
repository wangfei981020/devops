-- Migration 016: 失败 pod 归档拆成 3 种 (previous / current / events)
-- 旧 object_key 存的就是 --previous 日志，搬到 previous_key 列。
-- 加 unique (deployment_id, argocd_app, pod_name) 给后续 ON DUPLICATE KEY UPDATE 用。

-- 1. 加新列（NULL = 该 kind 内容为空，前端 tab 显空状态）
ALTER TABLE deployment_pod_logs ADD COLUMN previous_key  VARCHAR(512) NULL AFTER object_key;
ALTER TABLE deployment_pod_logs ADD COLUMN previous_size INT NOT NULL DEFAULT 0;
ALTER TABLE deployment_pod_logs ADD COLUMN current_key   VARCHAR(512) NULL AFTER previous_size;
ALTER TABLE deployment_pod_logs ADD COLUMN current_size  INT NOT NULL DEFAULT 0;
ALTER TABLE deployment_pod_logs ADD COLUMN events_key    VARCHAR(512) NULL AFTER current_size;
ALTER TABLE deployment_pod_logs ADD COLUMN events_size   INT NOT NULL DEFAULT 0;

-- 2. 老数据迁移：object_key 当时存的就是 --previous 的快照
UPDATE deployment_pod_logs SET previous_key = object_key, previous_size = size_bytes
  WHERE object_key IS NOT NULL AND object_key != '';

-- 3. 去重：以前没 unique 索引，理论上不会重，但生产万一有 dup 先清掉再加
DELETE t1 FROM deployment_pod_logs t1
INNER JOIN deployment_pod_logs t2
WHERE t1.deployment_id = t2.deployment_id
  AND t1.argocd_app = t2.argocd_app
  AND t1.pod_name = t2.pod_name
  AND t1.id < t2.id;

-- 4. 加 unique 索引：让后续归档用 ON DUPLICATE KEY UPDATE 累积 3 种 kind
ALTER TABLE deployment_pod_logs ADD UNIQUE INDEX uniq_dep_app_pod (deployment_id, argocd_app, pod_name);

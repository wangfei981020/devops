-- 回填存量域名的 last_synced_at：从已有 dns_records 的最近同步时间迁过来，
-- 避免 022 新加的列为 NULL 导致所有存量域名被误判"未同步"。
UPDATE domains d
SET d.last_synced_at = (SELECT MAX(dr.synced_at) FROM dns_records dr WHERE dr.domain_ci_id = d.ci_id)
WHERE d.last_synced_at IS NULL
  AND EXISTS (SELECT 1 FROM dns_records dr WHERE dr.domain_ci_id = d.ci_id);

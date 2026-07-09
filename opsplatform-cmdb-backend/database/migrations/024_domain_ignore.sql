-- 主域名忽略：不用了的死域名(过期/已移出账号/DNS已迁移等)可忽略，同步跳过、不再报未同步
ALTER TABLE domains ADD COLUMN ignored       TINYINT      NOT NULL DEFAULT 0;
ALTER TABLE domains ADD COLUMN ignore_reason VARCHAR(255) NOT NULL DEFAULT '';

-- 补回填 0 记录的存量 sync 域名的 last_synced_at（origin=sync 即已纳管过；有记录用记录时间，没记录用当前时间），
-- 彻底消除"0 记录域名误报未同步"。
UPDATE domains d
SET d.last_synced_at = COALESCE(
      (SELECT MAX(dr.synced_at) FROM dns_records dr WHERE dr.domain_ci_id = d.ci_id),
      NOW())
WHERE d.last_synced_at IS NULL AND d.origin = 'sync';

-- 域名同步状态：独立记录"最近同步时刻"(不再从 dns_records MAX 反推，0 记录也算已同步)
-- + DNS 已迁移标记(域名还在数据源账户，但 NS/DNS 解析迁到别处，拉不到记录)
ALTER TABLE domains ADD COLUMN last_synced_at DATETIME NULL;
ALTER TABLE domains ADD COLUMN dns_migrated   TINYINT NOT NULL DEFAULT 0;

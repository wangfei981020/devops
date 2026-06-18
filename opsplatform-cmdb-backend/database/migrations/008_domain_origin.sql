-- 主域名来源标识：manual=手动录入，sync=从数据源(GoDaddy/其他厂商)同步进来。
-- 具体哪家厂商由 registrar_id 关联的数据源 provider 体现。现有手动录入数据默认 manual。
ALTER TABLE domains ADD COLUMN origin VARCHAR(16) NOT NULL DEFAULT 'manual' COMMENT 'manual=手动录入, sync=数据源同步';

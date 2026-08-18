-- 手动 DNS-01 验证：记录需用户手动添加的 TXT 记录 + 用户确认已添加的标志
ALTER TABLE certificates ADD COLUMN challenge_fqdn  VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE certificates ADD COLUMN challenge_value VARCHAR(512) NOT NULL DEFAULT '';
ALTER TABLE certificates ADD COLUMN dns_ready       TINYINT      NOT NULL DEFAULT 0;

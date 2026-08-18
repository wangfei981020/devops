-- domain_renewals.env 原 VARCHAR(32) 装不下 OTE 测试环境标签(含 base_url)，改宽到 128。
-- 对已应用 030 的库补齐（新装库 030 已是 128，此 MODIFY 幂等无害）。
ALTER TABLE domain_renewals MODIFY env VARCHAR(128) NOT NULL DEFAULT '';

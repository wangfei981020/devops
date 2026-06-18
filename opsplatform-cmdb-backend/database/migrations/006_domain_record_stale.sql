-- 解析记录加「厂商已删除」标记：同步时 GoDaddy 已不存在的 A/CNAME 标为失效，保留业务字段，由人工确认后删除。
ALTER TABLE domain_records ADD COLUMN stale TINYINT NOT NULL DEFAULT 0 COMMENT '1=厂商(GoDaddy)已删除，待人工确认';

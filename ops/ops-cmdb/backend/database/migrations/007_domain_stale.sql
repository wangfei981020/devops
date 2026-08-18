-- 主域名加「厂商已删除」标记：整体同步时数据源(GoDaddy)下已不存在的域名标为失效，保留业务信息，由人工确认后删。
ALTER TABLE domains ADD COLUMN stale TINYINT NOT NULL DEFAULT 0 COMMENT '1=厂商(数据源)已不存在该域名，待人工确认';

-- 解析证书巡检：标记「不需要证书/忽略检测」的解析（内网、纯解析、邮件等），填忽略原因。
-- 忽略的从「快到期/检测失败」里排除，但可在巡检页「已忽略」筛选单独查看。
ALTER TABLE domain_records ADD COLUMN cert_ignored TINYINT NOT NULL DEFAULT 0 COMMENT '1=忽略证书巡检（不需要证书）';
ALTER TABLE domain_records ADD COLUMN cert_ignore_reason VARCHAR(255) NOT NULL DEFAULT '' COMMENT '忽略原因';

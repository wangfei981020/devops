-- 续费台账记录 GoDaddy 返回的实际扣费金额（此前只存客户端报价 quoted_amount）。
-- 对账用：actual vs quoted 可比对，超阈值可告警。厂商不一定返回金额，返回则填。
ALTER TABLE domain_renewals ADD COLUMN actual_amount DECIMAL(12,2) NOT NULL DEFAULT 0;
ALTER TABLE domain_renewals ADD COLUMN actual_currency VARCHAR(8) NOT NULL DEFAULT '';

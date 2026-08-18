-- 主机头「忽略」：项目里没用到的主机头标忽略，台账默认不展示、也不计入证书巡检/总览统计（区别于 cert_ignored=只从证书检查排除）
ALTER TABLE domain_records ADD COLUMN ignored TINYINT NOT NULL DEFAULT 0;
ALTER TABLE domain_records ADD COLUMN ignore_reason VARCHAR(255) NOT NULL DEFAULT '';

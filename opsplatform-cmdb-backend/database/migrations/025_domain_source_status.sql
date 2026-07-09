-- 存 GoDaddy(数据源) 返回的域名状态，用于识别"已转出/取消/没收"等消亡状态：
-- 这些状态的域名 GoDaddy API 仍会返回一段时间，此前被当成正常域名，导致转出后仍显示活跃。
ALTER TABLE domains ADD COLUMN source_status VARCHAR(32) NOT NULL DEFAULT '';

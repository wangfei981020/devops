-- 主机补 GCP 同步能自带获取的只读技术字段
ALTER TABLE hosts ADD COLUMN hostname            VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN vpc                 VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN subnet              VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN network_tags        VARCHAR(512) NOT NULL DEFAULT '';   -- 逗号分隔防火墙标签
ALTER TABLE hosts ADD COLUMN preemptible         TINYINT      NOT NULL DEFAULT 0;    -- 1=抢占式/Spot(会被回收)
ALTER TABLE hosts ADD COLUMN image               VARCHAR(255) NOT NULL DEFAULT '';   -- 启动盘镜像
ALTER TABLE hosts ADD COLUMN cpu_platform        VARCHAR(64)  NOT NULL DEFAULT '';
ALTER TABLE hosts ADD COLUMN deletion_protection TINYINT      NOT NULL DEFAULT 0;
ALTER TABLE hosts ADD COLUMN service_accounts    VARCHAR(512) NOT NULL DEFAULT '';   -- 逗号分隔绑定 SA

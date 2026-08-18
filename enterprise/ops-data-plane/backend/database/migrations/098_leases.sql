-- 分布式租约表：多副本下决定"谁来跑定时任务"。
--
-- ⚠️ 这是**平台级**表，不带 tenant_id。
--    租约协调的是进程，不是数据 —— 「谁是 leader」这件事对所有租户是同一个答案。
--    加 tenant_id 反而会让每个租户各选一个 leader，等于没有约束住任务只跑一次。
--    对应 store/whitelist.go 里的白名单条目。
CREATE TABLE IF NOT EXISTS leases (
  name        VARCHAR(128) NOT NULL COMMENT '租约名，如 scheduler',
  owner       VARCHAR(255) NOT NULL COMMENT '持有者标识，用 Pod 名',
  -- 用 DATETIME(6) 而不是 DATETIME：TTL 常在秒级，秒精度会让
  -- "刚好到期"的判断出现整整一秒的模糊地带，两个副本可能同时认为自己拿到了。
  expires_at  DATETIME(6)  NOT NULL COMMENT '到期时刻',
  -- fence 栅栏令牌，每次易主 +1。非幂等操作带上它做条件写，
  -- 就能挡住"停顿后醒来、以为自己还是 leader"的旧持有者。
  fence       BIGINT       NOT NULL DEFAULT 1 COMMENT '栅栏令牌，易主时递增',
  updated_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (name),
  KEY idx_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='多副本租约';

-- 跨副本的外部 API 调用配额计数。
--
-- # 为什么必须落库
--
-- 原来 dnsource.Limiter 是 `var limiters sync.Map` —— **进程内**的。
-- 它的职责是"别把 GoDaddy 的限流撞了"：客户端阈值 50/分钟，
-- GoDaddy 真限制 60/分钟，留 10 个缓冲。
--
-- 单副本时这个算术是对的。多副本下每个 Pod 各有一份自己的计数器，
-- 2 副本 = 100/分钟，直接越过 GoDaddy 的 60。
--
-- ⚠️ 和证书 bundle 那个按 IP 的限流器不是一回事：
-- 那个是"减速带 + 信号"，多算一倍无非是减速带松一点；
-- 这个是**用来不越过外部硬限制的**，多算一倍等于保护失效。
-- 撞上之后的现象是批量续费里"莫名其妙有几个没续上"。
--
-- # 为什么是固定窗口而不是令牌桶
--
-- 要对齐的是 GoDaddy 自己的窗口口径（按分钟计），令牌桶的平滑特性
-- 反而会让"这一分钟到底打了几次"算不准。
CREATE TABLE IF NOT EXISTS api_quota_windows (
  -- 配额作用域，如 dnsource:12（一个数据源一份配额，与厂商账号一一对应）
  scope       VARCHAR(128) NOT NULL,
  -- 窗口起点的 unix 分钟数（time.Now().Unix()/60）
  window_min  BIGINT       NOT NULL,
  used        INT          NOT NULL DEFAULT 0,
  updated_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (scope, window_min),
  KEY idx_window (window_min)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='外部 API 调用配额（跨副本），按分钟固定窗口';

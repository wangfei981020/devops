-- 批量续费任务的进度落库。
--
-- # 为什么必须落库
--
-- 原来这份进度只在**发起请求的那个 Pod 的内存里**。单副本时够用，
-- 多副本下：A 副本起任务并返回 job_id，前端下一次轮询被负载均衡打到 B 副本，
-- B 的那份 map 是空的 → 立刻 404「任务不存在或已过期（超过 2 小时）」。
--
-- 这句话在多副本下是**假的**：任务刚起来一秒，既没有不存在也没有过期。
-- 而用户看到它时，钱正在被扣 —— 他会以为续费没开始，然后再点一次。
--
-- ⚠️ 与续费互斥（cluster.Mutex）是同一类问题的两个面：
-- 那个防的是"两个副本各扣一次钱"，这个防的是"看不到自己那次扣了没"。
--
-- 进度是**临时数据**：2 小时后清掉，真正的追溯看 domain_renewals 台账。
CREATE TABLE IF NOT EXISTS domain_renew_jobs (
  id         VARCHAR(64)  NOT NULL COMMENT '任务 ID，返回给前端轮询',
  tenant_id  BIGINT       NOT NULL,
  total      INT          NOT NULL DEFAULT 0,
  done       INT          NOT NULL DEFAULT 0,
  succeeded  INT          NOT NULL DEFAULT 0,
  failed     INT          NOT NULL DEFAULT 0,
  -- 已扣费但没拿到厂商确认。它既不是成功也不是失败，必须单独计数
  uncertain  INT          NOT NULL DEFAULT 0,
  finished   TINYINT      NOT NULL DEFAULT 0,
  msg        VARCHAR(500) NOT NULL DEFAULT '',
  -- 逐域名结果的 JSON。只在这 2 小时里有用，之后看 domain_renewals
  items_json MEDIUMTEXT,
  operator   VARCHAR(255) NOT NULL DEFAULT '',
  started_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_started (started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='批量续费进度，2 小时后清理';

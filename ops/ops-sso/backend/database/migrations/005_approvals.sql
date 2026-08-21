-- 临时提权：申请 → 审批 → 到期自动回收。
--
-- # 为什么不复用 access_policies
--
-- 授权规则是"常设状态"，临时提权是"有生命周期的事件"：它有申请人、理由、
-- 审批人、有效期、回收记录。塞进同一张表会让"当前生效的规则"这个查询
-- 变得没法看，也会让常设授权的复核混进一堆早已过期的临时条目。
--
-- 生效中的提权在判定时**动态叠加**到规则集上（见 grant.ActiveRules）。

CREATE TABLE IF NOT EXISTS access_requests (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id     BIGINT UNSIGNED NOT NULL,

  requester_id  BIGINT UNSIGNED NOT NULL,
  app_id        BIGINT UNSIGNED NOT NULL,
  -- 申请的粒度：整个应用，或某个路径前缀。
  -- 允许后者是因为"我只是想删一个仓库"和"给我整个 Harbor"风险差着量级。
  scope         VARCHAR(255) NOT NULL DEFAULT '',

  reason        VARCHAR(1024) NOT NULL COMMENT '为什么需要 —— 审批人只看这一句',
  ticket_ref    VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '关联工单',
  duration_sec  INT          NOT NULL COMMENT '要多久',

  status        VARCHAR(16)  NOT NULL DEFAULT 'pending'
                COMMENT 'pending/approved/rejected/blocked/expired/revoked/cancelled',
  risk          VARCHAR(8)   NOT NULL DEFAULT 'low' COMMENT 'low/medium/high',

  -- SoD 拦截。**存下来而不是当场算完就丢**：
  -- 事后审计要能回答"这条为什么没批"，而不是只看到一个 rejected。
  blocked_rule  VARCHAR(64)  NOT NULL DEFAULT '',
  blocked_note  VARCHAR(512) NOT NULL DEFAULT '',

  approver_id   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  approver_note VARCHAR(512) NOT NULL DEFAULT '',
  decided_at    DATETIME NULL,

  -- 批准后的生效窗口。到点由回收任务处理。
  granted_at    DATETIME NULL,
  expires_at    DATETIME NULL,
  revoked_at    DATETIME NULL,
  revoke_reason VARCHAR(64) NOT NULL DEFAULT '',

  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  KEY idx_req_tenant_status (tenant_id, status, created_at),
  KEY idx_req_requester (tenant_id, requester_id, created_at),
  -- 回收任务按这个索引扫：生效中且已过期
  KEY idx_req_expiry (tenant_id, status, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 职责分离规则。
--
-- 两个能力互斥：同一个人不能同时持有。命中即拒绝，**运维不可绕过** ——
-- 能被运维绕过的 SoD，在审计眼里等于没有。
CREATE TABLE IF NOT EXISTS sod_rules (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id   BIGINT UNSIGNED NOT NULL,
  code        VARCHAR(32)  NOT NULL COMMENT '如 SoD-03，审计里引用它',
  name        VARCHAR(128) NOT NULL,
  -- 用能力标签而不是应用 ID：应用会增减，"生产发布"这个能力不会。
  capability_a VARCHAR(64) NOT NULL,
  capability_b VARCHAR(64) NOT NULL,
  note        VARCHAR(512) NOT NULL DEFAULT '',
  enabled     TINYINT(1) NOT NULL DEFAULT 1,
  created_by  BIGINT UNSIGNED NOT NULL DEFAULT 0,
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_sod_code (tenant_id, code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 应用具备哪些能力标签。一个应用可以有多个（Harbor 既是"制品库"也是"生产发布"）。
CREATE TABLE IF NOT EXISTS app_capabilities (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  app_id     BIGINT UNSIGNED NOT NULL,
  capability VARCHAR(64) NOT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_app_cap (tenant_id, app_id, capability),
  KEY idx_cap (tenant_id, capability)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 拨测执行记录。
--
-- 单独一张表而不是只在 probe_credentials 上覆盖最后结果：
-- 「30 天成功率」「什么时候开始坏的」这两个问题都需要历史。
CREATE TABLE IF NOT EXISTS probe_runs (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id   BIGINT UNSIGNED NOT NULL,
  app_id      BIGINT UNSIGNED NOT NULL,
  started_at  DATETIME(3) NOT NULL,
  duration_ms INT NOT NULL DEFAULT 0,
  result      VARCHAR(16) NOT NULL COMMENT 'ok/fail/skipped',
  -- 失败发生在哪一跳：dns/tcp/tls/http/login/upstream。
  -- 只记"失败"而不记哪一跳，排障时等于没记。
  failed_hop  VARCHAR(16) NOT NULL DEFAULT '',
  detail      VARCHAR(512) NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  KEY idx_run_app_time (tenant_id, app_id, started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

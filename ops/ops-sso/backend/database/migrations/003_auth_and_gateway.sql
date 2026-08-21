-- 会话认证、应急通道、拨测探针、路径级策略。
--
-- 这一批表对应六角色验证里的三个 P0（见 docs/design/onegate/REVIEW-6ROLES.md）：
--   P0-2 拨测探针是新增常设凭据 → probe_credentials 把约束写进表结构，不靠自觉
--   P0-3 应急通道环形依赖      → break_glass_codes 是离线一次性口令，不依赖短信/IdP
--   P0-1 策略调序即改语义      → path_rules **没有** order 字段，见下方说明

-- ══════════════════════════════════════════════════════════════════
-- 认证（平台级：会话属于人，不属于租户）
-- ══════════════════════════════════════════════════════════════════

CREATE TABLE IF NOT EXISTS local_credentials (
  user_id      BIGINT UNSIGNED NOT NULL,
  password_hash VARBINARY(255) NOT NULL COMMENT 'bcrypt，永不出库；任何接口都不返回它',
  must_change  TINYINT(1)  NOT NULL DEFAULT 0,
  updated_at   DATETIME    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  PRIMARY KEY (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 会话。
--
-- 存的是**令牌的 SHA-256**，不是令牌本身：库被拖走也无法拿去登录。
-- 这是最便宜的一道防线，代价只有一次哈希。
CREATE TABLE IF NOT EXISTS auth_sessions (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id      BIGINT UNSIGNED NOT NULL,
  tenant_id    BIGINT UNSIGNED NOT NULL COMMENT '登录时选定的租户，会话期内不变',
  token_hash   CHAR(64)     NOT NULL COMMENT 'sha256(令牌) 的十六进制',
  source       VARCHAR(24)  NOT NULL DEFAULT 'local' COMMENT 'local/oidc/break_glass',
  client_ip    VARCHAR(64)  NOT NULL DEFAULT '',
  user_agent   VARCHAR(255) NOT NULL DEFAULT '',
  created_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  last_seen_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at   DATETIME     NOT NULL,
  revoked_at   DATETIME     NULL COMMENT '离职断权/主动下线时置；到期回收也走它',
  revoke_reason VARCHAR(64) NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  UNIQUE KEY uq_session_token (token_hash),
  KEY idx_session_user (user_id, revoked_at),
  KEY idx_session_expire (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ══════════════════════════════════════════════════════════════════
-- 应急通道（P0-3）
-- ══════════════════════════════════════════════════════════════════

-- 离线一次性口令。
--
-- # 为什么不是短信验证码
--
-- 上一次断源演练暴露的问题：应急通道的验证码短信走的正是被断开的那条链路 ——
-- **应急通道依赖了它要救的东西**。这类环形依赖只有真断一次才会暴露。
--
-- 所以应急口令必须满足三条：
--   1. **预先生成、离线分发**（打印封存），使用时不需要任何外部系统
--   2. 一次性，用过即废
--   3. 使用后强制留痕并通知（通知可以延迟送达，但不能是登录的前置条件）
--
-- 存的是哈希，且只在生成的那一刻明文出现一次。
CREATE TABLE IF NOT EXISTS break_glass_codes (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  user_id     BIGINT UNSIGNED NOT NULL COMMENT '绑定到具体应急账号，不做通用口令',
  code_hash   CHAR(64)   NOT NULL COMMENT 'sha256(口令)',
  batch       VARCHAR(32) NOT NULL DEFAULT '' COMMENT '同一次签发的一批，便于整批作废',
  issued_by   BIGINT UNSIGNED NOT NULL DEFAULT 0,
  issued_at   DATETIME   NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at  DATETIME   NOT NULL,
  used_at     DATETIME   NULL,
  used_ip     VARCHAR(64) NOT NULL DEFAULT '',
  revoked_at  DATETIME   NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_bg_code (code_hash),
  KEY idx_bg_user (user_id, used_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ══════════════════════════════════════════════════════════════════
-- 拨测探针（P0-2）
-- ══════════════════════════════════════════════════════════════════

-- 拨测探针账号。
--
-- # 为什么单独一张表，而不是复用普通账号
--
-- 24 个应用 × 每 5 分钟 = 每天约 7000 次自动登录，这组凭据必须长期有效 ——
-- 它是我们**自己给客户造出来的新攻击面**。所以约束写进表结构，不靠自觉：
--   · read_only 恒为 1（写死在 CHECK 里，改不了）
--   · allowed_cidr 必填，只有网关网段能用
--   · 单独的审计动作前缀 probe.*，不与真人登录混在一条流里
--   · revoked_at 一键吊销，且吊销后拨测显示「未覆盖 —」而不是「健康」
CREATE TABLE IF NOT EXISTS probe_credentials (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id    BIGINT UNSIGNED NOT NULL,
  app_id       BIGINT UNSIGNED NOT NULL,
  username     VARCHAR(128) NOT NULL,
  secret_enc   VARBINARY(512) NOT NULL COMMENT 'AES-GCM 密文，密钥来自 KMS/环境，不进库',
  allowed_cidr VARCHAR(255) NOT NULL COMMENT '只允许这些来源使用，留空=禁用',
  read_only    TINYINT(1)   NOT NULL DEFAULT 1,
  interval_sec INT          NOT NULL DEFAULT 300 COMMENT '按应用可调：老系统扛不住 5 分钟一次',
  quiet_hours  VARCHAR(32)  NOT NULL DEFAULT '' COMMENT '静默窗口，如 02:00-04:00',
  last_probe_at DATETIME    NULL,
  last_result  VARCHAR(16)  NOT NULL DEFAULT '' COMMENT 'ok/fail/空=从没测过（界面显示 —，不显示健康）',
  last_detail  VARCHAR(512) NOT NULL DEFAULT '',
  revoked_at   DATETIME     NULL,
  created_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_probe_app (tenant_id, app_id),
  CONSTRAINT ck_probe_readonly CHECK (read_only = 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- ══════════════════════════════════════════════════════════════════
-- 路径级策略（P0-1）
-- ══════════════════════════════════════════════════════════════════

-- 网关判定用的细粒度规则：路径 + 方法 + 环境 + 时间 + 设备。
--
-- # 这张表**没有** order / priority 字段，这是刻意的
--
-- 原型里画的是「自上而下首次命中生效」的有序列表。六角色验证时测试提了个
-- P0：顺序即语义，把「设备未纳管→拒绝」拖到第一条，全公司服务账号瞬间被拒，
-- 而当时只有「保存前预演」，没有「调序预演」。
--
-- 与其补一个调序预演，不如**让顺序不再是语义的一部分**：
-- 判定按具体度排（路径越长越具体、方法指定的比通配的具体、带条件的比不带的具体），
-- 同具体度时 deny > challenge > allow。于是：
--   · 规则怎么排都不影响判定结果 → 不存在「拖一下就出事」
--   · 界面上仍可按具体度展示，人看到的顺序就是真实的判定顺序
--   · 代价：不能再写「先放行 A，再拒绝 A 的子集」这种依赖顺序的技巧 ——
--     但那种写法本来就是事故来源，用更具体的规则表达同样的意图更清楚
CREATE TABLE IF NOT EXISTS path_rules (
  id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id     BIGINT UNSIGNED NOT NULL,
  app_id        BIGINT UNSIGNED NOT NULL,
  methods       VARCHAR(64)  NOT NULL DEFAULT '*' COMMENT '逗号分隔，* = 全部',
  path_pattern  VARCHAR(255) NOT NULL DEFAULT '/**' COMMENT '前缀匹配，/** 结尾表示子树',
  subject_type  VARCHAR(16)  NOT NULL DEFAULT 'public',
  subject_id    BIGINT UNSIGNED NOT NULL DEFAULT 0,
  decision      VARCHAR(16)  NOT NULL COMMENT 'allow/challenge/deny',
  require_ticket TINYINT(1)  NOT NULL DEFAULT 0 COMMENT '必须绑定进行中的工单',
  device_state  VARCHAR(16)  NOT NULL DEFAULT '' COMMENT '空=不限；managed/unmanaged',
  time_window   VARCHAR(32)  NOT NULL DEFAULT '' COMMENT '空=不限；如 08:00-22:00',
  source_kind   VARCHAR(16)  NOT NULL DEFAULT '' COMMENT '空=不限；office/vpn/internet',
  mfa_ttl_sec   INT          NOT NULL DEFAULT 1800 COMMENT 'challenge 通过后的有效期',
  note          VARCHAR(512) NOT NULL DEFAULT '',
  created_by    BIGINT UNSIGNED NOT NULL DEFAULT 0,
  created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted_at    DATETIME NULL,
  del_key       BIGINT UNSIGNED NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uq_path_rule (tenant_id, app_id, methods, path_pattern, subject_type, subject_id, del_key),
  KEY idx_path_rule_app (tenant_id, app_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 网关代管的路由：Host → 应用。网关按 Host 找到应用，再判定。
CREATE TABLE IF NOT EXISTS app_routes (
  id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id   BIGINT UNSIGNED NOT NULL,
  app_id      BIGINT UNSIGNED NOT NULL,
  host        VARCHAR(255) NOT NULL COMMENT '对外域名',
  upstream    VARCHAR(255) NOT NULL COMMENT '后端地址，如 http://10.42.6.31:8080',
  inject_mode VARCHAR(16)  NOT NULL DEFAULT 'header' COMMENT 'header/formfill/cookie',
  created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  deleted_at  DATETIME NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_route_host (host),
  KEY idx_route_app (tenant_id, app_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 每一次网关判定一条。审计的主表。
CREATE TABLE IF NOT EXISTS access_events (
  id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id    BIGINT UNSIGNED NOT NULL,
  request_id   VARCHAR(64)  NOT NULL,
  occurred_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  user_id      BIGINT UNSIGNED NOT NULL DEFAULT 0,
  user_label   VARCHAR(128) NOT NULL DEFAULT '' COMMENT '当时的显示名，人改名后仍能看懂历史',
  app_id       BIGINT UNSIGNED NOT NULL DEFAULT 0,
  app_code     VARCHAR(64)  NOT NULL DEFAULT '',
  method       VARCHAR(8)   NOT NULL DEFAULT '',
  path         VARCHAR(512) NOT NULL DEFAULT '',
  decision     VARCHAR(16)  NOT NULL COMMENT 'allow/challenge/deny',
  reason       VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '原因码，不是中文',
  matched_rule BIGINT UNSIGNED NOT NULL DEFAULT 0,
  -- ★ 少了策略版本号，三个月后没人能复现「当时为什么放行」——策略早就改过了
  policy_version VARCHAR(64) NOT NULL DEFAULT '',
  client_ip    VARCHAR(64)  NOT NULL DEFAULT '',
  device_state VARCHAR(16)  NOT NULL DEFAULT '',
  gateway      VARCHAR(32)  NOT NULL DEFAULT '',
  latency_ms   INT          NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  KEY idx_event_tenant_time (tenant_id, occurred_at),
  KEY idx_event_user (tenant_id, user_id, occurred_at),
  KEY idx_event_decision (tenant_id, decision, occurred_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

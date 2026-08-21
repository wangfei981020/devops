-- 事件：生命周期、时间线、通知投递、判定链。
--
-- 事件（incident）不是告警（alert）：同一指纹的 N 次命中收敛成一条事件，
-- 认领、升级、静默、恢复都挂在它身上。上一代只有「发送流水」，
-- 回答不了「现在几个问题没解决、谁在处理、平均多久恢复」。

CREATE TABLE IF NOT EXISTS incidents (
  id            BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id     BIGINT       NOT NULL,
  rule_id       BIGINT       NULL,
  -- 来源：rule（自建规则）/ inbound（入站 webhook）
  source        VARCHAR(32)  NOT NULL DEFAULT 'rule',
  fingerprint   CHAR(40)     NOT NULL,
  -- ⚠️ 活跃唯一键：值 = fingerprint（活跃时）/ NULL（已恢复）。
  -- MySQL 没有部分唯一索引，而 NULL 不参与唯一性比较，
  -- 所以这一列既能保证「同一指纹同时只有一条活跃事件」，
  -- 又允许同一指纹历史上恢复过很多次。
  -- 直接对 (tenant_id, fingerprint) 建唯一索引会导致第二次故障插不进去，
  -- 现象是「问题复发了但界面上什么都没有」。
  active_key    CHAR(40)     NULL,
  title         VARCHAR(512) NOT NULL,
  severity      VARCHAR(16)  NOT NULL DEFAULT 'warning',
  -- firing / acked / resolved / suppressed
  status        VARCHAR(32)  NOT NULL DEFAULT 'firing',
  labels        JSON         NULL,
  -- 根因收敛：子事件指向父事件。父事件可来自 CMDB 的 K8s 事件
  -- （节点 NotReady 等），因此不依赖客户是否部署了指标监控。
  parent_id     BIGINT       NULL,
  correlation   VARCHAR(64)  NOT NULL DEFAULT '',
  count         INT          NOT NULL DEFAULT 1,
  first_at      DATETIME(3)  NOT NULL,
  last_at       DATETIME(3)  NOT NULL,
  resolved_at   DATETIME(3)  NULL,
  -- MTTA/MTTR 的原始数据：第一次通知送达、第一次被认领
  notified_at   DATETIME(3)  NULL,
  acked_at      DATETIME(3)  NULL,
  acked_by      VARCHAR(128) NOT NULL DEFAULT '',
  -- 人工反馈，回流到噪音治理与规则质量分
  false_positive TINYINT(1)  NOT NULL DEFAULT 0,
  sample        JSON         NULL,
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_inc_active (tenant_id, active_key),
  KEY idx_inc_list (tenant_id, status, last_at),
  KEY idx_inc_rule (rule_id, first_at),
  KEY idx_inc_parent (parent_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='事件。重复命中按 fingerprint 收敛；认领与恢复留痕，MTTA/MTTR 从这里算';

CREATE TABLE IF NOT EXISTS incident_events (
  id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id   BIGINT       NOT NULL,
  incident_id BIGINT       NOT NULL,
  -- created / repeated / notified / notify_failed / escalated / acked
  -- / suppressed / resolved / commented / false_positive
  kind        VARCHAR(32)  NOT NULL,
  actor       VARCHAR(128) NOT NULL DEFAULT '',
  message     VARCHAR(1024) NOT NULL DEFAULT '',
  detail      JSON         NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_ie_inc (incident_id, created_at),
  KEY idx_ie_tenant (tenant_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='事件时间线。复盘的时间线由它自动生成，人只补因果与行动项';

CREATE TABLE IF NOT EXISTS notifications (
  id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id   BIGINT       NOT NULL,
  incident_id BIGINT       NOT NULL,
  notifier_id BIGINT       NOT NULL,
  route_id    BIGINT       NULL,
  -- 第几级升级（0 = 首次通知）
  escalation_level INT     NOT NULL DEFAULT 0,
  -- pending / sent / failed / dropped
  status      VARCHAR(32)  NOT NULL DEFAULT 'pending',
  attempts    INT          NOT NULL DEFAULT 0,
  duration_ms INT          NOT NULL DEFAULT 0,
  error       VARCHAR(512) NOT NULL DEFAULT '',
  payload     JSON         NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  sent_at     DATETIME(3)  NULL,
  KEY idx_nf_inc (incident_id, created_at),
  KEY idx_nf_tenant_status (tenant_id, status, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='通知投递记录。渠道限流导致的漏告警必须看得见——否则值班以为「没人叫我」就是「没事」';

CREATE TABLE IF NOT EXISTS decision_traces (
  id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id   BIGINT       NOT NULL,
  rule_id     BIGINT       NULL,
  incident_id BIGINT       NULL,
  fingerprint CHAR(40)     NOT NULL DEFAULT '',
  -- 判定终局：fired / not_fired / suppressed / silenced / no_route / delivery_failed
  verdict     VARCHAR(32)  NOT NULL,
  -- 八步判定链的完整快照：
  -- [{"step":"query","ok":true,"detail":{...}}, {"step":"threshold",...}, ...]
  -- 存的是「当时」的判据与配置，不是现在的——事后改了阈值也不影响回溯。
  steps       JSON         NOT NULL,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  KEY idx_dt_inc (incident_id),
  KEY idx_dt_rule_time (rule_id, created_at),
  KEY idx_dt_tenant_time (tenant_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='判定链。正向回答「凭什么触发」，反向回答「我以为该响的怎么没响」——后者是排查漏告警的唯一有效手段，目前无人提供。保留 7 天，未触发的只留最近一次';

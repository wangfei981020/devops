-- 降噪：静默、抑制、关联、回放。

CREATE TABLE IF NOT EXISTS silences (
  id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id   BIGINT       NOT NULL,
  -- silence（一次性）/ maintenance（维护窗口）/ recurring（周期静音时段）
  kind        VARCHAR(32)  NOT NULL DEFAULT 'silence',
  matchers    JSON         NOT NULL,
  comment     VARCHAR(512) NOT NULL DEFAULT '',
  starts_at   DATETIME(3)  NOT NULL,
  -- ⚠️ 非空是刻意的：静默必须会过期。
  -- 永久静默 = 假装没问题，而且没人会回来把它关掉。
  -- 周期性静音用 recurrence 表达，仍然要有一个总的结束时间。
  ends_at     DATETIME(3)  NOT NULL,
  -- 周期规则：{"weekdays":[6],"start":"02:00","end":"04:00","tz":"Asia/Shanghai"}
  recurrence  JSON         NULL,
  -- 续期次数。反复续期会被标为疑似滥用——开源里静默一旦建好就没人再看。
  renew_count INT          NOT NULL DEFAULT 0,
  created_by  VARCHAR(128) NOT NULL DEFAULT '',
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  deleted_at  DATETIME(3)  NULL,
  KEY idx_sil_active (tenant_id, starts_at, ends_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='静默与维护窗口。抑制的是通知不是检测：窗口内事件照样记录，窗口结束仍未恢复会补发';

CREATE TABLE IF NOT EXISTS inhibitions (
  id           BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id    BIGINT       NOT NULL,
  name         VARCHAR(128) NOT NULL,
  -- 父事件匹配条件（存在时抑制子告警）
  source_matchers JSON      NOT NULL,
  target_matchers JSON      NOT NULL,
  -- 父子必须相等的标签，例 ["node"]：只抑制同一节点上的子告警
  equal_labels JSON         NULL,
  enabled      TINYINT(1)   NOT NULL DEFAULT 1,
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  deleted_at   DATETIME(3)  NULL,
  UNIQUE KEY uk_inh_name (tenant_id, name, deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='抑制规则。被抑制的告警仍记录可查，只是不通知——不是丢弃';

CREATE TABLE IF NOT EXISTS correlations (
  id         BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id  BIGINT       NOT NULL,
  name       VARCHAR(128) NOT NULL,
  -- topology（读 CMDB 依赖图）/ change（同发布批次）/ expression（标签表达式）
  -- / time_window（同标签 N 分钟内）/ text_cluster（日志模板指纹）
  strategy   VARCHAR(32)  NOT NULL,
  spec       JSON         NOT NULL,
  window_sec INT          NOT NULL DEFAULT 180,
  enabled    TINYINT(1)   NOT NULL DEFAULT 1,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3)  NULL,
  UNIQUE KEY uk_cor_name (tenant_id, name, deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='关联规则。topology 策略直接读 CMDB 的依赖图与 K8s 事件，不需要人先把父子关系维护一遍';

CREATE TABLE IF NOT EXISTS backtests (
  id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id   BIGINT       NOT NULL,
  rule_id     BIGINT       NULL,
  -- 草稿配置（可能还没落成规则）
  draft       JSON         NOT NULL,
  range_from  DATETIME(3)  NOT NULL,
  range_to    DATETIME(3)  NOT NULL,
  -- running / done / failed
  status      VARCHAR(32)  NOT NULL DEFAULT 'running',
  -- 结果：触发次数、按天分布、分组分布、与现网配置的对比、深夜叫醒次数
  result      JSON         NULL,
  error       VARCHAR(512) NOT NULL DEFAULT '',
  created_by  VARCHAR(128) NOT NULL DEFAULT '',
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  finished_at DATETIME(3)  NULL,
  KEY idx_bt_tenant_time (tenant_id, created_at),
  KEY idx_bt_rule (rule_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='规则回放。沙箱执行：不发通知、不产生事件、不影响现网规则——这一点必须在代码里强制，不能靠调用方自觉';

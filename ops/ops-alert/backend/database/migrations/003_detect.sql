-- 检测：规则与执行历史。
--
-- 公共字段在列上，场景特有配置在 spec JSON 里。
-- 上一代把四种玩法塞进一张 48 列的表，规则表单 11 个分区 1189 行，
-- 新客户打开看到的全是别人环境的字段。

CREATE TABLE IF NOT EXISTS rules (
  id            BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id     BIGINT       NOT NULL,
  name          VARCHAR(200) NOT NULL,
  description   VARCHAR(512) NOT NULL DEFAULT '',
  -- 一期四类：log_keyword / log_absent / log_spike / log_field_threshold
  -- 二期：metric_threshold / slo_burn / anomaly / sql_query / external_inbound
  kind          VARCHAR(32)  NOT NULL,
  datasource_id BIGINT       NOT NULL,
  -- 场景特有配置。例：日志关键词 {"query":"...","filters":[...],"extract":[...]}
  spec          JSON         NOT NULL,
  -- 调度与窗口
  interval_sec  INT          NOT NULL DEFAULT 60,
  lookback_sec  INT          NOT NULL DEFAULT 300,
  -- 判定：命中数阈值 + 连续满足周期数（for）
  threshold     INT          NOT NULL DEFAULT 1,
  for_periods   INT          NOT NULL DEFAULT 1,
  -- 分组维度，例 ["container"]。每个分组各自判定、各自成事件。
  group_by      JSON         NULL,
  severity      VARCHAR(16)  NOT NULL DEFAULT 'warning',
  labels        JSON         NULL,
  -- 事件指纹的组成字段，默认 rule + group_by 各值
  fingerprint_by JSON        NULL,
  -- 恢复判定：连续 N 个周期无命中才算恢复
  resolve_after  INT         NOT NULL DEFAULT 3,
  notify_resolved TINYINT(1) NOT NULL DEFAULT 1,
  -- 查询返回空且数据源不可达时，是否当作异常（默认否：数据源故障走自检通道，
  -- 混进业务告警会让值班分不清是业务坏了还是监控坏了）
  nodata_is_alert TINYINT(1) NOT NULL DEFAULT 0,
  route_id      BIGINT       NULL,
  -- 单次执行最多产生的事件数，超出折叠为汇总，防止刷屏
  max_events    INT          NOT NULL DEFAULT 20,
  enabled       TINYINT(1)   NOT NULL DEFAULT 1,
  -- 运行状态（供规则健康与质量分）
  last_run_at   DATETIME(3)  NULL,
  last_error    VARCHAR(512) NOT NULL DEFAULT '',
  consecutive_failures INT   NOT NULL DEFAULT 0,
  created_by    VARCHAR(128) NOT NULL DEFAULT '',
  created_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at    DATETIME(3)  NULL,
  UNIQUE KEY uk_rule_name (tenant_id, name, deleted_at),
  KEY idx_rule_sched (tenant_id, enabled, deleted_at),
  KEY idx_rule_ds (datasource_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='检测规则。last_error 与 consecutive_failures 是「规则健康」的数据来源：7 天触发 0 次可能是健康，也可能是查询一直在失败，两者必须能区分';

CREATE TABLE IF NOT EXISTS rule_runs (
  id           BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id    BIGINT       NOT NULL,
  rule_id      BIGINT       NOT NULL,
  started_at   DATETIME(3)  NOT NULL,
  duration_ms  INT          NOT NULL DEFAULT 0,
  -- ok / no_data / error
  outcome      VARCHAR(32)  NOT NULL,
  hits         INT          NOT NULL DEFAULT 0,
  -- 列名不用 groups：它是 MySQL 8 的保留字（窗口函数的帧单位），
  -- 不加反引号会直接语法错误。宁可改名也不用反引号——
  -- 反引号只是绕过去，之后每次写查询都得记得带。
  group_count  INT          NOT NULL DEFAULT 0,
  events_made  INT          NOT NULL DEFAULT 0,
  error        VARCHAR(512) NOT NULL DEFAULT '',
  KEY idx_run_rule_time (rule_id, started_at),
  KEY idx_run_tenant_time (tenant_id, started_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='规则执行历史。保留 30 天，由清理任务按 tenant 分批删——是质量分、噪音榜与自检的原始数据';

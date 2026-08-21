-- 接入与投递：数据源、通知渠道、联系人组、路由树。
-- 全部带 tenant_id（不在平台白名单里），跨租户查询会被 store 层拒绝。

CREATE TABLE IF NOT EXISTS datasources (
  id           BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id    BIGINT       NOT NULL,
  name         VARCHAR(128) NOT NULL,
  -- 一期：loki / elasticsearch / opensearch
  -- 二期：prometheus / victoriametrics / clickhouse / mysql（适配器接口已留，见 datasource/adapter.go）
  type         VARCHAR(32)  NOT NULL,
  endpoint     VARCHAR(512) NOT NULL,
  -- 认证信息整体加密后存这里（AES-GCM）。绝不回显明文：
  -- 上一代系统的两个 P0 都是接口把凭据发给了不该看的人。
  auth_enc     BLOB         NULL,
  -- 各类型特有配置：ES 的 version/index_pattern、Loki 的 org_id 等
  spec         JSON         NULL,
  skip_tls     TINYINT(1)   NOT NULL DEFAULT 0,
  status       VARCHAR(32)  NOT NULL DEFAULT 'unknown',
  -- 探测结果落库，是为了让「不可达」在界面上是显性状态而不是空白。
  -- 数据源挂了但规则还在跑 = 静默失明，这是本产品要根治的第一类问题。
  probe_at     DATETIME(3)  NULL,
  probe_ms     INT          NOT NULL DEFAULT 0,
  probe_error  VARCHAR(512) NOT NULL DEFAULT '',
  -- 最近一次成功返回数据的时间。用于「静默故障检测」：
  -- 连得上不代表有数据，日志断流同样是失明。
  last_data_at DATETIME(3)  NULL,
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at   DATETIME(3)  NULL,
  UNIQUE KEY uk_ds_name (tenant_id, name, deleted_at),
  KEY idx_ds_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='数据源。一期只实现日志三类；类型是字符串而非枚举，加类型不用改表';

CREATE TABLE IF NOT EXISTS notifiers (
  id         BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id  BIGINT       NOT NULL,
  name       VARCHAR(128) NOT NULL,
  -- feishu / wecom / dingtalk / slack / teams / email / sms / voice / webhook
  type       VARCHAR(32)  NOT NULL,
  config_enc BLOB         NULL,
  -- 渠道能力（卡片/@人/按钮/字数上限）由代码侧声明，不存库：
  -- 能力是代码的属性，存库会与实现漂移，且升级后老数据会说谎。
  status     VARCHAR(32)  NOT NULL DEFAULT 'active',
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3)  NULL,
  UNIQUE KEY uk_nt_name (tenant_id, name, deleted_at),
  KEY idx_nt_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='通知渠道';

CREATE TABLE IF NOT EXISTS contact_groups (
  id         BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id  BIGINT       NOT NULL,
  name       VARCHAR(128) NOT NULL,
  created_at DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  deleted_at DATETIME(3)  NULL,
  UNIQUE KEY uk_cg_name (tenant_id, name, deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='联系人组。一期不做值班排班，升级链直接指向联系人组——排班日历是后续再定的模块，届时升级链改为指向「当班人」即可，本表不必推倒';

CREATE TABLE IF NOT EXISTS contact_group_members (
  group_id   BIGINT      NOT NULL,
  tenant_id  BIGINT      NOT NULL,
  user_id    BIGINT      NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (group_id, user_id),
  KEY idx_cgm_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='联系人组成员';

CREATE TABLE IF NOT EXISTS routes (
  id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id   BIGINT       NOT NULL,
  parent_id   BIGINT       NULL,
  name        VARCHAR(128) NOT NULL,
  -- 标签匹配条件：[{"key":"severity","op":"=","value":"critical"}]
  -- 空数组 = 无条件匹配（兜底路由）
  matchers    JSON         NOT NULL,
  -- 命中后是否继续匹配后续兄弟分支（Alertmanager 的 continue 语义）
  continue_on TINYINT(1)   NOT NULL DEFAULT 0,
  notifier_ids JSON        NOT NULL,
  -- 分组与重复：group_wait 秒、repeat_interval 秒
  group_by    JSON         NULL,
  group_wait  INT          NOT NULL DEFAULT 30,
  repeat_sec  INT          NOT NULL DEFAULT 14400,
  -- 升级链：[{"after_sec":900,"group_id":3,"notifier_ids":[2]}]
  -- 一期指向联系人组；未认领即逐级升级，任一级投递失败立刻跳下一级
  -- （投递失败 ≠ 已通知，这是最危险的一类静默失败）。
  escalation  JSON         NULL,
  -- 兜底路由不可删除：没有兜底时，未匹配任何条件的事件会静默消失。
  is_fallback TINYINT(1)   NOT NULL DEFAULT 0,
  sort_order  INT          NOT NULL DEFAULT 0,
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at  DATETIME(3)  NULL,
  KEY idx_route_tenant (tenant_id, parent_id, sort_order)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='通知路由树。路由是独立对象、可被多条规则共用——上一代把它塞在每条规则的 JSON 字段里，同一份路由被抄了几十遍，改一次要改几十处';

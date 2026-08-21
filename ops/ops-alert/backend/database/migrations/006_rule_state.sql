-- 规则 × 分组 的连续计数状态。
--
-- 为什么不放内存：`for=2 个周期` 与 `恢复需连续 3 个周期无命中` 都是跨周期的状态。
-- 放内存的话，Pod 重启或多副本接管后计数从零开始——表现为「重启后告警延迟一个周期」
-- 和「恢复通知迟迟不发」，而且只在重启后偶发，是最难查的一类问题。
--
-- 也不放 Redis：一期不引入额外中间件；这张表的写入量 = 规则数 × 分组数 / 周期，
-- 量级远低于事件表，MySQL 完全扛得住。

CREATE TABLE IF NOT EXISTS rule_states (
  tenant_id     BIGINT       NOT NULL,
  rule_id       BIGINT       NOT NULL,
  -- 分组键：group_by 各值用 \x1f 连接；无分组时为空串（不是 NULL，
  -- 免得主键里出现 NULL 导致同一规则插出多行）
  group_key     VARCHAR(512) NOT NULL,
  hit_streak    INT          NOT NULL DEFAULT 0,
  miss_streak   INT          NOT NULL DEFAULT 0,
  last_hit_at   DATETIME(3)  NULL,
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  PRIMARY KEY (rule_id, group_key),
  KEY idx_rs_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='规则分组的连续命中/未命中计数。规则删除或改分组维度时要一并清理，否则老 group_key 会永远留在这里';

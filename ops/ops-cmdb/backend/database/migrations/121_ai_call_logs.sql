-- AI 兜底（Layer 2）的调用记录。
--
-- 🔴 这张表的存在理由：用户明确要求「必须要通过 AI 的，那你就要给一个详细的说明，
--    并且记录了」。所以每一次调用都必须能回答三个问题：
--      1. 为什么前面几层（规则 / 证据提炼 / CMDB 历史）都没结论
--      2. 喂给模型的到底是什么（原文，不是摘要）
--      3. 花了多少钱
--    detail 列装的就是这三样（AICallRecord 的 JSON）。
--
-- ⚠️ detail 里含**业务日志**（已过 diag.Redact 脱敏，但脱敏是减少泄露面、
--    不是保证不泄露）。所以：
--      · 这张表按保留期清理，不永久留存
--      · 查询接口要走权限，不能像普通台账那样开放
--
-- ⚠️ 失败的调用也要记（err 非空）。只记成功的话，
--    「AI 花了钱但没给出结论」这件事就没有任何痕迹 —— 而那正是要盯的浪费。
CREATE TABLE IF NOT EXISTS ai_call_logs (
  id            BIGINT       NOT NULL AUTO_INCREMENT,
  at            DATETIME     NOT NULL,
  cluster_id    INT          NOT NULL DEFAULT 0,
  object        VARCHAR(255) NOT NULL DEFAULT '',
  model         VARCHAR(64)  NOT NULL DEFAULT '',
  input_tokens  INT          NOT NULL DEFAULT 0,
  output_tokens INT          NOT NULL DEFAULT 0,
  -- 成本用 DECIMAL 不用 FLOAT：这是钱，累加时的浮点误差会让对账对不上
  cost_usd      DECIMAL(12,6) NOT NULL DEFAULT 0,
  elapsed_ms    BIGINT       NOT NULL DEFAULT 0,
  -- gate_code 走到这一步的原因码，用于统计「AI 被挡下来的原因分布」
  gate_code     VARCHAR(32)  NOT NULL DEFAULT '',
  err           VARCHAR(512) NOT NULL DEFAULT '',
  detail        MEDIUMTEXT   NULL,
  PRIMARY KEY (id),
  KEY idx_at (at),
  KEY idx_cluster_object (cluster_id, object)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

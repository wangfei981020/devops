-- 审计追溯：操作流水扩列 + 对象级变更明细表
--
-- 目标：所有人工操作留痕，所有变更能看到"改之前是什么"，并据此回滚。
--
-- 为什么拆两张表：一次 HTTP 写请求可能改多行（批量改 100 条 DNS 记录就是 100 行），
-- 把 before/after 塞进流水表要么放不下，要么退化成一坨没法逐条回滚的 JSON。
-- 主表记"谁在什么时候做了什么"，子表记"具体改了哪张表哪一行、改前改后各是什么"。

-- ── 主表：一次请求一条 ────────────────────────────────────────────────
-- 沿用已有列 username / action / target / ip / at，避免改动现存 66 处 WriteAudit 调用。
ALTER TABLE audit_logs ADD COLUMN trace_id     VARCHAR(64)  NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN actor_source VARCHAR(16)  NOT NULL DEFAULT 'local' COMMENT 'local/portal/system/mcp';
ALTER TABLE audit_logs ADD COLUMN target_type  VARCHAR(48)  NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN target_id    VARCHAR(64)  NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN method       VARCHAR(8)   NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN path         VARCHAR(255) NOT NULL DEFAULT '';
-- 记下"凭哪个权限码放行的"，事后能反查某个权限是不是给宽了
ALTER TABLE audit_logs ADD COLUMN perm_code    VARCHAR(64)  NOT NULL DEFAULT '';
-- success / fail / denied。403 也要记——谁在试探什么，本身就是要看的东西
ALTER TABLE audit_logs ADD COLUMN status       VARCHAR(16)  NOT NULL DEFAULT 'success';
ALTER TABLE audit_logs ADD COLUMN error_msg    VARCHAR(512) NOT NULL DEFAULT '';
ALTER TABLE audit_logs ADD COLUMN change_count INT          NOT NULL DEFAULT 0;
ALTER TABLE audit_logs ADD COLUMN duration_ms  INT          NOT NULL DEFAULT 0;
-- 本条若是一次回滚操作，指向被它回滚的那条 audit_logs.id（回滚本身也是变更）
ALTER TABLE audit_logs ADD COLUMN revert_of    BIGINT       NULL;

ALTER TABLE audit_logs ADD INDEX idx_actor (username, at);
ALTER TABLE audit_logs ADD INDEX idx_action (action, at);
ALTER TABLE audit_logs ADD INDEX idx_target (target_type, target_id);
ALTER TABLE audit_logs ADD INDEX idx_status (status, at);

-- ── 子表：对象级变更，一次操作 N 条 ──────────────────────────────────
CREATE TABLE IF NOT EXISTS audit_changes (
  id          BIGINT AUTO_INCREMENT PRIMARY KEY,
  audit_id    BIGINT       NOT NULL,
  seq         INT          NOT NULL DEFAULT 0,
  table_name  VARCHAR(64)  NOT NULL,
  row_pk      VARCHAR(64)  NOT NULL DEFAULT '',
  op          VARCHAR(8)   NOT NULL COMMENT 'INSERT/UPDATE/DELETE',

  -- 整行 JSON 的 AES 密文。
  --
  -- 为什么加密而不是像 diff 那样脱敏：脱敏过的值回滚不了——把 token 写成
  -- "***changed***" 之后，就再也没法把旧 token 恢复回去。所以原文必须留着，
  -- 但它等同于各系统的凭据，明文落库风险太大。折中是加密存、只在服务端
  -- 回滚流程里解密，任何 API 都不返回这两列。
  before_enc  LONGTEXT     NULL,
  after_enc   LONGTEXT     NULL,

  -- 脱敏后的字段级 diff，给人看：{"ttl":{"old":300,"new":60}}
  -- 敏感字段只写 {"changed":true}，URL 脱敏到 host
  diff_json   JSON         NULL,

  -- local  = 纯本地库改动，可直接反向 UPDATE/INSERT
  -- external = 会写外部系统（Cloudflare DNS 等），回滚要二次确认
  -- none   = 不可回滚（证书签发、同步、token 重置…）
  revert_kind VARCHAR(16)  NOT NULL DEFAULT 'local',

  reverted_at     DATETIME NULL,
  reverted_by     VARCHAR(64) NOT NULL DEFAULT '',
  revert_audit_id BIGINT   NULL,

  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_audit (audit_id, seq),
  KEY idx_row (table_name, row_pk, id),
  KEY idx_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ── 保留策略：默认永久 ────────────────────────────────────────────────
-- 0 = 永久保留（默认）。设成正数才启用清理任务。
-- 审计表没有删除接口，管理员在 UI 上也删不掉，只有到期清理任务能删。
INSERT IGNORE INTO settings (k, v) VALUES ('audit_retention_days', '0');
INSERT IGNORE INTO settings (k, v) VALUES ('audit_changes_retention_days', '0');

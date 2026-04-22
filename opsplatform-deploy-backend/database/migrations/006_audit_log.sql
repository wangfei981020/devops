-- Migration 006: audit_log 表 —— 记录敏感管理操作，参考告警系统
-- Date: 2026-04-22

CREATE TABLE IF NOT EXISTS audit_log (
  id BIGINT AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(100) NOT NULL COMMENT '操作人 JWT username',
  auth_source VARCHAR(20) NOT NULL DEFAULT 'local' COMMENT 'local/portal',
  action VARCHAR(64) NOT NULL COMMENT '操作类型如 user.create / project_env.update / lark_bot.test',
  target_type VARCHAR(64) NOT NULL DEFAULT '' COMMENT '目标类型',
  target_name VARCHAR(200) NOT NULL DEFAULT '' COMMENT '目标名称或ID',
  detail TEXT COMMENT '操作详情 JSON',
  ip VARCHAR(64) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_user (username),
  INDEX idx_action (action),
  INDEX idx_created (created_at DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- deployment 表经常按 project_env + 时间查询，加复合索引（审计里顺带做）
CREATE INDEX idx_dep_env_time ON deployment(project_env_id, created_at);
CREATE INDEX idx_dep_status_time ON deployment(status, created_at);
CREATE INDEX idx_dep_operator_time ON deployment(operator, created_at);

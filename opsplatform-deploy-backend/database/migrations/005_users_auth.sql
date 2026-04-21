-- Migration 005: users + sessions 表（SSO + 本地登录）
-- Date: 2026-04-21

-- 平台用户表（跟告警系统保持一致）
CREATE TABLE IF NOT EXISTS users (
  id INT AUTO_INCREMENT PRIMARY KEY,
  username VARCHAR(50) NOT NULL,
  password_hash VARCHAR(200) NOT NULL DEFAULT '',
  display_name VARCHAR(100) NOT NULL DEFAULT '',
  role VARCHAR(20) NOT NULL DEFAULT 'user' COMMENT 'admin/user',
  auth_source VARCHAR(20) NOT NULL DEFAULT 'local' COMMENT 'local/portal',
  portal_token TEXT COMMENT '运维平台 portal token（用于刷新权限）',
  status TINYINT NOT NULL DEFAULT 1 COMMENT '1=启用 0=禁用',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_username_source (username, auth_source)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 会话表
CREATE TABLE IF NOT EXISTS sessions (
  id INT AUTO_INCREMENT PRIMARY KEY,
  user_id INT NOT NULL,
  token_hash VARCHAR(64) NOT NULL UNIQUE,
  expires_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_token (token_hash),
  INDEX idx_expires (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 默认管理员 admin / admin123（bcrypt hash 取自告警系统）
INSERT IGNORE INTO users (username, password_hash, display_name, role, auth_source)
VALUES ('admin', '$2a$10$vXhq5Vju4qCuhXhbGNjvyOqrEkXxTkkzyOokD0jKV5d8bjMOpgNQ6', 'Admin', 'admin', 'local');

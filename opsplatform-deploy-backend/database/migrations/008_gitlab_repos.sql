-- Migration 008: gitlab_repo 表（复用仓库登记）+ project_env.gitlab_repo_id 关联字段
-- Date: 2026-04-22

CREATE TABLE IF NOT EXISTS gitlab_repo (
  id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  repo_url VARCHAR(500) NOT NULL COMMENT '完整仓库 URL，如 https://gitlab.xx/group/proj.git',
  default_branch VARCHAR(64) NOT NULL DEFAULT 'main',
  description VARCHAR(500) NOT NULL DEFAULT '',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

ALTER TABLE project_env ADD COLUMN gitlab_repo_id BIGINT NULL DEFAULT NULL
  COMMENT '引用 gitlab_repo.id，保存时 repo_url 会同步拷贝到 git_repo 字段（副本模式）';

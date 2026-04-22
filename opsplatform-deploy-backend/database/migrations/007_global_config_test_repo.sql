-- Migration 007: global_config 加 test_repo_path 字段
-- Date: 2026-04-22
-- 用户在「系统设置 → 全局凭证」里填一个有权限的仓库相对路径，
-- 点「测试连接」时后端用这个仓库做 git ls-remote 精确验证 token

ALTER TABLE global_config ADD COLUMN test_repo_path VARCHAR(500) NOT NULL DEFAULT ''
  COMMENT '测试连接用的仓库相对路径（如 argocd/uat-k8s-platform），可选';

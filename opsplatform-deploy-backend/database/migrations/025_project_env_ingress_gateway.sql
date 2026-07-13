-- 项目环境加 ingress 网关名（每个环境一个；新增模块部署到本环境时自动带出）
-- Date: 2026-07-13
ALTER TABLE project_env ADD COLUMN ingress_gateway VARCHAR(255) NOT NULL DEFAULT '';

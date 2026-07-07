-- 主机加 GCP 项目显示名（可在 GCP 重命名，同步时从 Resource Manager 取）；project 列存的是不可变 project id
ALTER TABLE hosts ADD COLUMN project_name VARCHAR(255) NOT NULL DEFAULT '';

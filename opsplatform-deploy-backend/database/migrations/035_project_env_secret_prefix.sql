-- Migration 035: project_env 加 secret_prefix（跨项目复用模板时，密钥名的项目前缀覆盖值）。
--   新增后端模块换密钥前缀时用：
--     留空 = 自动 = 项目名去环境后缀再转小写（G50-uat → g50、pa-re-uat → pa-re），正规项目都走这条；
--     填了 = 固定用这个前缀（历史特例用，如 g33 整体复用 g32，两个环境都填 g32，密钥保持 g32-*）。
--   几个月后 g33 迁移出来，把这格清空即可恢复自动。

ALTER TABLE project_env ADD COLUMN secret_prefix VARCHAR(64) NOT NULL DEFAULT '';

-- Migration 015: 接入 Harbor 镜像仓库做镜像版本下拉 + 提交前 tag 校验
--
-- 4 个字段加进 global_config（不引独立表）：
--   harbor_url                 https://harbor.slileisure.com
--   harbor_user                robot$public-pull (Robot 账号，含 $)
--   harbor_token               robot 的 password，AES 加密落库
--   harbor_verify_on_submit    提交前是否实时校验 tag 是否在 Harbor (默认 ON)
ALTER TABLE global_config ADD COLUMN harbor_url VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE global_config ADD COLUMN harbor_user VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE global_config ADD COLUMN harbor_token TEXT;
ALTER TABLE global_config ADD COLUMN harbor_verify_on_submit TINYINT NOT NULL DEFAULT 1;

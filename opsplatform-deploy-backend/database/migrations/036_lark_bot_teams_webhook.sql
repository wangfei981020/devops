-- Migration 036: Teams 双发。lark_bot 加 teams_webhook，页面上「Lark 机器人」改叫「通知渠道」。
--   一个渠道 = 飞书 + Teams，两边都可留空但至少填一个；项目环境 lark_bot_id 不变，选了渠道就两边都发。
--   teams_webhook 存 Teams「工作流」地址，带 sig = 凭据，AES 加密存（加密后约 1.4 倍长度）。
--   迁新发布系统前的临时双发用。

ALTER TABLE lark_bot ADD COLUMN teams_webhook VARCHAR(2048) NOT NULL DEFAULT '' AFTER secret;

-- 通知人加 Teams 账号（照运维平台桌台维护告警）：发 Teams 时按 Lark ID 反查通知人，@ 他的 Teams 账号；
--   没填就按「姓名小写@默认后缀」推导，默认后缀存 global_config.teams_mention_domain（空 = 不推导）。
ALTER TABLE contact ADD COLUMN teams_id VARCHAR(254) NOT NULL DEFAULT '' AFTER lark_id;
ALTER TABLE global_config ADD COLUMN teams_mention_domain VARCHAR(253) NOT NULL DEFAULT '';

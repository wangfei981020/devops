-- 单点登录改成页面可配（原先只认环境变量 PORTAL_API_URL）
--
-- 动因：改环境变量要改 Helm values 再滚动重启，生产上开一次 SSO 得走一遍发布流程；
-- 而且没配的时候用户在登录页只看到一句"未开启单点登录"，根本不知道去哪开。
-- 读取优先级：settings.portal_api_url > 环境变量 PORTAL_API_URL；
-- portal_sso_enabled='0' 可显式关掉（此时只能用本地账号登录）。
--
-- ⚠️ 这两行原本追加在 084 末尾，但 084 已经被执行过——runner 靠
-- schema_migrations 判重，已执行的文件再追加内容**不会重跑**。改迁移必须新开文件。
INSERT IGNORE INTO settings (k, v) VALUES ('portal_api_url', '');
INSERT IGNORE INTO settings (k, v) VALUES ('portal_sso_enabled', '1');

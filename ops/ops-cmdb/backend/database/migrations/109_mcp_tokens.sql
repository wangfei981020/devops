-- MCP 接入令牌：从"一个全局 token"改成"每个接入方一条，各自带角色"。
--
-- 原来的 mcp_config 是单行单 token，且进程内回调直接按 is_admin=true 放行 ——
-- 也就是说**拿到那一个 token 就等于拿到全部只读权限**，
-- 而系统里辛苦建的角色体系对 MCP 这条路完全不生效。
--
-- 这在自用时不明显（就我一个人），做成商业产品就是硬伤：
-- 客户给外包/给某个部门接一个 AI，就等于把全量资产、成本、日志一起给了。
--
-- 三个变化：
--   1. 一个接入方一条 token，可以单独吊销（现在吊销要换掉所有人的）
--   2. 每条绑一个角色，走**与人完全相同**的权限码校验
--   3. 记最后使用时间/IP，长期没用的能看出来并回收
CREATE TABLE IF NOT EXISTS mcp_tokens (
  id          INT          NOT NULL AUTO_INCREMENT,
  -- 给人看的名字，如「Claude Code - 运维组」。没有名字的令牌没人敢吊销
  name        VARCHAR(64)  NOT NULL,
  -- ⚠️ 只存哈希。明文只在创建那一刻返回一次。
  -- 存明文的话，一次数据库导出就等于把所有接入方的凭据一起泄了 ——
  -- 而这类令牌通常比人的密码活得久得多
  token_hash  CHAR(64)     NOT NULL,
  -- 前 8 位明文，用于在列表里认出"这是哪一条"（不足以还原令牌）
  token_hint  VARCHAR(16)  NOT NULL DEFAULT '',
  -- 绑定的角色，取值同 local_roles.code。
  -- ⚠️ 空 = 不受限。**不允许**通过接口设成空：给 AI 一个不受限身份，
  -- 等于绕开整套权限体系。留这个取值只是为了兼容升级前的老 token
  role_code   VARCHAR(64)  NOT NULL DEFAULT 'cmdb_viewer',
  enabled     TINYINT      NOT NULL DEFAULT 1,
  created_by  VARCHAR(255) NOT NULL DEFAULT '',
  created_at  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  -- 最后一次使用。用来回答"这条还有人在用吗"——
  -- 没有它，没人敢吊销任何一条，令牌只会越积越多
  last_used_at DATETIME    NULL DEFAULT NULL,
  last_used_ip VARCHAR(64) NOT NULL DEFAULT '',
  PRIMARY KEY (id),
  UNIQUE KEY uniq_token_hash (token_hash),
  KEY idx_enabled (enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='MCP 接入令牌，一接入方一条';

-- 把已有的全局 token 迁过来，不让现有接入断掉。
--
-- ⚠️ 角色给不受限（空），不是 cmdb_viewer：这条 token 现在正在被人用着，
-- 升级这一下突然把它降权，表现是"AI 昨天还能查成本，今天说没权限"，
-- 而没有任何地方提示发生过什么。降权必须是管理员看着界面做的决定。
INSERT INTO mcp_tokens (name, token_hash, token_hint, role_code, enabled, created_by)
SELECT '升级前的全局令牌', SHA2(token, 256), LEFT(token, 8), '', enabled, 'migration'
FROM mcp_config WHERE id=1 AND token <> ''
  AND NOT EXISTS (SELECT 1 FROM mcp_tokens WHERE token_hash = SHA2(mcp_config.token, 256));

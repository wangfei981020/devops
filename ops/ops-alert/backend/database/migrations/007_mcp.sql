-- MCP 接入令牌。
--
-- 一个接入方一条，而不是全局一把钥匙：全局令牌泄露后无法定位是谁泄的，
-- 也没法只吊销一个接入方。上一代就是一把全局令牌，拿到即全权限。

CREATE TABLE IF NOT EXISTS mcp_tokens (
  id           BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id    BIGINT       NOT NULL,
  name         VARCHAR(128) NOT NULL,
  -- 只存哈希。明文在创建时返回一次，之后任何接口都拿不到——
  -- 能被接口读出来的密钥等于没有密钥。
  token_hash   CHAR(64)     NOT NULL,
  -- 令牌自带角色，决定能调哪些工具。一期全部工具只读，
  -- 但角色位要先留着：等有了写工具，再改成"令牌绑角色"就来不及了。
  role_code    VARCHAR(32)  NOT NULL DEFAULT 'viewer',
  enabled      TINYINT(1)   NOT NULL DEFAULT 1,
  last_used_at DATETIME(3)  NULL,
  last_used_ip VARCHAR(64)  NOT NULL DEFAULT '',
  created_by   VARCHAR(128) NOT NULL DEFAULT '',
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  deleted_at   DATETIME(3)  NULL,
  UNIQUE KEY uk_mcp_hash (token_hash),
  KEY idx_mcp_tenant (tenant_id, deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='MCP 接入令牌。last_used_at 落库是为了能看出哪条令牌其实没人用——长期不用的令牌该吊销';

-- 表单填充凭据：给"只有账号密码登录页"的老系统用。
--
-- # 与浏览器扩展路线的区别
--
-- 扩展方案必须把下游密码送到浏览器里执行填充 —— 而浏览器是最不该放凭据的地方
-- （任何一个恶意扩展、一次 XSS、一台被控的机器都能拿走）。
-- 我们在网关侧完成，凭据**永不离开服务端**。
--
-- user_id = 0 表示整组共用一套凭据；非 0 表示某人专属。
-- 取的时候优先专属 —— 共用凭据在审计上说不清"到底是谁操作的"，
-- 只能作为过渡手段，界面上要明确标出这一点。
CREATE TABLE IF NOT EXISTS formfill_credentials (
  id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  tenant_id  BIGINT UNSIGNED NOT NULL,
  app_id     BIGINT UNSIGNED NOT NULL,
  user_id    BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '0 = 整组共用',
  username   VARCHAR(128) NOT NULL,
  secret_enc VARBINARY(1024) NOT NULL COMMENT 'AES-GCM；任何接口都不回显',
  note       VARCHAR(255) NOT NULL DEFAULT '',
  revoked_at DATETIME NULL,
  created_by BIGINT UNSIGNED NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (id),
  UNIQUE KEY uq_ff (tenant_id, app_id, user_id),
  KEY idx_ff_app (tenant_id, app_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

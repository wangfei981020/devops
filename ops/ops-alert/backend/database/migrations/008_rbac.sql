-- 接口级权限。
--
-- 在此之前只有两档：viewer 只读、其余可写（RequireWrite）。
-- 两档撑不住真实值班场景——值班员必须能静默和认领，但绝不该能改数据源；
-- 而"能改规则"和"能改通知渠道"是完全不同的授权，粗粒度下只能一起给。

CREATE TABLE IF NOT EXISTS roles (
  code         VARCHAR(32)  NOT NULL PRIMARY KEY,
  name         VARCHAR(64)  NOT NULL,
  description  VARCHAR(255) NOT NULL DEFAULT '',
  is_builtin   TINYINT(1)   NOT NULL DEFAULT 0,
  unrestricted TINYINT(1)   NOT NULL DEFAULT 0,
  created_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at   DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='角色。unrestricted=1 表示不受权限码约束（管理员）——它的权限不是靠往 role_permissions 里灌全量码实现的，否则新增一个权限码就得记得回头补管理员';

CREATE TABLE IF NOT EXISTS role_permissions (
  role_code  VARCHAR(32) NOT NULL,
  perm_code  VARCHAR(64) NOT NULL,
  created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  PRIMARY KEY (role_code, perm_code),
  KEY idx_rp_perm (perm_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='角色 → 权限码。权限码本身不建字典表：真相在代码的映射表里（internal/api/perm.go），再建一张表就有了两份真相，而它们一定会分叉';

-- ─────────────────────────────────────────────────────────────
-- 内置角色
--
-- ⚠️ 用 INSERT ... ON DUPLICATE KEY UPDATE 而不是 INSERT IGNORE：
--    改了内置角色的名字/描述后重跑迁移要能更新，而 IGNORE 会静默跳过，
--    表现为"改了代码但库里还是旧文案"。
-- ─────────────────────────────────────────────────────────────
INSERT INTO roles (code, name, description, is_builtin, unrestricted) VALUES
  ('admin',      '管理员',     '不受权限码约束，含数据源、通知渠道、路由与 MCP 令牌', 1, 1),
  ('rule_admin', '规则管理员', '维护检测规则与降噪配置，不能改数据源和通知出口',       1, 0),
  ('oncall',     '值班员',     '处理告警：认领、静默、标记误报，不改任何配置',         1, 0),
  ('viewer',     '只读',       '只能查看，不能做任何变更',                             1, 0)
ON DUPLICATE KEY UPDATE name = VALUES(name), description = VALUES(description),
  is_builtin = VALUES(is_builtin), unrestricted = VALUES(unrestricted);

-- ─────────────────────────────────────────────────────────────
-- 角色 → 权限码
--
-- admin 不在此列：它靠 unrestricted 生效。
--
-- ⚠️ 菜单权限（menu:）同时也是该模块的**读**权限。分成两个码试过，
--    结果是"菜单看得见、进去一片 403"，因为总有人只配了 menu 忘了配 read。
-- ─────────────────────────────────────────────────────────────
INSERT IGNORE INTO role_permissions (role_code, perm_code) VALUES
  -- 规则管理员：看全部（除 MCP 令牌），改规则、回放、降噪
  ('rule_admin', 'menu:alert_warroom'),
  ('rule_admin', 'menu:alert_incidents'),
  ('rule_admin', 'menu:alert_rules'),
  ('rule_admin', 'menu:alert_backtest'),
  ('rule_admin', 'menu:alert_silences'),
  ('rule_admin', 'menu:alert_noisetop'),
  ('rule_admin', 'menu:alert_datasources'),
  ('rule_admin', 'menu:alert_notifiers'),
  ('rule_admin', 'menu:alert_routes'),
  ('rule_admin', 'menu:alert_selfcheck'),
  ('rule_admin', 'menu:alert_audit'),
  ('rule_admin', 'alert:ack_incident'),
  ('rule_admin', 'alert:manage_rules'),
  ('rule_admin', 'alert:run_backtest'),
  ('rule_admin', 'alert:manage_silences'),
  ('rule_admin', 'alert:import'),

  -- 值班员：看全部（除 MCP 令牌），只能认领和静默。
  -- 静默必须给：半夜被同一条告警刷屏却没权限压下去，人只会去关通知，
  -- 那比给静默权限危险得多。
  ('oncall', 'menu:alert_warroom'),
  ('oncall', 'menu:alert_incidents'),
  ('oncall', 'menu:alert_rules'),
  ('oncall', 'menu:alert_backtest'),
  ('oncall', 'menu:alert_silences'),
  ('oncall', 'menu:alert_noisetop'),
  ('oncall', 'menu:alert_datasources'),
  ('oncall', 'menu:alert_notifiers'),
  ('oncall', 'menu:alert_routes'),
  ('oncall', 'menu:alert_selfcheck'),
  ('oncall', 'menu:alert_audit'),
  ('oncall', 'alert:ack_incident'),
  ('oncall', 'alert:manage_silences'),

  -- 只读：审计与 MCP 令牌不给（前者含他人操作轨迹，后者能看到接入方清单）
  ('viewer', 'menu:alert_warroom'),
  ('viewer', 'menu:alert_incidents'),
  ('viewer', 'menu:alert_rules'),
  ('viewer', 'menu:alert_backtest'),
  ('viewer', 'menu:alert_silences'),
  ('viewer', 'menu:alert_noisetop'),
  ('viewer', 'menu:alert_datasources'),
  ('viewer', 'menu:alert_notifiers'),
  ('viewer', 'menu:alert_routes'),
  ('viewer', 'menu:alert_selfcheck');

-- 存量账号：role_code 为空的一律落到 viewer。
--
-- ⚠️ 方向只能是"收紧"。反过来把空角色当管理员，迁移一跑，
--    所有老账号瞬间变超级用户，而界面上看不出任何异常
--    （users 表的注释里已经写着这条，这里是它的执行）。
UPDATE users SET role_code = 'viewer' WHERE role_code = '' AND deleted_at IS NULL;

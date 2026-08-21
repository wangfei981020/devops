-- 告警文案模板库。
--
-- # 为什么需要它
--
-- 告警文案原来写死在代码里（buildMessage），部署方想改措辞、加字段、
-- 换成自己团队的叫法，只能改代码重新构建 —— 而这套系统是要交付给
-- 多个子公司自建部署的，他们没有这条路。
--
-- # 为什么沿用 {{var}} 简单替换而不是完整的 Go template
--
-- 规则自己的 message_title / message_template 已经在用 `{{var}}`（见 renderTemplate）。
-- 换一套语法会出现两种并存，而写错的表现是**变量原样出现在告警里**——
-- 值班的人看到 "服务 {{.service}} 异常" 只会以为系统坏了。
--
-- 代价是没有条件与循环。实测下来模板要解决的是「换措辞、加字段、
-- 调顺序」，那些用变量替换就够；真需要分支的场景（按级别换措辞）
-- 更适合做成两个模板挂在不同路由上。

CREATE TABLE IF NOT EXISTS message_templates (
  id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id   BIGINT       NOT NULL,
  name        VARCHAR(128) NOT NULL,
  description VARCHAR(512) NOT NULL DEFAULT '',

  -- 适用的渠道类型。空串 = 通用（任何渠道都能用）。
  --
  -- ⚠️ 分渠道是必须的：飞书是卡片、webhook 是任意 JSON，
  -- 结构差异大到一个模板管不了两边。以后加 Teams（Adaptive Card）更明显。
  channel_type VARCHAR(32) NOT NULL DEFAULT '',

  title_tmpl  VARCHAR(512) NOT NULL DEFAULT '',
  body_tmpl   TEXT         NOT NULL,

  -- 内置模板不可删、不可改（要改就复制一份）。
  --
  -- ⚠️ 允许改内置模板的话，「渲染失败回落到内置模板」这条兜底
  -- 就可能回落到一个同样坏掉的模板上 —— 兜底必须是永远可用的。
  is_builtin  TINYINT(1)   NOT NULL DEFAULT 0,

  created_by  VARCHAR(128) NOT NULL DEFAULT '',
  created_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at  DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  deleted_at  DATETIME(3)  NULL,
  alive       TINYINT(1)   GENERATED ALWAYS AS (IF(deleted_at IS NULL,1,NULL)) STORED,
  -- 软删唯一：MySQL 的 NULL 不参与唯一性比较，直接对 (tenant_id,name) 建唯一索引
  -- 会让"删了再建同名"失败。用生成列的写法见 009 迁移
  UNIQUE KEY uk_tpl_name (tenant_id, name, alive),
  KEY idx_tpl_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='告警文案模板。内置模板是渲染失败时的兜底，不可改不可删';

-- 通知渠道绑定模板。NULL = 用内置默认模板。
ALTER TABLE notifiers
  ADD COLUMN template_id BIGINT NULL COMMENT '文案模板；NULL=用内置默认';

-- ── 内置模板 ────────────────────────────────────────────────
--
-- 每个租户各种一份：模板是租户级资源，共享一份的话一个租户改了会波及别人。
-- 这里只给已存在的租户种；新建租户时由代码补种。
INSERT INTO message_templates
  (tenant_id, name, description, channel_type, title_tmpl, body_tmpl, is_builtin, created_by)
SELECT t.id, '内置 · 通用告警',
       '默认文案。渲染失败时也会回落到它，所以它不可改不可删。',
       '',
       '{{rule}}',
       '级别：{{severity}}\n对象：{{group}}\n命中：{{count}} 条\n首次：{{first_at}}\n\n{{sample}}',
       1, 'system'
FROM tenants t
WHERE NOT EXISTS (
  SELECT 1 FROM message_templates m
  WHERE m.tenant_id = t.id AND m.name = '内置 · 通用告警' AND m.deleted_at IS NULL
);

INSERT INTO message_templates
  (tenant_id, name, description, channel_type, title_tmpl, body_tmpl, is_builtin, created_by)
SELECT t.id, '内置 · 恢复通知',
       '事件恢复时用。与告警文案分开：恢复消息里写"命中 N 条"会让人以为又出问题了。',
       '',
       '已恢复：{{rule}}',
       '对象：{{group}}\n状态：连续多个周期未再命中，已自动恢复\n持续：{{duration}}',
       1, 'system'
FROM tenants t
WHERE NOT EXISTS (
  SELECT 1 FROM message_templates m
  WHERE m.tenant_id = t.id AND m.name = '内置 · 恢复通知' AND m.deleted_at IS NULL
);

-- 模板库的菜单权限。
--
-- ⚠️ 只读角色要能**看**模板：值班的人收到一条看不懂的告警时，
-- 第一件事是去看这条文案是怎么拼出来的。看不了的话只能问人。
INSERT IGNORE INTO role_permissions (role_code, perm_code) VALUES
  ('rule_admin', 'menu:alert_templates_msg'),
  ('rule_admin', 'alert:manage_msg_templates'),
  ('oncall',     'menu:alert_templates_msg'),
  ('viewer',     'menu:alert_templates_msg');

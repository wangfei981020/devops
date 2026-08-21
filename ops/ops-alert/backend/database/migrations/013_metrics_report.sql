-- 指标导出 + 日报。补的是从旧系统迁过来时会丢掉的两项能力：
-- 生产 7 条规则里 5 条开着 prometheus_config、1 条开着 report_enabled。

-- ── 规则上的指标导出开关 ──────────────────────────────────────
-- 对应旧系统的 prometheus_config = {"enabled":true,"static_labels":{...}}
ALTER TABLE rules
  ADD COLUMN metrics_enabled TINYINT(1) NOT NULL DEFAULT 0
    COMMENT '是否把这条规则的执行结果导出为 Prometheus 指标',
  ADD COLUMN metrics_labels JSON NULL
    COMMENT '附加到该规则所有指标上的静态标签，例 {"project":"G32","env":"PROD"}';

-- ── 指标累计值 ────────────────────────────────────────────────
--
-- 🔴 counter 类指标**不能从 rule_runs 算**。
--
-- rule_runs 只保留 30 天，清理任务一删，SUM(hits) 就会往回跳。
-- Prometheus 把 counter 回退解释成"进程重启、计数器归零"，
-- 于是 rate() 在那一刻产生一个凭空的巨大尖峰 —— 依赖它的告警会误报，
-- 而看板上那根尖峰看起来就像真的出了事故。
--
-- 所以另立一张只增不减的累计表：清理任务不碰它，
-- 它唯一的归零时机是规则被删除（那时指标本来也该消失）。
CREATE TABLE IF NOT EXISTS rule_metrics (
  tenant_id     BIGINT      NOT NULL,
  rule_id       BIGINT      NOT NULL,
  hits_total    BIGINT      NOT NULL DEFAULT 0 COMMENT '累计命中条数',
  runs_ok       BIGINT      NOT NULL DEFAULT 0 COMMENT '累计成功执行次数',
  runs_nodata   BIGINT      NOT NULL DEFAULT 0 COMMENT '累计查询无数据次数',
  runs_error    BIGINT      NOT NULL DEFAULT 0 COMMENT '累计执行失败次数',
  events_total  BIGINT      NOT NULL DEFAULT 0 COMMENT '累计产生事件数',
  last_run_at   DATETIME(3) NULL COMMENT '上次执行时刻，导出为 gauge',
  PRIMARY KEY (tenant_id, rule_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='规则指标累计值。只增不减，不参与 30 天清理——见表内注释';

-- ── 日报 ──────────────────────────────────────────────────────
--
-- 旧系统的 report_enabled 是**每条规则一个开关**，发出来是按 domain
-- 硬编码的模板。这里改成租户级的一份日报：值班的人要的是
-- "昨天整体怎么样"，而不是收 20 封各自只讲一条规则的邮件。
CREATE TABLE IF NOT EXISTS report_configs (
  id            BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
  tenant_id     BIGINT       NOT NULL,
  enabled       TINYINT(1)   NOT NULL DEFAULT 0,
  -- 本地时间的发送时刻，例 "09:00"。存字符串而不是 TIME：
  -- 它是"每天几点"的意图，不是某个具体时刻
  send_at       VARCHAR(5)   NOT NULL DEFAULT '09:00',
  -- 投递到哪些通知渠道。空 = 没配 = 不发（并在自检里报出来，
  -- 否则"开了日报却从没收到"要查很久）
  notifier_ids  JSON         NULL,
  last_sent_on  DATE         NULL COMMENT '上次成功发送的日期，用于当天去重',
  last_error    VARCHAR(512) NOT NULL DEFAULT '',
  updated_at    DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uk_report_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='日报配置。一个租户一份';

-- ── 日报菜单的权限种子 ────────────────────────────────────────
--
-- ⚠️ 新菜单必须补种子，否则**除 admin 外所有角色进去都是 403**
-- （admin 靠 unrestricted 生效，所以自测时发现不了）。
-- 这正是 fail-closed 的代价：漏配不会报错，只会让别人打不开页面。
INSERT IGNORE INTO role_permissions (role_code, perm_code) VALUES
  ('rule_admin', 'menu:alert_report'),
  ('rule_admin', 'alert:manage_report'),
  -- 值班员能看日报内容（它就是给值班的人看的），但不改配置
  ('oncall',     'menu:alert_report'),
  ('viewer',     'menu:alert_report');

-- 日志检索：所有角色都要能用。
-- "为什么没告警"是只读角色最需要自己回答的问题，
-- 把它锁给管理员，等于每次都要找人帮忙查一遍。
INSERT IGNORE INTO role_permissions (role_code, perm_code) VALUES
  ('rule_admin', 'menu:alert_explore'),
  ('oncall',     'menu:alert_explore'),
  ('viewer',     'menu:alert_explore');

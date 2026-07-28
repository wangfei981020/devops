-- CDN 规则（Cloudflare Page Rules + Rulesets）
--
-- 补的是「CDN 上到底配了什么规则」这个盲区：缓存策略、强制 HTTPS、WAF 自定义规则
-- 此前一概查不到，排查「为什么这个路径没走缓存」「为什么跳了 HTTPS」只能登控制台。
--
-- 两种规则体系并存是 Cloudflare 的历史包袱：
--   Page Rules —— 老体系，按 URL 匹配，有套餐数量上限（Free 3 / Pro 20 / Business 50）
--   Rulesets   —— 新体系，按 phase 分类（缓存/转换/WAF 等），表达式语法
-- 两者可能同时生效且互相覆盖，所以要一起采、一起看。

CREATE TABLE IF NOT EXISTS cdn_rules (
  id          BIGINT AUTO_INCREMENT PRIMARY KEY,
  account_id  INT          NOT NULL,
  zone_id     VARCHAR(64)  NOT NULL,
  zone_name   VARCHAR(255) NOT NULL DEFAULT '',
  source      VARCHAR(16)  NOT NULL COMMENT 'pagerule | ruleset',
  rule_id     VARCHAR(64)  NOT NULL DEFAULT '',
  name        VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'ruleset 名或 pagerule 的匹配目标',
  phase       VARCHAR(64)  NOT NULL DEFAULT '' COMMENT 'ruleset 专有：http_request_cache_settings 等',
  kind        VARCHAR(32)  NOT NULL DEFAULT '' COMMENT 'ruleset 专有：zone/managed/custom',
  priority    INT          NOT NULL DEFAULT 0 COMMENT 'pagerule 专有：数字越小越先匹配',
  status      VARCHAR(16)  NOT NULL DEFAULT '' COMMENT 'active/disabled',
  expression  TEXT         COMMENT 'ruleset 规则表达式 / pagerule 的 URL 匹配式',
  actions     TEXT         COMMENT '动作摘要，逗号分隔',
  last_updated VARCHAR(40) NOT NULL DEFAULT '',
  synced_at   DATETIME     NOT NULL,
  KEY idx_cdnrule_zone (account_id, zone_id),
  KEY idx_cdnrule_src (source)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='CDN 规则（Cloudflare Page Rules / Rulesets）';

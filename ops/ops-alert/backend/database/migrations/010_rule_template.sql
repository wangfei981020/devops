-- 记住规则是从哪个模板建的，以及当时填了什么。
--
-- # 为什么要存
--
-- 不存的话，模板就退化成一次性的"生成器"：生成完就断了关系。
-- 后果有两个，都是运维期才暴露：
--
--   1. 改不回去 —— 想给某条规则再加一个"不告警的错误码"，
--      只能人工看懂生成出来的 LogQL 再手改，而那正是模板要替人做的事。
--   2. 模板改了，已建的规则不知道 —— 比如以后修正了错误码模板的提取正则，
--      没有这层记录就无从知道哪些规则受影响。
--
-- params 存的是**用户填的业务参数**（命名空间、要忽略的错误码…），
-- 不是生成结果。生成结果在 spec 里，两者的关系是 params --模板--> spec。

ALTER TABLE rules
  ADD COLUMN template VARCHAR(64) NOT NULL DEFAULT ''
    COMMENT '来源模板 key，空串表示手写规则（不是所有规则都来自模板）',
  ADD COLUMN template_params JSON NULL
    COMMENT '当时填的模板参数。改规则时用它回填表单，而不是让人去反推生成好的 LogQL';

-- 按模板统计/筛选：模板页要显示「用这个模板建了几条」
CREATE INDEX idx_rules_template ON rules (tenant_id, template);

-- OPSCMDB-046：区分「成本是 0」与「不知道成本是多少」。
--
-- 价目表里没有的型号，cost_usd 记 0。但 0 有两种含义：
--   · 真的没花钱
--   · **不知道花了多少**（认不出型号，按 0 记）
-- 只看 cost_usd 分不开这两件事，而账单对不上时正是要查这个。
--
-- ⚠️ 存量行默认 1（认得）：历史记录都是用价目表里的型号调的，
--    默认 0 会把它们全标成"成本不可信"，那是另一种谎。
ALTER TABLE ai_call_logs
  ADD COLUMN cost_known TINYINT NOT NULL DEFAULT 1
  COMMENT '0=价目表里没有这个型号，cost_usd 的 0 是「不知道」不是「没花钱」';

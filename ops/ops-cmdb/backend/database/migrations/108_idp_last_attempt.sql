-- 记住最近一次 SSO 登录失败的原因，显示在接入页上。
--
-- 为什么值得单独存一份：接 SSO 卡住的时候，**失败的人和能改配置的人不是同一个**。
-- 用户看到"取不到用户名"，管理员打开配置页看到一切正常，
-- 中间隔着一次"你截个图发我" —— 而真正的线索（身份源实际返回了哪些字段）
-- 只在后端日志里，管理员多半没有看日志的路径。
--
-- 把它落到配置页上，管理员自己就能闭环：看到实际字段名 → 改「用户名取自」→ 让对方重试。
ALTER TABLE idp_configs
  ADD COLUMN last_error      VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '最近一次失败的原因短码',
  ADD COLUMN last_error_at   DATETIME     NULL DEFAULT NULL,
  ADD COLUMN last_error_user VARCHAR(255) NOT NULL DEFAULT '' COMMENT '谁触发的，便于让本人重试',
  -- 身份源实际返回的 claim 名字，逗号分隔。
  -- ⚠️ 只存**字段名**，不存值：值里有邮箱、姓名这些个人信息，
  -- 而排障需要的只是"有哪些字段可选"。
  ADD COLUMN last_claims     VARCHAR(512) NOT NULL DEFAULT '';

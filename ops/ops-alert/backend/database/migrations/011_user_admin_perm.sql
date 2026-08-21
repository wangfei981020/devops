-- 用户与角色管理的权限码。
--
-- ⚠️ 只给 admin（它靠 unrestricted 生效，不需要在 role_permissions 里列）。
-- 这里**故意不给** rule_admin / oncall / viewer：
-- 能维护规则不等于能建账号、改别人的角色。把这两件事绑在一起，
-- 等于任何一个能改规则的人都能把自己提成管理员。
--
-- 所以本迁移不插入任何 role_permissions 行 —— 它的作用是把这个决定写下来，
-- 让下一个看到"viewer 打不开用户管理"的人知道那是设计而不是遗漏。
SELECT 'menu:alert_users / alert:manage_users 仅管理员可用（admin 靠 unrestricted 生效）' AS note;

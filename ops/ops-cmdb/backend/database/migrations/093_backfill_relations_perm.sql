-- 把新增的 menu:cmdb_relations 补进已存在的内置角色。
--
-- 背景：SeedLocalRoles 用 INSERT IGNORE，角色已存在就整个跳过——
-- 这是刻意的（"绝不覆盖管理员在界面上做过的调整"）。
-- 但**新增一个此前不存在的权限码**是另一回事：管理员从没对它做过决定，
-- 不该被"别覆盖"这条规则挡在外面，否则新功能对所有存量角色永久不可见。
--
-- 所以做一次性的定向回填，而不是改成"每次启动都重灌"——
-- 后者会把管理员刻意去掉的权限又加回来，且毫无提示。
--
-- 只补 admin / viewer / asset 三个角色：
--   admin  —— 全量，本来就该有
--   viewer —— 关系图谱是只读视图，只读角色该看得到
--   asset  —— 它管的就是域名/证书，关系图谱正是这两者的关系
-- cluster / cost 不补：和它们的职责无关。
UPDATE local_roles
SET permissions = JSON_ARRAY_APPEND(permissions, '$', 'menu:cmdb_relations')
WHERE code IN ('cmdb_admin', 'cmdb_viewer', 'cmdb_asset')
  AND JSON_SEARCH(permissions, 'one', 'menu:cmdb_relations') IS NULL;

-- 角色描述要与实际能力一致。
--
-- 008 里给 oncall 写的是「处理告警：认领、静默、标记误报」，
-- 而一期把**认领**从界面上去掉了（值班台只展示，不做处理动作）。
-- 描述比实际多写一项，结果是有人按描述去找认领按钮，找不到就以为是坏了。
--
-- ⚠️ 后端的 alert:ack_incident 权限码与接口**保留不动**：
-- 认领是「先不做」而不是「不做」，接口还在，只是界面上没有入口。
-- 把权限码一起删掉的话，以后恢复认领要连着迁移一起改，代价更大。
UPDATE roles SET description = '查看全部告警，可静默与维护窗口，不改任何检测配置'
 WHERE code = 'oncall';

UPDATE roles SET description = '维护检测规则、场景模板与降噪配置，不能改数据源和通知出口'
 WHERE code = 'rule_admin';

UPDATE roles SET description = '不受权限码约束：含数据源、通知渠道、路由、MCP 令牌与账号管理'
 WHERE code = 'admin';

-- 字典表加英文显示名。
--
-- # 为什么
--
-- 这些字典的值会**原样渲染到界面上**，而它们只有一个中文名字段。
-- 于是英文界面下枚举值全是中文（OPSCMDB-031 P1-76）：
--
--   /admin/basic    生产 / 测试 / 开发 / 域名 / 证书 / 主机 / 负载均衡 /
--                   使用中 / 备用 / 未使用 / 待下线 / 已下线
--   /admin/license  Scope: 生产
--
-- 讽刺的是基础配置页自己说明了「代号会原样出现在接口和 MCP 里，
-- **中文名只用于显示**」—— 说明是对的，但"显示"这件事本身没有考虑第二种语言。
--
-- # ⚠️ 为什么加列而不是塞进语言包
--
-- 这些是**用户可以自己增删改**的字典（基础配置页就是干这个的）。
-- 客户新建一个环境「灰度」，语言包里不可能有它。
-- 所以英文名必须和中文名一样，是**数据**而不是代码。
--
-- # ⚠️ 为什么允许为空
--
-- 空 = 没填英文名，此时界面回退显示中文名。
-- 这比强制填写好：绝大多数客户只用一种语言，逼他们给每个字典项填两遍
-- 是在为一个他们用不到的功能收税。回退是安静且正确的。
--
-- ⚠️ 不给已有行填英文名 —— 那需要逐个翻译，而翻译是产品决定不是迁移该做的事。
-- 内置项的英文名由下面的 UPDATE 补上（它们是我们自己定义的，不是客户数据）。

ALTER TABLE ci_types          ADD COLUMN name_en VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE environments      ADD COLUMN name_en VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE lifecycle_statuses ADD COLUMN label_en VARCHAR(64) NOT NULL DEFAULT '';

-- 内置项的英文名。用 code 定位而不是中文名 —— 客户可能已经改过显示名。
UPDATE ci_types SET name_en = 'Domain'        WHERE code = 'domain'       AND name_en = '';
UPDATE ci_types SET name_en = 'Certificate'   WHERE code = 'certificate'  AND name_en = '';
UPDATE ci_types SET name_en = 'Host'          WHERE code = 'host'         AND name_en = '';
UPDATE ci_types SET name_en = 'Load balancer' WHERE code = 'loadbalancer' AND name_en = '';

UPDATE environments SET name_en = 'Production'  WHERE code = 'PROD' AND name_en = '';
UPDATE environments SET name_en = 'UAT'         WHERE code = 'UAT'  AND name_en = '';
UPDATE environments SET name_en = 'Test'        WHERE code = 'TEST' AND name_en = '';
UPDATE environments SET name_en = 'Development' WHERE code = 'DEV'  AND name_en = '';

-- 生命周期状态没有 code，只能按内置的 label 定位。
-- ⚠️ 客户改过 label 的话这里匹配不上 —— 那正确：改过的就是客户自己的数据，
-- 该由他自己决定英文名，我们不该猜。
UPDATE lifecycle_statuses SET label_en = 'Live (production)'   WHERE scope='project' AND label='已上线（生产）' AND label_en='';
UPDATE lifecycle_statuses SET label_en = 'UAT · on hold'       WHERE scope='project' AND label='UAT·暂停上线'   AND label_en='';
UPDATE lifecycle_statuses SET label_en = 'In use'              WHERE label='使用中'   AND label_en='';
UPDATE lifecycle_statuses SET label_en = 'Standby'             WHERE label='备用'     AND label_en='';
UPDATE lifecycle_statuses SET label_en = 'Unused'              WHERE label='未使用'   AND label_en='';
UPDATE lifecycle_statuses SET label_en = 'To be retired'       WHERE label='待下线'   AND label_en='';
UPDATE lifecycle_statuses SET label_en = 'Retired'             WHERE label='已下线'   AND label_en='';

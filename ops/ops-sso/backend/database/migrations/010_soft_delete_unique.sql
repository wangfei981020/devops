-- 修：软删之后，同一个标识再也建不回来。
--
-- # 怎么发现的
--
-- 给应用做删除功能时先手删了几条测试数据，然后发现 `harbor-1` 这个 code
-- 再也建不出来了 —— 报「已存在」，而界面上那一行明明已经没了。
--
-- # 根因
--
-- 这三张表都有 deleted_at，但唯一索引里没有对应的处理：
--
--     apps         UNIQUE (tenant_id, code)
--     app_routes   UNIQUE (host)
--     oidc_clients UNIQUE (client_id)
--
-- 软删只是把 deleted_at 置上，行还在，于是唯一索引照旧把它算进去。
-- 表现是「删得掉、建不回」，而且报错说的是「已存在」——
-- 人会去列表里找那个"已存在"的东西，找不到。
--
-- ⚠️ 注意这**不是**把 deleted_at 加进唯一索引就能解决的（001 在
-- access_policies 上就那么试过，见迁移 002）：MySQL 的唯一索引不约束 NULL，
-- 加进去之后所有活行（deleted_at 全是 NULL）反而互相不冲突，索引形同虚设。
--
-- # 修法：沿用 002 已经在 access_policies / path_rules 上用的 del_key
--
-- 活着的行恒为 0，软删时置为自己的 id。
--   · 活行之间：del_key 都是 0 → 唯一性照常生效
--   · 删掉的行：del_key 各不相同 → 同一个标识可以反复建、删、再建
--
-- 用 0 而不是 NULL，正是因为 NULL 不参与唯一性 —— 同一个陷阱的两面。
--
-- ⚠️ 与之配套：所有软删这三张表的代码都必须**同时**写 del_key = id。
-- 只写 deleted_at 的话这个迁移等于没做（行仍然占着标识）。

ALTER TABLE apps
  ADD COLUMN del_key BIGINT UNSIGNED NOT NULL DEFAULT 0
      COMMENT '活行恒为 0，软删时置为 id —— 让唯一索引只约束活行（NULL 不参与唯一性）';
UPDATE apps SET del_key = id WHERE deleted_at IS NOT NULL AND del_key = 0;
ALTER TABLE apps
  DROP INDEX uq_apps_tenant_code,
  ADD UNIQUE KEY uq_apps_tenant_code (tenant_id, code, del_key);

ALTER TABLE app_routes
  ADD COLUMN del_key BIGINT UNSIGNED NOT NULL DEFAULT 0
      COMMENT '活行恒为 0，软删时置为 id';
UPDATE app_routes SET del_key = id WHERE deleted_at IS NOT NULL AND del_key = 0;
-- host 的唯一性是**全局**的（不带 tenant_id）：一个域名只能路由到一处，
-- 因为网关是按 Host 头查表的。这一点不变，只是把已删的行让出来。
ALTER TABLE app_routes
  DROP INDEX uq_route_host,
  ADD UNIQUE KEY uq_route_host (host, del_key);

ALTER TABLE oidc_clients
  ADD COLUMN del_key BIGINT UNSIGNED NOT NULL DEFAULT 0
      COMMENT '活行恒为 0，软删时置为 id';
UPDATE oidc_clients SET del_key = id WHERE deleted_at IS NOT NULL AND del_key = 0;
ALTER TABLE oidc_clients
  DROP INDEX uq_oidc_client,
  ADD UNIQUE KEY uq_oidc_client (client_id, del_key);

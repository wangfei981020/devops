-- 修复「软删 + 唯一索引」的经典失效。
--
-- # 症状
--
-- users 表里有 25 个 username='admin' 且 deleted_at IS NULL 的账号。
--
-- # 根因
--
-- 索引写成 UNIQUE KEY (username, deleted_at)，本意是「软删后同名可以再建」。
-- 但 MySQL 的 NULL **不参与唯一性比较**：('admin', NULL) 与 ('admin', NULL)
-- 不算冲突，于是活跃记录也能无限重复——唯一约束对活跃数据完全失效。
--
-- seedAdmin 每次启动都跑，用 ON DUPLICATE KEY UPDATE 做幂等，
-- 而这个索引永远不冲突，所以启动一次就插一条。启动 25 次 = 25 个 admin。
--
-- # 为什么必须修（不只是脏数据）
--
-- 权限判定用 QueryRow 取「username=? AND deleted_at IS NULL」的一条：
-- 有多条时取哪条是不确定的。后果是给管理员降权、改密码之后
-- 「看起来保存成功了但没生效」——因为改的是另一条记录。
--
-- # 修法
--
-- 加一个生成列 alive：未删时 = 1，软删后 = NULL。
-- 唯一索引改用 (…, alive)：
--   活跃记录 → (name, 1) 之间会冲突 ✓ 真正唯一
--   软删记录 → (name, NULL) 不参与比较 ✓ 同名可以再建
-- 这才是原注释想要的语义，只是当时的实现没做到。

-- ── 1. 先清理 users 的重复数据 ────────────────────────────────
--
-- 保留 id 最小的那条（最早创建、被其他表引用的可能性最大），
-- 其余软删而不是物理删除：审计里可能引用过它们，
-- 直接删会让历史记录指向一个不存在的账号。
-- ⚠️ deleted_at 必须每条都不同。
-- 第一版写的是 SET deleted_at = NOW(3)，24 条拿到同一个时间戳，
-- 而此时索引里不再有 NULL —— ('admin', '同一时刻') 之间真的冲突，
-- 迁移直接报 1062。这反过来正好证明了：那个索引只在 NULL 时失效。
--
-- 按 id 往前错开毫秒（DATETIME(3) 是毫秒精度，用 MICROSECOND 错开会被截断成同值）。
UPDATE users u
  JOIN (SELECT username, MIN(id) AS keep_id FROM users
         WHERE deleted_at IS NULL GROUP BY username) k
    ON u.username = k.username
   SET u.deleted_at = NOW(3) - INTERVAL (u.id * 1000) MICROSECOND
 WHERE u.deleted_at IS NULL AND u.id <> k.keep_id;

-- 被软删的账号如果还挂着租户归属，登录后会拿到一个「用户已删但仍在租户里」
-- 的矛盾状态。一并清掉。
DELETE ut FROM user_tenants ut
  JOIN users u ON u.id = ut.user_id
 WHERE u.deleted_at IS NOT NULL;

-- ── 2. 八张表统一换索引 ──────────────────────────────────────
--
-- 顺序固定：先加列 → 再删旧索引 → 再建新索引。
-- 先删索引的话，中间这一小段时间里唯一性完全没有保护。

ALTER TABLE users
  ADD COLUMN alive TINYINT(1) GENERATED ALWAYS AS (IF(deleted_at IS NULL, 1, NULL)) STORED
    COMMENT '活跃标记：未删=1，软删=NULL。只为唯一索引存在，不要在业务查询里用它',
  DROP INDEX uk_users_name,
  ADD UNIQUE KEY uk_users_name (username, alive);

ALTER TABLE tenants
  ADD COLUMN alive TINYINT(1) GENERATED ALWAYS AS (IF(deleted_at IS NULL, 1, NULL)) STORED
    COMMENT '活跃标记：未删=1，软删=NULL',
  DROP INDEX uk_tenants_code,
  ADD UNIQUE KEY uk_tenants_code (code, alive);

ALTER TABLE rules
  ADD COLUMN alive TINYINT(1) GENERATED ALWAYS AS (IF(deleted_at IS NULL, 1, NULL)) STORED
    COMMENT '活跃标记：未删=1，软删=NULL',
  DROP INDEX uk_rule_name,
  ADD UNIQUE KEY uk_rule_name (tenant_id, name, alive);

ALTER TABLE datasources
  ADD COLUMN alive TINYINT(1) GENERATED ALWAYS AS (IF(deleted_at IS NULL, 1, NULL)) STORED
    COMMENT '活跃标记：未删=1，软删=NULL',
  DROP INDEX uk_ds_name,
  ADD UNIQUE KEY uk_ds_name (tenant_id, name, alive);

ALTER TABLE notifiers
  ADD COLUMN alive TINYINT(1) GENERATED ALWAYS AS (IF(deleted_at IS NULL, 1, NULL)) STORED
    COMMENT '活跃标记：未删=1，软删=NULL',
  DROP INDEX uk_nt_name,
  ADD UNIQUE KEY uk_nt_name (tenant_id, name, alive);

ALTER TABLE contact_groups
  ADD COLUMN alive TINYINT(1) GENERATED ALWAYS AS (IF(deleted_at IS NULL, 1, NULL)) STORED
    COMMENT '活跃标记：未删=1，软删=NULL',
  DROP INDEX uk_cg_name,
  ADD UNIQUE KEY uk_cg_name (tenant_id, name, alive);

ALTER TABLE inhibitions
  ADD COLUMN alive TINYINT(1) GENERATED ALWAYS AS (IF(deleted_at IS NULL, 1, NULL)) STORED
    COMMENT '活跃标记：未删=1，软删=NULL',
  DROP INDEX uk_inh_name,
  ADD UNIQUE KEY uk_inh_name (tenant_id, name, alive);

ALTER TABLE correlations
  ADD COLUMN alive TINYINT(1) GENERATED ALWAYS AS (IF(deleted_at IS NULL, 1, NULL)) STORED
    COMMENT '活跃标记：未删=1，软删=NULL',
  DROP INDEX uk_cor_name,
  ADD UNIQUE KEY uk_cor_name (tenant_id, name, alive);

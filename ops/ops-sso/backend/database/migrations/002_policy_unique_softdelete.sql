-- 修：同一主体在同一作用域可以重复配置，唯一索引没拦住。
--
-- # 怎么发现的
--
-- 端到端验证时，先建了「应用 1 · 研发组 · allow」，又建「应用 1 · 研发组 · deny」，
-- 两条都建成功了。此时谁赢取决于排序里最后那个 ID 比较 —— 运维在界面上会看到
-- 两条互相矛盾的规则，并且**改哪一条都可能不生效**。
--
-- # 根因
--
-- 001 里的唯一索引带了 deleted_at：
--
--     UNIQUE (tenant_id, scope, scope_id, subject_type, subject_id, deleted_at)
--
-- 想法是「软删的行不参与唯一性」，但 MySQL 的唯一索引**不约束 NULL** ——
-- 多个 NULL 互不冲突，于是所有未删除的行（deleted_at 全是 NULL）也互不冲突，
-- 索引形同虚设。这个坑的隐蔽之处在于：索引在，`SHOW CREATE TABLE` 看着没问题。
--
-- # 修法
--
-- 加一个 del_key：活着的行恒为 0，软删时置为自己的 id。
--   · 活行之间：del_key 都是 0 → 唯一性生效
--   · 删掉的行：del_key 各不相同 → 同一个主体可以被反复配置、删除、再配置
--
-- 用 0 而不是 NULL，正是因为 NULL 不参与唯一性 —— 这是同一个陷阱的两面。

ALTER TABLE access_policies
  ADD COLUMN del_key BIGINT UNSIGNED NOT NULL DEFAULT 0
      COMMENT '活行恒为 0，软删时置为 id —— 让唯一索引对活行生效（NULL 不参与唯一性）';

-- 存量里已经软删的行，把 del_key 补上，否则它们会互相冲突
UPDATE access_policies SET del_key = id WHERE deleted_at IS NOT NULL AND del_key = 0;

-- 存量里可能已经存在重复的活行（本 bug 的产物）：保留 id 最小的一条，
-- 其余软删。**不静默丢弃** —— 被软删的那条仍在库里，审计能查到。
UPDATE access_policies p
  JOIN (
    SELECT MIN(id) AS keep_id, tenant_id, scope, scope_id, subject_type, subject_id
    FROM access_policies
    WHERE deleted_at IS NULL
    GROUP BY tenant_id, scope, scope_id, subject_type, subject_id
    HAVING COUNT(*) > 1
  ) dup
    ON  p.tenant_id = dup.tenant_id AND p.scope = dup.scope AND p.scope_id = dup.scope_id
    AND p.subject_type = dup.subject_type AND p.subject_id = dup.subject_id
  SET p.deleted_at = NOW(), p.del_key = p.id, p.note = CONCAT(p.note, ' [002 迁移：与规则 ', dup.keep_id, ' 重复，自动收起]')
  WHERE p.deleted_at IS NULL AND p.id <> dup.keep_id;

ALTER TABLE access_policies
  DROP INDEX uq_policy_scope_subject,
  ADD UNIQUE KEY uq_policy_scope_subject
      (tenant_id, scope, scope_id, subject_type, subject_id, del_key);

-- OPSCMDB-082：K8s 类 CI 的 name 里带的是集群**别名**，改成技术名。
--
-- 原来 relations_k8s_edges.go 用 COALESCE(display_name, name) 拼 CI 名：
--     G32 生产/game/game-ing
-- 把一个**可改的**别名写进了 CI 的身份。别名一改，同一个对象下次采集就算成
-- 新 CI，旧的变孤儿、关系边跟着断，而且不报任何错。
--
-- ⚠️ 只改**前缀恰好等于某个集群别名**的那些，且该集群的别名与技术名确实不同。
--    用 CONCAT(别名,'/') 做前缀匹配，避免误伤名字里恰好含该串的其它 CI。
--
-- ⚠️ 幂等：已经是技术名的不会被再改（别名 = 技术名时 WHERE 不成立）。
UPDATE cis c
JOIN k8s_clusters k
  ON k.tenant_id = c.tenant_id
 AND k.display_name IS NOT NULL
 AND k.display_name <> ''
 AND k.display_name <> k.name
 AND c.name LIKE CONCAT(k.display_name, '/%')
SET c.name = CONCAT(k.name, SUBSTRING(c.name, CHAR_LENGTH(k.display_name) + 1))
WHERE c.type IN ('k8s_service', 'k8s_ingress');

-- 事件的幂等键改用 **Event 对象自己的名字**。
--
-- 104 里用的是 (cluster, ns, kind, obj, reason, first_at)。实测发现它会把
-- **不同的 Event 对象合并成一行**：同一个 Pod 拉镜像失败会先后产生
-- `ErrImagePull` 和 `ImagePullBackOff` 两条事件，reason 都是 Failed、
-- 首次时间相同 —— 于是后写的覆盖先写的，界面上只剩一条，
-- 而丢掉的那条恰恰说明了失败是怎么演进的。
--
-- Event 对象在命名空间内名字唯一（apiserver 生成，形如 pod.17e0a3...），
-- 拿它当身份是精确的：同一个对象被多轮采集看到就更新，不同对象各占一行。
ALTER TABLE k8s_events ADD COLUMN event_name VARCHAR(255) NOT NULL DEFAULT '' AFTER namespace;
-- 先补齐历史行的 event_name，否则空串会撞唯一键。
-- 用主键兜底：这些是 104 期间采的，本来就分不清谁是谁
UPDATE k8s_events SET event_name = CONCAT('legacy-', id) WHERE event_name = '';
ALTER TABLE k8s_events DROP INDEX uniq_event;
ALTER TABLE k8s_events ADD UNIQUE KEY uniq_event (cluster_id, namespace, event_name);

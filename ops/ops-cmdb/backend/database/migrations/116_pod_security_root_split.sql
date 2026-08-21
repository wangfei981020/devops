-- OPSCMDB-032：root 判定从「标志位」改成「有效 uid」，并把 init 容器拆出来。
--
-- 原来只有 run_as_root 一个 bool，判据是 `runAsUser==0 || !runAsNonRoot` ——
-- 只要没显式写 runAsNonRoot: true 就算 root。UAT 实测 168 条里 92 条误报（55%），
-- 非 root 整改因此**无法验收**：改完一批数字不降，改过的和没改的在报告里长得一样。
--
-- 拆成三态之后：
--   run_as_root  = 主容器里确定以 uid 0 跑的      → 真安全风险，优先改
--   root_init    = init 容器里有 root 的          → 过不了 restricted PSA，但主进程是安全的
--   root_unknown = 两级都没声明，实际 uid 由镜像 USER 决定 → 判不了，要人去查
--
-- ⚠️ 存量行的这两个新列都是 0，含义是「上一轮采集时还没有这个判据」，
-- 不是「确定没有 root init / 确定已知」。下一轮采集覆盖后才是真值。
-- 采集是全量覆盖写（writeRows 按 cluster_id 清后重写），所以一轮就对齐。

ALTER TABLE k8s_pod_security
  ADD COLUMN root_init    TINYINT NOT NULL DEFAULT 0 COMMENT '1=init 容器里有 root（restricted PSA 不通过，但主容器可能是安全的）';

ALTER TABLE k8s_pod_security
  ADD COLUMN root_unknown TINYINT NOT NULL DEFAULT 0 COMMENT '1=两级都没声明 runAsUser/runAsNonRoot，实际 uid 取决于镜像 USER，判不了';

-- 旧注释写的是「未设 runAsNonRoot」，那正是被修掉的错误判据，一并改掉，
-- 免得后人照着注释把逻辑改回去。
ALTER TABLE k8s_pod_security
  MODIFY COLUMN run_as_root TINYINT NOT NULL DEFAULT 0 COMMENT '1=主容器确定以 uid 0 运行（按有效 runAsUser 判，不看 runAsNonRoot 标志位）';

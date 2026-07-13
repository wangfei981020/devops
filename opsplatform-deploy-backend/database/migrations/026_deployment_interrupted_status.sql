-- Migration 026: deployment.status 增加 'interrupted'（已中断·需重发）
--
-- 场景：一个发布还没 push 到 git，deploy-center 后端就重启了（比如升级代码版本）。
-- 服务实际没有任何变化（git 没提交），旧的 pending 记录成了孤儿。启动对账时：
--   · 若期间没有别人对同模块发过更新版本 → 自动重跑（无感续完，仍是 pending → success）
--   · 若期间已有更新的发布 → 不覆盖，标 'interrupted'，前端橙色提示「服务未受影响，请重试」
--
-- 与 'failed' 区分开：failed = 已 push、部署了但不健康（要排查）；
--                   interrupted = 根本没发生（重启打断），一键重试即可。
--
-- ALTER MODIFY 不动现有数据。

ALTER TABLE deployment
  MODIFY COLUMN status
  ENUM('pending','success','partial','failed','no_change','canceled','interrupted') NOT NULL DEFAULT 'pending';

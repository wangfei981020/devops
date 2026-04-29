-- Migration 020: 扩 deployment 表的 action / status 枚举，让 VM 部署 + 取消能落库
--
-- 001_init_schema 里写死的两个 ENUM 是 K8s 时代的：
--   action ENUM('update_image','restart','rollback')
--   status ENUM('pending','success','partial','failed','no_change')
--
-- VM 部署写入 'vm_rsync' / 'vm_update_version' 时 MySQL 直接 "Data truncated" 报错。
-- 取消任务写入 'canceled' 也撞同样问题（K8s 之前是因为非严格模式被静默截断没暴露）。
--
-- ALTER MODIFY 不动现有数据。

ALTER TABLE deployment
  MODIFY COLUMN action
  ENUM('update_image','restart','rollback','vm_rsync','vm_update_version') NOT NULL;

ALTER TABLE deployment
  MODIFY COLUMN status
  ENUM('pending','success','partial','failed','no_change','canceled') NOT NULL DEFAULT 'pending';

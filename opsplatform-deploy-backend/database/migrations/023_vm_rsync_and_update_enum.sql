-- Migration 023: 扩 deployment.action ENUM 加 'vm_rsync_and_update'
--
-- v121 引入「批量 rsync + 更新」一键 action（vm_rsync_and_update），
-- 但当时漏了改 ENUM。上线后 INSERT deployment 报：
--   Error 1265 (01000): Data truncated for column 'action' at row 1
--
-- 此 migration 把 vm_rsync_and_update 加进 action 枚举，不动现有数据。

ALTER TABLE deployment
  MODIFY COLUMN action
  ENUM('update_image','restart','rollback','vm_rsync','vm_update_version','vm_rsync_and_update') NOT NULL;

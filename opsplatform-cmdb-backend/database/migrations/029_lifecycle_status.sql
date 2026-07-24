-- 生命周期状态：可自定义的状态字典 + 项目/主域名各自的状态字段。
-- scope=project（项目状态）/ domain（主域名状态）。存 label（显示名，可改名，记录侧存 label 字符串，删改不破坏历史值）。
CREATE TABLE IF NOT EXISTS lifecycle_statuses (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  scope      VARCHAR(16)  NOT NULL,
  label      VARCHAR(64)  NOT NULL,
  color      VARCHAR(16)  NOT NULL DEFAULT '',
  sort_order INT          NOT NULL DEFAULT 0,
  UNIQUE KEY uk_scope_label (scope, label)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

INSERT IGNORE INTO lifecycle_statuses (scope, label, color, sort_order) VALUES
 ('project','已上线（生产）','#3b7dd8',1),
 ('project','UAT·暂停上线','#e6a23c',2),
 ('project','仅UAT','#909399',3),
 ('project','已下线','#f56c6c',4),
 ('domain','使用中','#3b7dd8',1),
 ('domain','备用','#409eff',2),
 ('domain','未使用','#909399',3),
 ('domain','待下线','#e6a23c',4),
 ('domain','已下线','#f56c6c',5);

ALTER TABLE projects ADD COLUMN status VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE domains  ADD COLUMN status VARCHAR(64) NOT NULL DEFAULT '';

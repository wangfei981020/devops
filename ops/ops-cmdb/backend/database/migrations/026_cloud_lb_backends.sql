-- 负载均衡后端成员（LB 详情弹窗用）：同步时从转发规则追溯到的后端实例。
CREATE TABLE IF NOT EXISTS cloud_lb_backends (
  id               INT AUTO_INCREMENT PRIMARY KEY,
  cloud_account_id INT          NOT NULL,
  project          VARCHAR(128) NOT NULL,
  lb_name          VARCHAR(128) NOT NULL,
  instance         VARCHAR(255) NOT NULL,
  group_name       VARCHAR(255) NOT NULL DEFAULT '',
  zone             VARCHAR(64)  NOT NULL DEFAULT '',
  synced_at        DATETIME,
  KEY idx_lb (cloud_account_id, project, lb_name)
);

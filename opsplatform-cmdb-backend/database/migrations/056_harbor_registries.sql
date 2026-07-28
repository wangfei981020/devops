-- Harbor 镜像仓库接入
--
-- 补的是 CMDB-004 覆盖度矩阵里「环节 6 推送 Harbor」和「环节 8 拉取镜像」两个空白：
-- 此前 Harbor 完全没接，配额满没满、存储剩多少、GC 有没有在回收、镜像到底推上去没有，
-- 一概查不到；ImagePullBackOff 时也分不清是凭证缺失、Harbor 挂了、还是网络问题。
--
-- 凭证用 Basic Auth（Harbor 的机器人账号形如 robot$name）。密码走 AES 加密，
-- 与 obs_endpoints.token_enc / cdn_accounts 同一套 cipher。
-- 只需要只读权限：接入用的账号给「项目只读 + 系统只读」即可，不要给推送/删除。

CREATE TABLE IF NOT EXISTS harbor_registries (
  id           INT AUTO_INCREMENT PRIMARY KEY,
  name         VARCHAR(128) NOT NULL COMMENT '显示名，如 生产Harbor',
  url          VARCHAR(512) NOT NULL COMMENT '如 https://harbor.example.com，不带尾部斜杠',
  username     VARCHAR(255) NOT NULL DEFAULT '' COMMENT '机器人账号，形如 robot$cmdb-readonly',
  password_enc TEXT                  COMMENT '密码/令牌（AES 加密）',
  env          VARCHAR(32)  NOT NULL DEFAULT '' COMMENT '适用环境 PROD/UAT/DEV，空=通用',
  cluster_id   INT          NOT NULL DEFAULT 0 COMMENT '关联集群，0=通用',
  skip_verify  TINYINT      NOT NULL DEFAULT 0 COMMENT '1=跳过 TLS 校验（自签证书的内网 Harbor）',
  enabled      TINYINT      NOT NULL DEFAULT 1,
  created_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_harbor_enabled (enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Harbor 镜像仓库接入配置';

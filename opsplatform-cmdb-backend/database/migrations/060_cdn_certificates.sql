-- CDN 侧证书（Cloudflare Certificate Packs）
--
-- 与现有的 certs / cert_inspect 是两码事，必须分开存：
--   certs        —— 我方自己签发/管理、部署在源站的证书
--   cdn_certificates —— Cloudflare 边缘上的证书（Universal SSL 等），由 CF 自动续期
--
-- 混为一谈会得出错误结论：边缘证书没过期不代表源站证书没过期，
-- 反过来源站证书过期时，若 SSL 模式是 flexible/full(非 strict)，用户侧甚至看不出异常。
--
-- 需要 token 具备 Zone·SSL and Certificates·Read 权限；没有该权限时采集会明确报错。

CREATE TABLE IF NOT EXISTS cdn_certificates (
  id          BIGINT AUTO_INCREMENT PRIMARY KEY,
  account_id  INT          NOT NULL,
  zone_id     VARCHAR(64)  NOT NULL,
  zone_name   VARCHAR(255) NOT NULL DEFAULT '',
  pack_id     VARCHAR(64)  NOT NULL DEFAULT '',
  type        VARCHAR(48)  NOT NULL DEFAULT '' COMMENT 'universal / advanced / custom',
  hosts       TEXT         COMMENT '覆盖的域名，逗号分隔',
  issuer      VARCHAR(255) NOT NULL DEFAULT '',
  status      VARCHAR(32)  NOT NULL DEFAULT '',
  expires_on  DATETIME     NULL,
  synced_at   DATETIME     NOT NULL,
  KEY idx_cdncert_zone (account_id, zone_id),
  KEY idx_cdncert_exp (expires_on)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='CDN 边缘证书（Cloudflare Certificate Packs）';

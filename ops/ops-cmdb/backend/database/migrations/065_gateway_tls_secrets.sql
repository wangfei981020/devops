-- Istio Gateway 的 TLS 证书来源
--
-- 全链路拓扑里「证书」这一环一直是空的：原来查旧域名台账的 cert_expiry_at，
-- 而那张表对绝大多数域名没有登记。真正决定「用户看到的证书是哪张」的有两处：
--   1. CDN 边缘证书  —— 已有（cdn_certificates），只是没接进拓扑
--   2. Gateway 的 TLS —— servers[].tls.credentialName 指向的 Secret（源站侧证书）
--
-- 第 2 处此前只采到了 tls.mode（SIMPLE/PASSTHROUGH），没采 credentialName，
-- 所以答不出「这个入口用哪张证书」。PROD-002 正是这一类：6 个 Gateway 引用的
-- TLS Secret 不存在，istiod 持续报错，而 CMDB 完全看不到引用关系。

ALTER TABLE k8s_gateways
  ADD COLUMN tls_secrets VARCHAR(1024) NOT NULL DEFAULT ''
  COMMENT 'servers[].tls.credentialName 去重后逗号分隔；Gateway API 侧为 listeners 的证书引用';

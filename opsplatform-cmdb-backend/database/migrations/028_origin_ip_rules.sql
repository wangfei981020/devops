-- 源站映射规则：回源 CNAME → 源站IP（全局，一个回源 CNAME 一条）。
-- 用于业务域名源站IP 的兜底：DNS 查不到 A 记录时用规则值。优先级 手填 > DNS解析 > 本规则。
CREATE TABLE IF NOT EXISTS origin_ip_rules (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  cname      VARCHAR(255) NOT NULL,
  origin_ip  VARCHAR(255) NOT NULL DEFAULT '',
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_cname (cname)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

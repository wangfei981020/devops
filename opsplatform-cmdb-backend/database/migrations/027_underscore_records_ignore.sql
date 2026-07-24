-- 下划线服务记录（如 _domainconnect 的 Domain Connect CNAME）非业务主机，业务域名台账默认忽略。
-- 存量一次性标忽略；未来新导入由 importBusinessRecords 自动处理（仅 INSERT 时设，不覆盖人工取消）。
-- LIKE '\_%' 用反斜杠转义下划线通配符，只匹配真正以 _ 开头的 host。
UPDATE domain_records SET ignored=1, ignore_reason='下划线服务记录，自动忽略'
WHERE host LIKE '\_%' AND ignored=0;

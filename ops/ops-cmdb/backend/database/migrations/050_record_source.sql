-- 解析记录:区分模块/使用中状态的来源(auto=K8s入口自动关联 / manual=手动设置),前端标签区分。
ALTER TABLE domain_records ADD COLUMN module_source VARCHAR(16) NOT NULL DEFAULT '' AFTER module;
ALTER TABLE domain_records ADD COLUMN life_status    VARCHAR(32) NOT NULL DEFAULT '' AFTER module_source;
ALTER TABLE domain_records ADD COLUMN status_source  VARCHAR(16) NOT NULL DEFAULT '' AFTER life_status;

-- 项目/环境可配颜色（hex），域名台账按配置色显示标签；环境沿用 tag_type 不动
ALTER TABLE projects ADD COLUMN color VARCHAR(16) NOT NULL DEFAULT '';
ALTER TABLE environments ADD COLUMN color VARCHAR(16) NOT NULL DEFAULT '';

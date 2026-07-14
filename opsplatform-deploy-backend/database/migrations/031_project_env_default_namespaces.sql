-- Migration 031: project_env 加 default_namespaces（一个项目环境可配多个 namespace）。
--   「项目参数」页配（一行一个 / 逗号分隔）；第一个作默认。
--   新增模块时 namespace 自动填默认，可下拉从列表选、也可手输列表外的。
--   留空 = 默认用环境名。

ALTER TABLE project_env ADD COLUMN default_namespaces VARCHAR(1000) NOT NULL DEFAULT '';

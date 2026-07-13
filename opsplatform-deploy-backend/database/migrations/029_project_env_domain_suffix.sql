-- Migration 029: project_env 加 domain_suffix 列（跟 ingress_gateway / harbor_project 同套路）。
--   「项目参数」页每环境配「域名后缀」，如 uat.slileisure.com。
--   新增模块预填时，前端模块(-frontend)的访问域名自动带出 = <模块名去-frontend>.<域名后缀>；
--   留空 = 域名不自动带出（保持空让用户手填）。可手改。

ALTER TABLE project_env ADD COLUMN domain_suffix VARCHAR(255) NOT NULL DEFAULT '';

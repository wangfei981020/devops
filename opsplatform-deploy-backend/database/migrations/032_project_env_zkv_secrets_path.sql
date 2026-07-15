-- Migration 032: project_env 加 zkv_secrets_path（后端 secret 集中定义处 z-kv-secrets 的 chart 路径）。
--   新增后端模块时，专属 secret 追加到这个路径下的 values.yaml。
--   留空 = 自动推导 <chart_base_path>/z-kv-secrets（新项目都在自己目录）；
--   历史遗留共用的（如 g33 复用 g32）在「项目参数」手动填 charts/g32-uat/z-kv-secrets。

ALTER TABLE project_env ADD COLUMN zkv_secrets_path VARCHAR(500) NOT NULL DEFAULT '';

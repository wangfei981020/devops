-- Migration 030: orchestration_task 加"部署跟踪"字段。
--   新增模块(disable:false 真部署)提交后，跟发布一样轮询新模块的 ArgoCD app 到 Synced+Healthy，
--   「新增历史」展示同步/健康/是否启动成功 + 失败报错(pod)，并支持重试(手动触发 ArgoCD 同步)。
--   argocd_results：跟 deployment.argocd_results 同结构([{app,sync_status,health,duration_sec,msg,...}])。

ALTER TABLE orchestration_task ADD COLUMN argocd_results TEXT              AFTER error_msg;
ALTER TABLE orchestration_task ADD COLUMN duration_sec   INT     NOT NULL DEFAULT 0 AFTER argocd_results;
ALTER TABLE orchestration_task ADD COLUMN disable        TINYINT NOT NULL DEFAULT 0 AFTER duration_sec;
ALTER TABLE orchestration_task ADD COLUMN app_name       VARCHAR(255) NOT NULL DEFAULT '' AFTER disable;
ALTER TABLE orchestration_task ADD COLUMN namespace      VARCHAR(255) NOT NULL DEFAULT '' AFTER app_name;

# opsplatform-deploy-backend API 文档

> 发布系统后端 RESTful API 清单
> 版本: v1
> 基础路径: `/api`

---

## 一、通用约定

### 1.1 请求

- `Content-Type: application/json`
- 所有 POST/PUT 请求体为 JSON

### 1.2 响应格式

**成功：**
```json
{
  "code": 0,
  "message": "success",
  "data": { ... }
}
```

**失败：**
```json
{
  "code": 40001,
  "message": "错误说明",
  "data": null
}
```

### 1.3 错误码

| code | 说明 |
|---|---|
| 0 | 成功 |
| 40000 | 请求参数错误 |
| 40001 | 字段校验失败 |
| 40100 | 未登录/Token 无效 |
| 40300 | 无权限 |
| 40400 | 资源不存在 |
| 40900 | 资源冲突（已存在） |
| 50000 | 服务内部错误 |
| 50001 | Git 操作失败 |
| 50002 | ArgoCD 调用失败 |
| 50003 | K8s 调用失败 |
| 50004 | Harbor 调用失败 |
| 50005 | Lark 通知失败 |

### 1.4 分页参数

```
?page=1&page_size=20&keyword=xx
```
响应：
```json
{
  "code": 0,
  "data": {
    "total": 100,
    "list": [ ... ]
  }
}
```

---

## 二、项目管理 `/api/projects`

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/projects` | 项目列表 |
| GET | `/api/projects/{id}` | 项目详情 |
| POST | `/api/projects` | 新建项目 |
| PUT | `/api/projects/{id}` | 更新项目 |
| DELETE | `/api/projects/{id}` | 删除项目（若关联模块存在则拒绝） |

### POST `/api/projects`

**请求：**
```json
{
  "name": "g50",
  "display_name": "G50 业务线",
  "description": "..."
}
```

**响应：**
```json
{
  "code": 0,
  "data": { "id": 1, "name": "g50", ... }
}
```

---

## 三、环境字典 `/api/environments`

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/environments` | 环境列表 |
| POST | `/api/environments` | 新增环境 |
| PUT | `/api/environments/{id}` | 更新环境 |
| DELETE | `/api/environments/{id}` | 删除（有关联时拒绝） |

### POST `/api/environments`
```json
{
  "name": "uat",
  "display_name": "预发布",
  "auto_sync": 1,
  "sort_order": 2
}
```

---

## 四、项目-环境 `/api/project-envs`

> 对应 GitLab 里某个项目某环境的配置（如 g50-uat 对应一条记录）

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/project-envs?project_id=1` | 某项目的所有环境实例 |
| GET | `/api/project-envs/{id}` | 详情 |
| POST | `/api/project-envs` | 新建 |
| PUT | `/api/project-envs/{id}` | 更新 |
| DELETE | `/api/project-envs/{id}` | 删除（有模块时拒绝） |
| POST | `/api/project-envs/{id}/test-git` | 测试 git 连通性 |
| POST | `/api/project-envs/{id}/test-argocd` | 测试 ArgoCD 连通性 |

### POST `/api/project-envs`
```json
{
  "project_id": 1,
  "env_id": 2,
  "git_repo": "http://gitlab.xx/ops/UAT-K8S-PLATFORM.git",
  "git_branch": "main",
  "git_base_path": "charts/g50-uat",
  "namespace": "g50-uat",
  "argocd_project": "g50-uat",
  "argocd_cluster": "in-cluster"
}
```

---

## 五、Chart 模板管理 `/api/chart-templates`

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/chart-templates?type=backend` | 模板列表（type 可选：backend/frontend） |
| GET | `/api/chart-templates/{id}` | 详情 |
| POST | `/api/chart-templates` | 新建模板 |
| PUT | `/api/chart-templates/{id}` | 更新 |
| DELETE | `/api/chart-templates/{id}` | 删除（被模块引用时拒绝） |
| POST | `/api/chart-templates/{id}/preview` | 预览渲染 values.yaml |

### POST `/api/chart-templates`
```json
{
  "name": "test1",
  "type": "backend",
  "description": "通用后端模板",
  "git_repo": "http://gitlab.xx/ops/chart-templates.git",
  "chart_path": "charts/test1",
  "default_values": "replicaCount: 1\nimage:\n  ...",
  "probe_config": {
    "liveness": { "path": "/health", "port": 8080 },
    "readiness": { "path": "/ready", "port": 8080 }
  },
  "configmap_schema": null,
  "version": "v1"
}
```

前端模板的 `configmap_schema` 示例：
```json
{
  "fields": [
    { "key": "apiGateway", "type": "string", "label": "API网关地址", "required": true },
    { "key": "theme", "type": "enum", "options": ["light","dark"] },
    { "key": "features.tracing", "type": "boolean" }
  ]
}
```

---

## 六、模块管理 `/api/modules`（核心）

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/modules?project_env_id=1&status=active` | 模块列表 |
| GET | `/api/modules/{id}` | 模块详情（含 values 实际渲染） |
| POST | `/api/modules` | 新建模块（复制 chart + 推 git + 建 ArgoCD App） |
| PUT | `/api/modules/{id}` | 更新模块配置（推 git + sync） |
| DELETE | `/api/modules/{id}` | 删除模块（删 ArgoCD + 删 git 目录） |
| POST | `/api/modules/{id}/update-image` | 仅更新镜像 tag |
| POST | `/api/modules/{id}/restart` | 重启服务（调 ArgoCD restart） |
| POST | `/api/modules/{id}/scale` | 修改副本数（支持 0） |
| POST | `/api/modules/{id}/sync` | 手动触发 ArgoCD sync |
| POST | `/api/modules/{id}/rollback` | 回滚到指定发布记录 |
| GET | `/api/modules/{id}/values` | 查看当前 values.yaml 渲染内容 |
| GET | `/api/modules/{id}/runtime` | 运行时状态（Pod 数量、健康、镜像等） |

### POST `/api/modules` — 新建模块
```json
{
  "project_env_id": 1,
  "name": "g50-baccarat-resource-backend",
  "template_id": 1,
  "image_repo": "harbor.xx/g50/g50-baccarat-resource-backend",
  "current_tag": "20260415092722-8",
  "replicas": 1,
  "autoscaling": {
    "enabled": true,
    "minReplicas": 1,
    "maxReplicas": 3
  },
  "resources": {
    "requests": { "cpu": "100m", "memory": "256Mi" },
    "limits":   { "cpu": "1",    "memory": "1Gi" }
  },
  "rolling_update": { "maxSurge": 1, "maxUnavailable": 0 },
  "revision_history_limit": 1,
  "env_vars": [
    { "key": "LOG_LEVEL", "value": "info" }
  ],
  "extra_env_vars": [
    "g50-nacos-secret",
    "g50-redis-secret"
  ],
  "tidb_secrets": [
    { "name": "g50-baccarat-tidb", "database": "baccarat" }
  ],
  "probe_override": null,
  "configmap_data": null,
  "notify_contacts": [1, 2]
}
```

**后端流程：**
1. 校验模块名唯一（project_env_id + name）
2. 加载 chart_templates 模板
3. 渲染 values.yaml
4. `git clone` → 新建目录 `{git_base_path}/{module.name}/` → 写 Chart.yaml + values.yaml + templates/ → commit → push
5. 调 ArgoCD API 创建 Application（name = `{module.name}-{project.name}-{env.name}`）
6. 若 env.auto_sync=1 → 调 sync API
7. 写 modules 表 + 写 deployments 表（action=create）
8. 发 Lark 通知

**响应：**
```json
{
  "code": 0,
  "data": {
    "id": 123,
    "argocd_app_name": "g50-baccarat-resource-backend-g50-uat",
    "git_commit": "abc123def",
    "git_commit_url": "http://gitlab.xx/.../commit/abc123def",
    "argocd_sync_status": "pending"
  }
}
```

### POST `/api/modules/{id}/update-image` — 仅更新镜像
```json
{ "tag": "20260416120000-9" }
```
响应：
```json
{
  "code": 0,
  "data": {
    "deployment_id": 456,
    "from_tag": "20260415092722-8",
    "to_tag": "20260416120000-9",
    "git_commit": "xyz789",
    "argocd_sync_status": "pending"
  }
}
```

### POST `/api/modules/{id}/restart`
无请求体。后端调 ArgoCD：
```
POST /api/v1/applications/{app}/resource/actions
```
执行 Deployment 的 restart 动作。记 deployments 表 (action=restart)。

### POST `/api/modules/{id}/scale`
```json
{ "replicas": 0 }
```
`replicas=0` 时 modules.status 改为 `scaled_zero`，UI 显示"已停机"。

### POST `/api/modules/{id}/rollback`
```json
{ "deployment_id": 456 }
```
恢复到该发布记录的 values_snapshot，commit + sync。

### GET `/api/modules/{id}/runtime`
从 ArgoCD + K8s 拉实时状态：
```json
{
  "code": 0,
  "data": {
    "sync_status": "Synced",
    "health_status": "Healthy",
    "pods": [
      { "name": "xx-5f8c-abc", "status": "Running", "ready": true, "restarts": 0, "image": "harbor.xx/...:v1" }
    ],
    "replicas": { "desired": 2, "ready": 2 }
  }
}
```

---

## 七、Secret 管理 `/api/secrets`

> 数据源：DB（AES 加密存储）+ 同步渲染到 z-kv-secrets/values.yaml

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/secrets?project_env_id=1` | Secret 列表（value 脱敏） |
| GET | `/api/secrets/{id}` | Secret 详情（key 显示，value 默认脱敏，`?reveal=1` 解密返回） |
| POST | `/api/secrets` | 新建 Secret |
| PUT | `/api/secrets/{id}` | 更新 Secret |
| DELETE | `/api/secrets/{id}` | 删除 Secret |
| POST | `/api/secrets/batch-update` | 批量修改同名字段 |
| GET | `/api/secrets/{id}/referenced-by` | 查被哪些模块引用 |

### POST `/api/secrets`
```json
{
  "project_env_id": 1,
  "name": "g50-nacos-secret",
  "type": "Opaque",
  "data": {
    "nacos.addr": "http://10.0.0.1:8848",
    "nacos.user": "admin",
    "nacos.password": "secret123"
  },
  "description": "Nacos 配置"
}
```

**后端流程：**
1. AES 加密 data.value 存 DB
2. 加行级锁（project_env_id 维度）避免并发冲突
3. `git clone` → 读取 z-kv-secrets/values.yaml → 合并/覆盖该 secret → commit → push
4. `git pull --rebase` 冲突时自动重试 3 次
5. 触发 ArgoCD sync（z-kv-secrets app）
6. 写 deployments 表 (action=update_secret)

### POST `/api/secrets/batch-update`
```json
{
  "field_key": "nacos.addr",
  "new_value": "http://10.0.0.2:8848",
  "project_env_ids": [1, 2, 3]
}
```

### GET `/api/secrets/{id}/referenced-by`
```json
{
  "code": 0,
  "data": [
    { "module_id": 10, "module_name": "alert-backend", "reference_type": "extraEnvVars" }
  ]
}
```

---

## 八、通知人管理 `/api/contacts`

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/contacts?status=1` | 通知人列表 |
| GET | `/api/contacts/{id}` | 详情 |
| POST | `/api/contacts` | 新建 |
| PUT | `/api/contacts/{id}` | 更新 |
| DELETE | `/api/contacts/{id}` | 删除 |

### POST `/api/contacts`
```json
{
  "name": "张三",
  "lark_id": "ou_xxxxxxxxxxxxxxxxxxxx",
  "remark": "后端负责人（可空）"
}
```

---

## 九、Lark 群配置 `/api/lark-configs`

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/lark-configs` | 列表 |
| POST | `/api/lark-configs` | 新建 |
| PUT | `/api/lark-configs/{id}` | 更新 |
| DELETE | `/api/lark-configs/{id}` | 删除 |
| POST | `/api/lark-configs/{id}/test` | 发送测试消息 |

### POST `/api/lark-configs`
```json
{
  "name": "发布通知群",
  "webhook_url": "https://open.larksuite.com/open-apis/bot/v2/hook/xxx",
  "secret": "签名密钥（可空）",
  "lark_type": "feishu",
  "description": ""
}
```

---

## 十、项目-环境 通知绑定 `/api/project-env-notify`

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/project-env-notify?project_env_id=1` | 查绑定 |
| PUT | `/api/project-env-notify` | 设置/更新绑定（upsert） |

### PUT `/api/project-env-notify`
```json
{
  "project_env_id": 1,
  "lark_config_id": 2,
  "contact_ids": [1, 3, 5]
}
```

---

## 十一、发布历史 `/api/deployments`

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/deployments?module_id=1&action=update_image&page=1` | 列表（多筛选） |
| GET | `/api/deployments/{id}` | 详情（含 values_before/after） |
| GET | `/api/deployments/{id}/diff` | values.yaml 前后 diff |

### GET `/api/deployments`
```
筛选：module_id / project_env_id / project_id / env_id /
     action / status / operator / time_from / time_to
```

**响应：**
```json
{
  "code": 0,
  "data": {
    "total": 100,
    "list": [
      {
        "id": 456,
        "module_id": 10,
        "module_name": "alert-backend",
        "action": "update_image",
        "from_tag": "v1.0",
        "to_tag": "v1.1",
        "git_commit": "abc",
        "git_commit_url": "...",
        "argocd_sync_status": "success",
        "status": "success",
        "operator": "zhangsan",
        "created_at": "2026-04-16 10:00:00"
      }
    ]
  }
}
```

---

## 十二、Harbor 代理 `/api/harbor`

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/harbor/projects` | Harbor 项目列表 |
| GET | `/api/harbor/repositories?project=g50` | 某项目的镜像仓库 |
| GET | `/api/harbor/tags?repo=g50/alert-backend` | 某仓库的 tag 列表（按时间倒序） |

### GET `/api/harbor/tags`
响应：
```json
{
  "code": 0,
  "data": [
    { "tag": "20260416120000-9", "pushed_at": "2026-04-16 12:00:00", "digest": "sha256:xxx", "size": 12345678 },
    { "tag": "20260415092722-8", "pushed_at": "2026-04-15 09:27:22", "digest": "sha256:yyy", "size": 12000000 }
  ]
}
```

---

## 十三、ArgoCD 代理 `/api/argocd`

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/argocd/applications` | 所有 Application 列表 |
| GET | `/api/argocd/applications/{name}` | 应用详情 |
| GET | `/api/argocd/applications/{name}/status` | 健康/同步状态 |
| POST | `/api/argocd/applications/{name}/sync` | 手动 sync |
| GET | `/api/argocd/applications/{name}/events` | 事件日志 |

---

## 十四、全局配置 `/api/global-config`

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/global-config` | 查询（token 脱敏） |
| PUT | `/api/global-config` | 更新（token AES 加密存 DB） |
| POST | `/api/global-config/test-gitlab` | 测试 GitLab 连通 |
| POST | `/api/global-config/test-harbor` | 测试 Harbor 连通 |
| POST | `/api/global-config/test-argocd` | 测试 ArgoCD 连通 |

### PUT `/api/global-config`
```json
{
  "gitlab_url": "http://gitlab.xx",
  "gitlab_token": "glpat-xxx",
  "gitlab_user": "deploy-bot",
  "gitlab_email": "deploy-bot@xx.com",
  "harbor_url": "http://harbor.xx",
  "harbor_user": "admin",
  "harbor_password": "xxx",
  "argocd_url": "http://argocd.xx",
  "argocd_token": "xxx"
}
```

---

## 十五、环境变量模板 `/api/env-templates`

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/env-templates` | 列表 |
| POST | `/api/env-templates` | 新建 |
| PUT | `/api/env-templates/{id}` | 更新 |
| DELETE | `/api/env-templates/{id}` | 删除 |

### POST `/api/env-templates`
```json
{
  "name": "uat-common",
  "env_vars": [
    { "key": "ENV", "value": "uat" },
    { "key": "LOG_LEVEL", "value": "debug" }
  ],
  "description": "UAT 通用环境变量"
}
```

---

## 十六、健康检查 `/api/health`

| Method | Path | 说明 |
|---|---|---|
| GET | `/api/health` | 服务存活（K8s livenessProbe 用） |
| GET | `/api/ready` | 服务就绪（readinessProbe，会检 DB） |

---

## 附录 A：ArgoCD Application 命名规则

```
{module.name}-{project.name}-{env.name}
```
示例：
- `g50-baccarat-resource-backend-g50-uat`
- `alert-backend-opsplatform-prod`
- `z-kv-secrets-g50-uat`（Secret 专用 app）

## 附录 B：GitLab 仓库组织

**UAT**（所有项目共享一个仓库）：
```
UAT-K8S-PLATFORM/
└── charts/
    ├── g50-uat/
    │   ├── {模块1}/
    │   ├── {模块2}/
    │   └── z-kv-secrets/
    ├── g32-uat/
    └── ...
```

**PROD**（每项目独立仓库）：
```
g50-prod-helm/
└── charts/
    ├── {模块1}/
    ├── {模块2}/
    └── z-kv-secrets/
```

## 附录 C：发布操作类型（deployments.action）

| action | 说明 |
|---|---|
| create | 新建模块 |
| update_image | 更新镜像 tag |
| update_config | 改配置（replicas/env/资源/探针等非镜像） |
| update_secret | 改 Secret |
| restart | 重启服务 |
| scale_zero | 副本数设为 0 |
| scale_up | 副本数恢复 |
| delete | 删除模块 |
| rollback | 回滚到历史版本 |

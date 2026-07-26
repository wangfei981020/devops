# CMDB · K8s 模块设计（k8sinsight 合并 + 轻量多集群管控台）

> 状态：设计稿（2026-07-25）· 只读一期 · 待评审后进入实现
> 决策已定：**k8sinsight 真合并进 CMDB（一个入口、一个登录、集群都在 CMDB 配置）**；一期做到 **Pod + 节点健康（含 IDC 卡死）**；**当前只做只读，不操控任何集群**。

## 1. 背景与定位

### 定位（一句话）
一个以「关系」为中心的**轻量多集群 K8s 管控台**，把 K8s 资产、实时健康诊断，和 **域名 / 证书 / CDN / 云主机 / 成本 / AI** 全链路打通——不做重 Rancher 的集群编排，专注「看得清、连得上、诊得出」（一期只读，轻操作与 AI 管控放后期）。

### 与市面产品的差异
- 纯 CMDB（NetBox / 蓝鲸容器管理）：懂资产关系，**不懂实时健康**（看不到 IDC 节点卡死）。
- 纯 K8s 平台（Rancher / Lens / KubeSphere）：懂实时运维，**不懂业务关系**（不知道某 workload 背后是哪个域名/证书/云主机/成本）。
- **我们两头都占，并把 K8s 与既有 域名/证书/CDN/云主机/成本/AI 打通**——这是现成产品都没有的整合，也是护城河。

### 主要做的事（5 件）
1. 多集群资产纳管（GKE + IDC 异构，一个页面配置看全部）。
2. 全链路关系与影响分析（含 CDN，正向访问 + 反向影响）。
3. 健康与卡死诊断（节点/Pod 实时健康、IDC 节点卡死检出 + AI 诊断）。
4. 变更追溯 / 配置漂移 drift。
5. 轻操作 + AI 管控（**后期**，一期不做）。

## 2. 范围与边界

### 一期范围（做全，只读）
- 集群 / 节点池 / 节点（含卡死检测）/ 命名空间 / 工作负载(Deploy/STS/DS/CronJob/Job) / Service / Ingress·Gateway / **Pod** / 镜像 全纳管。
- 全链路关系（含 CDN）+ 反向影响分析。
- 节点健康 + 卡死检出 + AI 诊断接入点。
- 变更历史 / drift。
- 多集群：GKE（托管）+ IDC（自管）。

### 明确不做（保持「轻」+ 只读）
- ❌ 任何写操作：restart / scale / cordon / drain / apply / exec（**一期全禁**，后期再评估并单独授权）。
- ❌ 建集群 / 装 K8s / 升级集群。
- ❌ Helm 应用商店、CI/CD 流水线、完整多租户 RBAC UI。
- ❌ 读取 Secret 的值（只读元数据：名字/类型/键名，不取 data）。

## 3. 安全模型（回应「容器里跑、网络权限太大颗」）

### 3.1 只连 apiserver，不连节点
- CMDB 只需访问**每个集群的 kube-apiserver 一个端点**（GKE 用 endpoint + SA token；IDC 用 kubeconfig / SA token）。
- **节点卡死等健康数据全部来自 apiserver 的 `node.status`**（conditions / lastHeartbeatTime），无需直连 kubelet / 节点 → 网络面从「N 个节点」缩到「每集群 1 个 apiserver」。

### 3.2 最小只读 RBAC
每个被纳管集群创建专用只读 ServiceAccount，绑定自定义 ClusterRole：
```
verbs: [get, list, watch]           # 仅只读，禁 create/update/patch/delete/exec
resources: nodes, namespaces, pods, services, endpoints, ingresses,
           deployments, statefulsets, daemonsets, cronjobs, jobs, replicasets,
           events, nodes/status, persistentvolumeclaims, horizontalpodautoscalers
# Secret：只允许 list/get 元数据，不在代码里读取/落库 .data（值）
```
- 绝不使用 cluster-admin / edit / view(含 secret 值) 级别。
- 未来若要轻操作，另建独立可写 SA + 二次确认 + 审计，与只读 SA 隔离。

### 3.3 凭据管理
- 集群连接凭据（apiserver 地址 + CA + token / kubeconfig）**AES 加密存库**（复用 CMDB 现有 crypto 加密，与注册商凭据同机制）。
- 凭据只在后端使用，不下发前端；配置页只显示「已配/未配」。

### 3.4 采集方式
- **拉取式**：CMDB 后端定时用只读 SA 调各集群 apiserver 的 list（可选 watch 增量）。
- 频率分级：节点/健康（30s~60s，卡死要快）、工作负载/Service/Ingress（60s~120s）、Pod（60s）、镜像/配额（5m）。
- 采集失败/超时按集群隔离，不互相拖累；写执行记录（复用 CMDB task_run_log）。

## 4. 数据模型（CI 表）

沿用 CMDB 混合模型：通用 `cis` + 专属表 + `ci_labels` + `ci_relations`。K8s 专属表：

| 表 | 关键字段 | 关联 |
|---|---|---|
| `k8s_clusters` | name, provider(gke/idc), region, version, endpoint(加密), env(PROD/UAT), status, node_count | → 云项目 / 机房 |
| `k8s_node_pools` | cluster_id, name, machine_type, min/max/desired, version, autoscale | → cluster |
| `k8s_nodes` | cluster_id, pool_id, name, internal_ip, role, cpu/mem_cap, k8s_version, os, **ready_status(Ready/NotReady/Unknown)**, **last_heartbeat**, conditions(json), **stuck(bool)** | → 节点池；→ 主机 CI(GKE=GCE / IDC=物理机) |
| `k8s_namespaces` | cluster_id, name, quota(json), phase | → 项目/团队 |
| `k8s_workloads` | cluster_id, ns, kind(Deploy/STS/DS/CronJob/Job), name, replicas_desired/ready, image, image_tag, status | → 项目/模块/镜像 |
| `k8s_pods` | cluster_id, ns, name, node_name, workload_id, phase, restarts, pod_ip, start_time | → workload / node |
| `k8s_services` | cluster_id, ns, name, type, cluster_ip, ports(json), selector | → workload |
| `k8s_ingresses` | cluster_id, ns, name, hosts(json), tls(json), backend(json) | **→ 域名 + 证书** |
| `k8s_images` | registry, repo, tag, digest | → Harbor/构建 |
| `k8s_sync_state` | cluster_id, resource, last_sync, ok, err, duration | 采集健康自监控 |

- 变更历史：复用/新增 `k8s_changes`（object, field, old, new, at, source）记录镜像/副本等关键字段变更（drift 与追溯）。
- 关系统一进 `ci_relations`（type: serves/runs_on/belongs_to/fronted_by 等），供全链路查询与影响分析。

## 5. 多集群同步（GKE + IDC 异构）

| | GKE（托管）| IDC（自管）|
|---|---|---|
| 连接 | apiserver endpoint + 只读 SA token（或 GCP SA + connect gateway）| kubeconfig / 只读 SA token |
| 节点池 | GKE Node Pool（机型/伸缩/版本，可从 label `cloud.google.com/gke-nodepool` 或 GKE API）| 节点组 = node label / 手工归组 |
| 「云主机」 | 节点 ↔ GCE 实例 ↔ **现有 GCP 主机模块 + 成本** | 节点 ↔ 机房物理机 CI，成本手录 |
| 卡死 | 偶发 | **重点**（你反馈 IDC 常卡死）|

- 统一抽象 `K8sSource` 适配层（类似 dnsource 的 Adapter）：GKE / 通用 kubeconfig 两种实现，屏蔽差异。
- 一个页面「集群管理」增删集群、配凭据、测连通、看采集健康（`k8s_sync_state`）。

## 6. 全链路关系模型（护城河，含 CDN）

```
用户 → CDN(Cloudflare) → 域名(DNS) → Istio网关/Ingress(+证书)
     → Service → 工作负载(+镜像) → Pod → 节点(所属节点池)
     → 云主机(GKE=GCE实例 / IDC=机房物理机) → 云项目/机房(+成本)
```
- 关系构建：Ingress.hosts ↔ 域名表匹配；Ingress.tls ↔ 证书；Ingress.backend ↔ Service ↔ selector ↔ Workload ↔ Pod.node ↔ Node ↔ 主机/云项目；域名 ↔ CDN（cdns 表既有）。
- **正向**：一个域名点开，看到它这一条链每一跳（对应「遥测蓝图」效果图）。
- **反向影响分析**：选中 节点/节点池/CDN/证书 → 列出受影响的域名/服务（如「default-pool 缩容 / node 卡死 → 影响 ops. / api. 等 5 个域名」）。

## 7. 节点健康 / 卡死检测

判定（全部来自 apiserver node.status，无需连节点）：
- **卡死/失联**：`Ready == Unknown` 或 `lastHeartbeatTime` 距今 > 阈值（如 5min）。
- **压力**：MemoryPressure / DiskPressure / PIDPressure == True。
- **连带**：该节点上 Pod 卡 Terminating / NotReady 计数。
- 节点表落 `stuck` 标志 + 症状摘要，列表红标、可筛「只看异常」。

## 8. AI 诊断接入（对接运维大脑 MCP）

- 节点/工作负载详情页「AI 诊断」按钮：把 `describe`（node/pod status + conditions）+ 最近事件 + 可选指标 打包，交给 **CMDB/k8sinsight MCP**（见 [运维 AI 大脑愿景]），返回根因+建议（如 kubelet OOM / containerd 卡 / 磁盘满 / 内核软死锁 → 建议 cordon+drain+重启）。
- k8sinsight 原有「告警诊断」能力并入，作为该模块诊断引擎。
- 一期先做「取证据 + 调 AI 出建议」，**建议里的操作仍需人工执行**（只读约束）。

## 9. 页面清单

1. **K8s 概览**：集群/节点/命名空间/工作负载数、异常负载、镜像 tag 风险、关联域名（已接 Ingress / 裸奔）、按项目分布。
2. **集群管理**：增删集群、配只读凭据、测连通、采集健康。
3. **节点健康**：跨集群节点列表（池/状态/心跳/压力/Pod 数），卡死红标 + AI 诊断 + 跳详情。
4. **工作负载 / Service / Ingress / Pod**：列表（按 集群/命名空间/项目/环境 筛选）+ 详情 + 变更历史/drift。
5. **全链路关系拓扑**：正向链路 + 反向影响（复用「遥测蓝图」风格）。
6. 详情页统一提供「关系」「变更历史」「AI 诊断」入口。

## 10. API（只读为主）

- `GET /api/k8s/clusters` / `POST`(配置，加密存) / `POST /:id/test`(测连通) / `GET /:id/sync-state`
- `GET /api/k8s/nodes`（筛选/健康）、`/node-pools`、`/namespaces`
- `GET /api/k8s/workloads` / `:id`（含变更历史）、`/pods`、`/services`、`/ingresses`
- `GET /api/k8s/topology?domain=` / `?node=`（正向/反向影响）
- `POST /api/k8s/diagnose`（喂证据给 MCP 出建议）
- （写操作接口一期不提供）

## 11. 分期实施计划

- **阶段 1 · 骨架 + 连接**：K8sSource 适配层（GKE + kubeconfig）、集群管理页、只读 SA 接入、`k8s_clusters/sync_state`、测连通。
- **阶段 2 · 资源纳管**：节点池/节点/命名空间/工作负载/Service/Ingress/Pod 采集 + 表 + 列表/详情/搜索。
- **阶段 3 · 节点健康 + 卡死**：健康判定、节点健康页、异常筛选。
- **阶段 4 · 全链路关系**：ci_relations 构建（含 CDN/域名/证书/主机）、正向拓扑 + 反向影响。
- **阶段 5 · 变更/drift + AI 诊断**：变更历史、drift、diagnose 接 MCP。
- **阶段 6 · k8sinsight 合并收尾**：把 k8sinsight 的实时/诊断能力与前端并入，统一入口，下线独立部署。

每阶段编译或构镜像部署本地 30829 手工验证（沿用 CMDB 一贯节奏）。

## 12. 与现有模块整合

- **域名/证书**：Ingress ↔ 域名/证书自动关联，补全「域名→Pod」链。
- **主机/云成本**：K8s 节点 ↔ 现有 GCP 主机 CI ↔ 成本；IDC 节点 ↔ 机房物理机。
- **k8sinsight**：作为 K8s 模块的实时/诊断数据层被吸收（阶段 6 收尾统一）；**需先通读 k8sinsight 代码**规划移植（多集群 client、诊断逻辑、前端视图）——列为阶段 1 的前置调研。
- **CDN**：cdns 表接入链头。
- **AI/MCP**：诊断接口作为运维大脑第一个 K8s 能力点。

---
待评审：定位 / 只读边界 / 一期范围 / 安全模型 是否 OK；确认后从阶段 1 开工。

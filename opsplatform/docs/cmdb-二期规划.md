# CMDB 二期规划：混合基础设施分层纳管 + 通用能力补强

> 状态：**规划中（范围待定稿）** ｜ 日期：2026-07-24
> 定位升级：一期是「域名/证书/DNS + 云主机」的**垂直运维 CMDB**（护城河已落地）。二期把场景扩展为 **云（GCP）+ IDC 自建（物理机 → 虚拟化 → K8s → 服务）的混合基础设施纳管**，并吸收 iTop/NetBox/蓝鲸 三派招牌里**对本场景有价值**的通用能力。
> 前置文档：[cmdb-开源对标报告.md](cmdb-开源对标报告.md)（六款开源 CMDB 功能矩阵）、[cmdb-功能规划.md](cmdb-功能规划.md)（一期基线）。

---

## 0. 背景：场景从"纯云"升级为"混合基础设施"

一期默认纯云（GCP），当时判定 DCIM/机房/IPAM 无价值。**二期场景变了**：用户 IDC 有物理机，计划

1. 用 IDC 物理机部署**虚拟化集群**；
2. 在其上部署 **K8s 集群**；
3. 跑**业务服务**。

于是 NetBox/蓝鲸那些"物理世界"的招牌（DCIM/IPAM/虚拟化/K8s 拓扑/自动发现）从"负资产"变成**刚需**。二期的核心命题：**把物理 → 虚拟 → 容器 → 应用 → 域名 → 证书这条纵深链路完整纳管，喂给影响分析。**

## 1. 分层资产模型（二期的骨架）

```
应用/服务  ──依赖──> 域名 ──> 证书          (一期已有 ✅)
   │
   └─运行在─> K8s workload ─> pod ─> Node
                                      │
                        ┌──── 是VM ──> 宿主机(hypervisor) ─┐
                        └──── 是物理机 ────────────────────┤
                                                            ▼
                                              物理服务器 ─> 机架 ─> 机房(IDC)
                                                   │
                                          IPMI/BMC带外 + 网段/VLAN(IPAM)
```

一条链能从"某证书快过期 / 某台物理机宕机 / 某个网段耗尽"直接算出**影响哪些应用**——这就是 iTop 影响分析 + NetBox DCIM/IPAM + 蓝鲸拓扑三家招牌在混合场景下的合体。

## 2. 关键架构优势：一期已经铺好路

- **多云 adapter 模式可直接复用**：一期做多云时 `provider` 已贯穿全表 + adapter 工厂（GCP 已做，阿里/AWS/腾讯预留）。**IDC 物理机、虚拟化 VM 本质就是"新 provider"**（`provider=idc` / `vsphere` / `proxmox`）——表结构、主机列表、筛选、成本页零改动，只加采集 adapter。
- **混合数据模型可承接新 CI 类型**：`cis` 通用表 + 专属表 + `ci_relations` + `ci_labels`。新类型（机房/机架/物理机/网络设备/VM/K8s 集群/节点/应用/网段）= cis 加一种 type + 可选专属表 + ci_relations 连边。
- **不要重造已有系统**：已有 **k8sinsight**（多集群只读 + 告警诊断，NodePort 30828）和 **deploy-center**（发布系统）。K8s 纳管应**联邦 k8sinsight 数据**，工单/发布归 deploy-center，CMDB 不重做。

## 3. 招牌吸收决策表（IDC 场景重估）

✅ 吸收 ｜ ◐ 轻量/复用 ｜ 🟡 可选后置 ｜ ❌ 跳过

| 能力 | 来源 | 决策 | 说明 |
|---|---|:--:|---|
| **DCIM 轻量**（机房/机架/U位/物理服务器/网络设备） | NetBox/Ralph | ✅ | 物理机台账刚需，带 IPMI/BMC 带外地址。机架平面图后置 |
| **IPAM**（网段/VLAN/IP 占用） | NetBox | ✅ | 虚拟化 + K8s 落地地基，不做必 IP 冲突。扩现有 cloud-ips 聚合 |
| **虚拟化纳管**（hypervisor→VM） | 蓝鲸/iTop | ✅(缓) | **技术栈未定，先占位记录**（§6），选型后加 adapter |
| **K8s 纳管**（集群/节点/workload） | 蓝鲸 | ◐ | 联邦 k8sinsight，别重做 |
| **应用/服务 CI + 全链路拓扑** | 三家 | ✅ | 串起所有层，影响分析的价值兑现点 |
| **自动发现（IDC 版）** | 蓝鲸/GLPI/NetBox | ✅ | 物理机无云 API，见 §5 发现方案 |
| **影响分析/依赖链** | iTop | ✅ | 基于 ci_relations 图遍历，正反向 |
| **RBAC 多用户/角色** | 全部 | ✅ | 上生产硬门槛，一期只有单 admin |
| **变更历史/回溯页** | NetBox/蓝鲸 | ✅ | 有 WriteAudit 底层，补可视化 diff 回溯 |
| **Webhook/事件订阅** | 蓝鲸/NetBox | ✅ | 扩现有飞书通知为通用事件总线 |
| **全局搜索** | 全部 | ◐ | 跨类型统一搜索入口 |
| **Excel 批量导入** | 全部 | ◐ | 有导出缺导入，首次灌存量需要 |
| **SSO/LDAP** | 全部 | 🟡 | RBAC 之后接（opsplatform 已有 SSO 可对接） |
| **资产折旧/财务** | Ralph | 🟡 | 物理机是采购资产（采购/保修/折旧），云按量付费不需要。要管物理资产财务再做 |
| **SNMP 网络拓扑** | NetBox | 🟡 | 接交换机、补端口/MAC/邻居时再上 |
| **GraphQL** | NetBox | 🟡 | REST 够用，锦上添花 |
| **机房平面图/机架 U 位可视化** | NetBox/Ralph | 🟡 | DCIM 台账立住后可做 |
| ITIL 工单/事件/变更/发布/服务台 | iTop/GLPI | ❌ | 有 deploy-center，工单是另一个产品 |
| agent 自动发现 / 进程管理 | 蓝鲸/GLPI | ❌ | 软件层由虚拟化端 + k8sinsight 覆盖，不值得维护 agent |

## 4. 二期功能模块（含数据模型草案）

> 数据模型延续混合模型：每类新 CI 在 `cis` 建 type，type 专属字段进专属表，跨 CI 关联走 `ci_relations`。

### M1. RBAC 多用户/角色 🔵〔硬门槛，第一优先〕
- 表：`rbac_users` / `rbac_roles` / `rbac_permissions` / `rbac_user_roles`；授权粒度：**按 CI 类型 + 按项目/环境**（复用现有 projects/environments）。
- 中间件在现有 JWT 基础上加角色/权限校验；菜单按权限渲染。
- SSO 后置（opsplatform 已有 SSO，可对接，非第一步）。

### M2. IPAM 网段/IP 管理 🔵〔虚拟化/K8s 前置地基〕
- 表：`ip_subnets`（网段/CIDR/VLAN/网关/用途/所属机房）、`ip_addresses`（IP/状态 used·reserved·free/绑定的 CI/MAC）。
- 扩现有 `cloud-ips` 聚合：把 IDC 物理机内网 IP、VM IP、K8s service/pod CIDR 都纳入统一 IP 视图。
- 网段用量/冲突检测、可用 IP 分配建议。

### M3. DCIM 物理资产层 🟢
- 表：`idc_sites`（机房）、`racks`（机柜，含 U 位总数）、`physical_servers`（物理服务器：型号/序列号/CPU/内存/磁盘/U位/**IPMI/BMC 地址**/保修）、`network_devices`（交换机/路由器/硬件防火墙）。
- 关系：物理机 located_in 机架 located_in 机房。
- 机架平面图/U 位可视化 🟡 后置。

### M4. 自动发现引擎（IDC 版）🟢
- 表：`discovery_jobs`（发现任务：类型 ipmi·snmp·nmap / 目标网段·凭据 / 定时）、`discovery_results`（待确认入库的发现项，人工 review 后落 CI）。
- adapter：见 §5，先做 **IPMI/Redfish + nmap**。
- **对账**：发现 vs 台账差异（未登记设备、失联设备），喂 IPAM/DCIM。

### M5. 虚拟化纳管 🟢〔技术栈未定，占位，见 §6〕
- 表（草案）：`hypervisors`（可复用/关联 physical_servers）、`virtual_machines`（VM：宿主机/vCPU/内存/磁盘/IP/资源超分）。
- 关系：VM hosted_on hypervisor。adapter 按选型（Proxmox/vSphere/oVirt）实现。

### M6. K8s 纳管（联邦 k8sinsight）◐
- 表：`k8s_clusters` / `k8s_nodes` / `k8s_workloads`（尽量引用 k8sinsight 数据，CMDB 侧只存 CI + 关系，不重复采集）。
- 关系：workload runs_on node，node 是 VM 或物理机（连回 M3/M5），workload 属于应用（连 M7）。

### M7. 应用/服务 CI + 全链路影响分析 🟢〔价值兑现点〕
- 表：`applications`（服务/应用：负责人/所属业务/环境/仓库/镜像）、可选 `service_instances`（运行实例）。
- 关系：应用 → K8s workload → pod → node(VM/物理机) → 机架 → 机房；应用 → 域名 → 证书。
- **影响分析页**：选任一 CI，正向（我依赖谁）/反向（谁依赖我）图遍历，算受影响面。基于 `ci_relations`，是一期关系图谱的深化。

### M8. 变更历史/回溯 🟢
- 复用/升级 WriteAudit：`change_logs`（对象/字段/旧值/新值/操作人/时间/request-id 分组）。
- 可视化回溯页 + 字段级 diff，敏感资产（证书/域名/物理机）优先。

### M9. Webhook/事件订阅 🟡
- 表：`event_subscriptions`（事件类型/回调 URL/密钥/过滤）。
- 事件源：证书签发·续期、域名到期、资产变更、发现对账差异、物理机/K8s 告警。
- 扩现有飞书通知为通用事件总线，回调 deploy-center 等下游。

### M10. 全局搜索 / Excel 导入 ◐
- 跨域名/证书/主机/物理机/IP/应用 统一搜索入口。
- Excel 模板批量导入（首次灌存量），对齐现有导出。

## 5. 物理机自动发现方案（推荐已定）

物理机无云 API，台账手填会腐烂，必须有采集。**推荐组合：IPMI/Redfish 带外 + nmap 扫网段先上，SNMP 后置，agent 不上。**

| 手段 | 采什么 | 定位 | 排期 |
|---|---|---|---|
| **IPMI/Redfish 带外** | 硬件事实（型号/序列号/CPU/内存/电源/温度）+ 远程开关机 | **主力**，物理机的"云 API 等价物"，不装 agent 不侵入，BMC 网络可达即可 | 二期 |
| **nmap 扫网段** | 活跃 IP/开放端口/发现未登记设备 | **兜底入口**，喂 IPAM，台账 vs 现实对账 | 二期 |
| SNMP | 交换机端口/MAC/邻居 | 补网络拓扑 | 二期后半 🟡 |
| agent | 软件层（进程/已装软件） | ❌ 不上：该层由虚拟化端 + k8sinsight 覆盖，不值得维护 agent | — |

原则：**带外拿硬件、扫描拿存在性、SNMP 拿拓扑**，三层递进。

## 6. 虚拟化技术栈：未定，待选型（占位）

用户暂未定虚拟化栈，**二期是否做也未定，先记录**。选型直接决定 M5 的采集 adapter，候选：

| 候选 | 特点 | 采集方式 |
|---|---|---|
| **Proxmox VE** | 开源/轻量/自带 REST API，中小 IDC 主流 | Proxmox REST API 拉 宿主机/VM/存储 |
| **VMware vSphere** | 企业级、vCenter API 成熟 | vCenter API，但有商业授权成本 |
| **oVirt / OpenStack** | 开源企业级，功能全但重 | 各自 API，适合较大规模私有云 IaaS |

> 待用户定型后再补 M5 详细设计。选型建议维度：机器规模、团队运维熟悉度、是否要私有云 IaaS 级能力、授权成本。

## 7. 推荐排期

二期主线 = **补齐混合基础设施分层纳管 + 上生产必需的通用标配**：

1. **M1 RBAC**〔硬门槛，先做〕
2. **M2 IPAM + M3 DCIM 物理层**〔IDC 地基，虚拟化/K8s 落地前置〕
3. **M4 自动发现（IPMI/Redfish + nmap）**〔让物理台账不腐烂〕
4. **M5 虚拟化纳管**〔选型后做〕
5. **M6 K8s 纳管（联邦 k8sinsight）**
6. **M7 应用/服务 CI + 全链路影响分析**〔价值最大化，把所有层串起来〕
7. **M8 变更历史 / M9 Webhook / M10 全局搜索 & 导入**〔通用增强，可穿插〕

**并行护城河**（不冲突，独立排期）：证书自动部署闭环、DNS 写回厂商 + 多厂商、CT Log 监控、域名自动续费（详见 [cmdb-开源对标报告.md](cmdb-开源对标报告.md) §9 及项目记忆 backlog）。

## 8. 边界与不做

- ❌ ITIL 工单/事件/变更/发布/服务台 —— 归 deploy-center，不重做。
- ❌ agent 自动发现 / 进程管理 —— 软件层由虚拟化端 + k8sinsight 覆盖。
- ❌ 追 NetBox 完整 IPAM（VRF/前缀树）、iTop 全套 ITSM —— 只做够用，不追广度。
- 🟡 机房平面图、资产折旧财务、SNMP、SSO、GraphQL —— 后置，按需再上。

> 战略定位：**垂直深（ACME/域名/DNS 别人没有）+ 混合基础设施够用（物理→虚拟→容器→应用分层纳管）+ 不背无用包袱（工单/纯物理 DCIM 细节/agent）**。

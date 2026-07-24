# 顶尖开源 CMDB 功能对标报告

> 日期：2026-07 ｜ 对象：iTop · NetBox · 蓝鲸CMDB · Ralph · GLPI · i-doit
> 目的：对标主流开源 CMDB 的完整功能，看我们这套「域名/证书/DNS 运维专用 CMDB」缺什么、又强在哪
> 来源：各产品官方文档 / GitHub / 官网（见文末）

## 0. 总览与定位

六款覆盖了开源 CMDB 的三大流派：
- **ITSM 型**：iTop、GLPI、i-doit —— CMDB + 工单一体
- **网络/数据中心型**：NetBox、Ralph —— 真相源 + DCIM/IPAM
- **通用运维型**：蓝鲸 CMDB —— 主机三层模型 + 自动发现

它们横向做得广，但在**证书自动化、域名/DNS 到期运维**这一垂直块普遍空白——这正是我们的差异化。

---

## 1. iTop（Combodo）— CMDB + 全套 ITIL ITSM

技术栈 PHP/MySQL，AGPL。

- **模型/CI**：预置 server/VM/hypervisor/app/DB/network/contract/location/person/team；iTop Designer 低代码拖拽自定义类与字段；生命周期状态；许可/保修到期提醒
- **关系**：★ **影响分析（impact analysis）**——图形化依赖视图，一眼看清改/删某 CI 谁受影响（招牌功能）
- **发现/同步**：强数据同步引擎（federate 多源）；采集器 Azure/vSphere/Ansible/OCS Inventory/VMware/Microsoft Graph
- **权限/审计**：RBAC 按 profile + 部门数据隔离；SSO（SAML/OAuth/OpenID/CAS）；MFA；暴力破解防护；一致性审计（数据质量）
- **API/集成**：全 REST API；Webhook（Slack/Teams）；PowerBI
- **视图/大盘**：自定义 Dashboard、Kanban、Gantt、日历；报表导出 CSV/Excel/PDF
- **ITSM**：事件/请求/问题/变更/发布（ITIL）；服务目录；SLA；知识库；客户门户；项目管理（PMI）
- **证书/域名**：❌ 无 ACME 签发；仅「日期属性到期提醒」可泛覆盖证书/合同/许可，不签发、不同步 DNS

## 2. NetBox — 网络真相源 · DCIM/IPAM

技术栈 Python/Django/PostgreSQL，Apache 2。集成能力最强。

- **模型/CI**：DCIM（机架/设备/设备类型/端口/线缆/电源）+ 虚拟化 + 电路 + 租户；自定义字段 + 标签
- **IPAM**：★ **业界最强 IPAM**——IP/前缀/聚合/VLAN/VRF/ASN
- **关系/拓扑**：设备-线缆-接口连接关系、线缆追踪；无 ITIL 影响分析（靠拓扑）
- **发现/同步**：无内置 agent 发现（它是「真相源」，由外部自动化写入）；强 API 供 Ansible/Terraform 写
- **权限/审计**：对象级 RBAC；LDAP/SSO；★ **变更日志**——全对象增删改留痕、归因到用户、按 request ID 分组
- **API/集成**：★ **REST + GraphQL + Webhook（Event Rules）**；自定义脚本/报表；丰富插件生态
- **导入/搜索**：CSV 批量导入；全局搜索；自定义字段/视图
- **证书/域名**：❌ 核心无；第三方插件 netbox-ssl 可**追踪** ACME/LE 证书 + 到期邮件告警，但官方定位「**只追踪监控，不签发、不碰私钥**」；DNS 靠 netbox-dns 插件记录，不同步厂商

## 3. 蓝鲸 CMDB（BlueKing / 腾讯）— 企业级通用运维 CMDB

技术栈 Go/Python + MongoDB。国内运维主流。

- **模型/CI**：内置 业务/集群/模块/主机/进程 三层拓扑；★ **自定义模型 + 字段 + 关联关系**（网络/中间件/虚拟资源纳管）；字段模板、唯一校验、字段分组
- **关系/拓扑**：可拓展的业务拓扑；通用对象关联
- **发现/采集**：★ **主机数据自动发现**（节点管理 agent 采集）；机器数据快照；云资源同步
- **进程管理**：基于模块的主机进程管理
- **事件**：★ **变更事件主动推送**——回调方式的事件注册与订阅
- **权限/审计**：精细权限（按业务/模型/用户组）；操作审计与回溯
- **API/集成**：蓝鲸 ESB / API 网关，与作业平台/监控打通
- **证书/域名**：❌ 聚焦主机/进程，无证书/域名/DNS 能力

## 4. Ralph（Allegro）— 数据中心资产 · DCIM · 财务

技术栈 Python/Django，Apache 2。

- **模型/CI**：数据中心资产 + 办公硬件；资产状态（new/in use/free/damaged/liquidated/to deploy）
- **生命周期**：★ **transitions 生命周期自动化**；采购追踪；**自动折旧**（可配规则）；完整变更/转换历史
- **DCIM**：交互式机房平面图（机架布局/配电/U位）；网络拓扑可视化
- **IPAM/网络**：IP 地址管理；DHCP/DNS 集成（部署侧）
- **财务**：采购/成本/折旧/合同（六款里最强的资产财务）
- **API**：token 认证 REST API（JSON）
- **证书/域名**：❌ 无 ACME；DNS 集成偏部署，非厂商同步/到期巡检

## 5. GLPI — IT 资产 + ITIL 服务台 + 盘点

技术栈 PHP/MySQL，GPL。社区活跃度极高。

- **模型/CI**：SACM 资产配置管理；CI 关系与依赖/影响；电脑/外设/网络设备及组件；生命周期
- **发现/采集**：★ **原生动态盘点（v10+）+ GLPI Agent**（Win/Linux/macOS/Android）：采集/网络发现/SNMP/ESX 远程盘点/软件部署
- **ITSM**：事件/问题/请求/变更/发布（ITIL）；知识库；服务台/helpdesk
- **资产**：软件许可、消耗品、合同、供应商管理
- **权限/扩展**：多用户角色；LDAP/SSO（插件）；庞大插件生态
- **证书/域名**：◐ 有**证书资产类型**可登记证书 + 到期提醒（比 iTop 稍强的「记录」），但不 ACME 签发、不同步 DNS

## 6. i-doit — IT 文档 + 关系型 CMDB

技术栈 PHP/MySQL，AGPL（Open 版）。

- **模型/CI**：大量预置对象类型（可隐藏）；自定义对象类型/字段；图形化机架视图；集群/SAN/虚拟化/blade/chassis
- **关系**：关系映射（设备连接/软件/集群）；★ **自动生成依赖链**
- **文档**：IT 文档为核心（应急预案、关键信息）
- **API**：JSON-RPC API
- **版本差异**：Open（个人/小规模、基础）vs Pro（团队/大规模、协作 + 高级集成）
- **证书/域名**：❌ 可作为对象类型登记，无 ACME/DNS 自动化

---

## 7. 横向能力对比矩阵

✓ 内置 ｜ ◐ 部分/需插件/仅记录 ｜ ✗ 无

| 能力维度 | iTop | NetBox | 蓝鲸 | Ralph | GLPI | i-doit | 我们的CMDB |
|---|:--:|:--:|:--:|:--:|:--:|:--:|:--:|
| 自定义模型/字段 | ✓ | ◐ | ✓ | ◐ | ◐ | ✓ | ◐ |
| 关系建模 | ✓ | ✓ | ✓ | ◐ | ✓ | ✓ | ◐ |
| 影响分析/依赖链 | ✓ | ◐ | ◐ | ✗ | ◐ | ✓ | ✗ |
| 拓扑可视化 | ✓ | ✓ | ✓ | ✓ | ◐ | ✓ | ◐ |
| 自动发现(agent/扫描) | ◐ | ✗ | ✓ | ◐ | ✓ | ◐ | ✗ |
| 云资源同步 | ✓ | ◐ | ✓ | ✗ | ◐ | ✗ | ✗ |
| IPAM / DCIM | ◐ | ✓ | ✗ | ✓ | ◐ | ◐ | ✗ |
| RBAC 多用户/角色 | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | **✗** |
| SSO / LDAP | ✓ | ✓ | ✓ | ◐ | ✓ | ◐ | ✗ |
| 变更历史/回溯 | ✓ | ✓ | ✓ | ✓ | ✓ | ◐ | ◐ |
| REST API | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| Webhook/事件订阅 | ✓ | ✓ | ✓ | ✗ | ◐ | ✗ | ✗ |
| 批量导入(CSV/Excel) | ✓ | ✓ | ✓ | ◐ | ✓ | ✓ | ✗ |
| 全局搜索/大盘 | ✓ | ✓ | ✓ | ◐ | ✓ | ◐ | ◐ |
| **证书到期记录** | ◐ | ◐ | ✗ | ✗ | ◐ | ◐ | **✓** |
| **ACME 自动签发/续期** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | **✓** |
| **WHOIS 域名到期** | ✗ | ✗ | ✗ | ✗ | ✗ | ✗ | **✓** |
| **DNS 厂商同步** | ✗ | ◐ | ✗ | ✗ | ✗ | ✗ | **✓** |
| **到期告警(飞书/邮件)** | ◐ | ◐ | ◐ | ✗ | ◐ | ✗ | **✓** |

---

## 8. 关键结论：证书/域名这块，通用 CMDB 普遍缺

1. **没有一款通用开源 CMDB 内置「证书自动签发 + 续期」**。六款全部止步于「记录证书到期日」。连做得最好的 NetBox，也只有第三方插件 netbox-ssl，且官方明确「只追踪监控，不生成证书、不碰私钥」——它等你用 certbot 签好再回来登记。**我们的 CMDB 是反过来的**：内置 lego + ACME 主动签发和续期，管私钥（AES 加密）、A+ 拉取式落地。

2. **WHOIS 域名到期 + DNS 厂商同步几乎无人做**。通用 CMDB 不会主动 WHOIS 查注册到期、不连 443 探证书、更不会从 GoDaddy/阿里云 DNS 拉解析。NetBox 的 netbox-dns 插件也是手工/自动化写入，不主动同步厂商。我们的「主域名 + 业务域名 + DNS 记录 + 到期巡检 + 飞书提醒 + 定时任务」是开箱即用的闭环。

---

## 9. 给我们的补强建议（按性价比排）

别去追通用 CMDB 的广度（主机/云/工单不是我们的战场）。补的是几个通用标配、且能强化垂直闭环的能力：

1. **RBAC 多用户/角色**〔必补〕—— 现在只有单 admin，是上生产的硬门槛。至少「用户 + 角色 + 按项目/环境授权」。
2. **变更历史/回溯页面** —— 有 WriteAudit 审计，但缺「谁在何时把哪字段从 A 改成 B」的可视化回溯。证书/域名敏感资产尤其需要。
3. **影响分析/依赖链路** —— 已有 域名→证书 关系，延伸成 域名→证书→LB→应用 + 影响面。iTop 的招牌，契合证书运维。
4. **Webhook/事件订阅** —— 证书签发/续期、域名快到期时回调下游发布系统。已有飞书通知，扩成通用 webhook。
5. **批量导入（Excel 模板）** —— 有导出、缺导入，首次灌存量数据需要。
6. **全局搜索** —— 跨域名/证书/DNS 的统一搜索入口。

---

## 来源
- iTop：combodo.com/features、github.com/Combodo/iTop
- NetBox：netboxlabs.com/docs、github.com/netbox-community/netbox、github.com/ctrl-alt-automate/netbox-ssl（追踪型插件）
- 蓝鲸 CMDB：github TencentBlueKing/bk-cmdb、bookstack.cn/read/bk-cmdb-doc
- Ralph：github.com/allegro/ralph、ralph-ng.readthedocs.io
- GLPI：glpi-project.org/features、github.com/glpi-project/glpi
- i-doit：i-doit.org、kb.i-doit.com

> 评级为基于公开文档的定性判断，个别插件/版本能力可能随更新变化。

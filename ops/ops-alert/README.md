# OpsAlert · 告警与事件响应平台

> 企业版商业产品。一期只做**日志侧**（ES / OpenSearch / Loki），**不替代夜莺**——
> 指标告警仍归夜莺，两边各管各的。指标检测、SLO、自愈编排、外部告警入站均为架构预留。

| | |
|---|---|
| 效果图 | [docs/design/opsalert-v2-mockup.html](../../docs/design/opsalert-v2-mockup.html)（11 视图，第 11 页是分期路线图） |
| 工程约定 | [ops/CONVENTIONS.md](../CONVENTIONS.md)（四态、租户隔离、非 root、版本注入…） |
| 设计系统 | [docs/design/design-system.md](../../docs/design/design-system.md) |
| 本地端口 | 30835（经 k8s-proxy 转发） |

## 它和上一代（opsplatform-alert）的区别

上一代能发告警，但不是一个平台：只有「发送流水」，回答不了"现在几个问题没解决、
谁在处理、平均多久恢复"；48 列的规则表塞了四种玩法；`*lark.Sender` 出现在检测引擎的
函数签名里；租户隔离靠 handler 自己记得拼 `WHERE project_id`。

这一版按顺序解掉：租户隔离层 → 事件模型 → 场景化规则 → 数据源/渠道适配器。

## 五项别人没有的能力

1. **判定链**（`decision_traces`）——八步摊开：查询 → 阈值 → 持续 → 抑制 → 静默 → 路由 → 投递 → 升级。
   正向回答"凭什么触发"，反向回答"我以为该响的怎么没响"。
2. **规则回放 backtest** —— 拿历史数据试算噪音，答三问：真实故障还抓得到吗、噪音降多少、深夜会不会叫醒人。
3. **拓扑根因收敛** —— 父事件取自 CMDB 的 K8s 事件，不依赖客户是否部署了指标监控。
4. **变更关联** —— 事件自动挂上发布/配置变更。
5. **静默故障检测** —— 数据源停报、规则查询失败、渠道限流，把"没有告警"和"告警系统坏了"分开。

## 目录

```
backend/           Go 1.25 + gin + MySQL
  database/migrations/   6 个迁移，21 张表
  internal/store/        租户隔离层（不声明 tenant_id 的 SQL 直接拒绝执行）
  datasource/            数据源适配器：loki / elasticsearch(opensearch)
  engine/                检测引擎：调度、判定链、事件、投递、回放、旧规则导入
  notify/                通知渠道适配器：feishu / webhook
  internal/api/          HTTP 接口
frontend/          React 19 + Vite + Tailwind v4 + @ops/{design,i18n,ui}
deploy/helm/       前后端同一个 chart，一次发布一次回滚
```

## 本地跑起来

```bash
# 1. 库（一次性）
docker exec mysql-deploy mysql -uroot -p'…' -e "CREATE DATABASE opsalert …"

# 2. 后端
cd backend && GOWORK=off go build -o /tmp/opsalert-backend . && \
  MYSQL_USER=alert_user MYSQL_PASSWORD='…' MYSQL_DATABASE=opsalert \
  PORT=:18090 HEALTH_PORT=:18098 /tmp/opsalert-backend

# 3. 前端（开发模式，/api 代理到 18090）
cd frontend && pnpm dev      # http://localhost:5274

# 4. 镜像与部署（版本号收口在构建脚本里，不要直接 docker build）
../tooling/scripts/build-image.sh ops-alert v0.1.1 backend  --push
../tooling/scripts/build-image.sh ops-alert v0.1.1 frontend --push
helm upgrade --install opsalert deploy/helm -n ops-alert \
  -f deploy/helm/values-local.yaml --set backend.image.tag=v0.1.1 --set frontend.image.tag=v0.1.1
```

## MCP（AI 接入）

JSON-RPC over HTTP，端点 `POST /api/v1/mcp`，令牌走 `X-MCP-Token` 头。
界面在「平台 › AI 接入 (MCP)」发令牌，一个接入方一条，明文只显示一次（库里存 sha256）。

10 个只读工具：`selfcheck` `list_rules` `get_rule` `list_incidents` `get_incident`
`why_fired` `why_not_fired` `dry_run` `list_datasources` `noise_top`。

**`why_not_fired` 是这套 MCP 存在的主要理由**：给一个规则 ID，它回答卡在判定链的哪一步——
规则被停用 / 一直在报错 / 从未执行 / 调度停了 / 查询没命中 / 未达持续周期 / 被抑制 / 被静默 /
匹配不到路由。排查漏告警在别的系统里只能靠人翻日志猜。

三条实现约定：
- **一期只读**。建规则、改阈值、静默、认领都不给 AI——和「系统不自动改阈值」同一个理由。
  令牌已带角色位，将来开写工具不用改表。
- **工具失败必须置 `isError`**。返回成功外壳装错误内容，AI 会把它理解成"查到了但是空的"，
  转述给人就成了"该项没有数据"。这是 CMDB MCP 上真实发生过的事。
- **工具描述里写清语义陷阱**。例如 `selfcheck` 的描述直接告诉模型「规则执行失败时事件列表也是空的，
  空列表不等于健康」——不写的话它一定会把空读成好。

租户来自令牌而不是请求参数，鉴权后每一次业务查询都走 `store.Scoped`。

## 三条不能违反的约定

**1. 失败必须看起来像失败。** 查询失败落 `rules.last_error` 并计入连续失败次数；
数据源探测结果落库；投递失败单独记录。「7 天触发 0 次」和「查询一直在失败」
在界面上绝不能长成一样——上一代的头号问题就是安静被读成太平。

**2. 查询失败不等于未命中。** 数据源不可达时不累加 miss 计数，事件不会被误判恢复。
否则"监控挂了"会表现成"告警全部自动恢复"。

**3. 业务代码拿不到裸 `*sql.DB`。** 租户数据走 `store.Scoped`，SQL 里必须有
`tenant_id = ?`（值由本层注入）；平台级表走 `store.Platform` 且要在
`whitelist.go` 登记。防的是"忘记"，不是"故意"。

## 一期已完成 / 未做

**已完成**：检测引擎（四类日志场景各自的判定语义、for 持续、指纹收敛、自动恢复、无数据告警）、
字段提取与 `{{变量}}` 模板、事件模型与时间线、判定链、抑制/静默/路由树/路由试算、
投递与重复通知、恢复通知、未认领升级、数据源探测与自检、回放实验室、噪音榜、
旧规则翻译与导入、审计、多租户隔离、**MCP（10 个只读工具）**、Helm chart、镜像（Trivy 0 漏洞）。

**未做（明确不做或留给二期）**：
- **值班排班**（用户决定先不加）：升级链目前指向 `contact_groups`，将来加排班时改指向"当班人"即可，表不用推倒
- **授权门控**：`ops-kit/licensekit` 尚未接入，当前无功能分档
- 二期：指标/SLO/异常检测/SQL 规则、夜莺与 Alertmanager 入站、自愈编排、复盘、对外状态页

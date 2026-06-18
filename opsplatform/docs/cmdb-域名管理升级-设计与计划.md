# CMDB 域名管理升级 — 设计与计划

> 状态：定稿待开发 ｜ 日期：2026-06-18
> 在 CMDB 一期（[cmdb-一期设计.md](cmdb-一期设计.md)）基础上，把「扁平域名」升级为「域名→解析 两层 + 多数据源同步 + DNS 记录管理」。

---

## 1. 需求汇总（本轮确认）

1. **域名→解析 两层**：主域名（注册单位）下挂多条解析记录。
2. **多数据源**：域名/解析从域名厂商同步，支持多厂商（GoDaddy 先做，阿里云/腾讯/DNSPod/Cloudflare 预留），adapter 可扩展。厂商凭据与"注册商"（签证书用）统一成一份。
3. **解析字段**：主机名、CDN(下拉)、回源 CNAME、源站 IP、证书到期、项目、环境、模块、操作人、创建/更新时间（回源/源站/CDN 可选，内网解析可空）。
4. **CDN 厂商**：基础配置里单独维护列表，录入时下拉选。
5. **操作人**：去掉"负责人"，改"操作人"=最后编辑/操作者，自动记录登录账号+时间。
6. **数据库为准**：列表/分页/搜索/筛选全查 DB，零 API 调用；只有「同步」才调厂商 API。
7. **DNS 记录单独页**：列某域名在厂商的全部 DNS 记录（A/CNAME/MX/TXT/NS/CAA…），关键记录(`_acme-challenge` 等)🔒受保护标记，**当前版本只读**。
8. **客户端限流**：每个数据源(厂商)独立限流，**50 次/分钟固定窗口**，超了主动 429（带窗口、可重试时刻、倒计时秒），批量自动排队。
9. **API 用量卡片**：本分钟已用/剩余、今日累计、上次 429（CMDB 自己计数）。

**不做（下一期）**：在 CMDB 改解析**直接写回厂商**（编辑/删/增 DNS 记录推 GoDaddy），带二次确认+审计+受保护。

## 2. 多数据源设计（adapter）

```
domain_sources（域名数据源 = 厂商账号，统一凭据）
  id, name, provider(godaddy/aliyun/tencent/dnspod/cloudflare),
  credential_enc(AES), enabled, rate_limit_per_min(默认50)

adapter 接口（每厂商一个实现）：
  ListDomains() []Domain            // 拉账户下域名 + 到期
  ListRecords(domain) []DNSRecord   // 拉某域名全部 DNS 记录
  （DNS-01 加 TXT 仍走 lego provider，凭据共用 domain_sources）

当前实现：GoDaddy adapter；其余 provider 预留接口，后续补。
```
> domain_sources 取代/合并一期的 `registrars` 表（一份厂商凭据，同步域名 + 签证书 DNS-01 共用）。迁移：registrars 数据并入 domain_sources。

每个数据源独立限流器（厂商限制不同，GoDaddy 60→客户端设 50）。

## 3. 数据模型

```
domain_sources   厂商数据源(见上)
domains          域名层: ci_id, name, source_id, registrar_provider, expiry_at(域名注册到期)
domain_records   解析层(新表):
                   id, domain_ci_id, host(主机名/全名), record_type,
                   cdn_id, cname(回源), origin_ip, cert_expiry_at, cert_check_msg,
                   project, env, module, operator, source_id,
                   protected(受保护,如_acme-challenge), created_at, updated_at
dns_records      厂商原始 DNS 记录(同步缓存): domain_ci_id, type, name, data, ttl, priority, protected, synced_at
cdns             CDN 厂商列表(基础配置): id, name, sort_order
```
- 解析的 project/env/module 放 domain_records（每条解析各自归属）；域名层只管注册信息。
- `operator` 每次增改自动写登录账号；`updated_at` 自动。
- DB 为准：所有列表查 DB；同步把厂商数据写入 domains/domain_records/dns_records。

## 4. 关键设计

- **同步流程**：点同步 → 按 source 调 adapter.ListDomains/ListRecords → 写/更新 DB（受限流器约束，批量排队）。单向（厂商→DB）。可选定时同步。
- **限流器**：每 source 一个内存计数器（mutex + 当前自然分钟计数），≥50 主动返回 429 结构 `{window, used, limit, retry_at, retry_after_seconds}`，前端倒计时 + 自动重试；批量同步内部排队等下个窗口。
- **受保护记录**：同步时识别 `_acme-challenge.*`、`NS @` 等标 protected，DNS 记录页禁改（下一期反向写时强制拦截）。
- **API 用量**：限流器顺带统计本分钟/今日调用数，设置页卡片展示。

## 5. 阶段计划

- **阶段 A 后端·数据源+模型**：migration（domain_sources/domain_records/dns_records/cdns + registrars 迁移）；domain_sources CRUD；CDN CRUD。
- **阶段 B 后端·GoDaddy adapter + 限流**：adapter 接口 + GoDaddy 实现(ListDomains/ListRecords)；每源限流器(50/min, 429结构)；API 用量统计。
- **阶段 C 后端·同步 + 两层 CRUD**：同步引擎(厂商→DB)；域名/解析两层 CRUD；操作人自动记录；DNS 记录拉取接口。
- **阶段 D 前端·菜单+基础配置**：菜单调整(DNS记录单独页)；基础配置加 CDN；数据源配置页(替代/扩展注册商)。
- **阶段 E 前端·域名两层页**：主域名分组 + 解析表格；录入表单(CDN下拉/回源/源站/项目环境/可选)；同步按钮 + 限流 429 倒计时提示。
- **阶段 F 前端·DNS记录页 + 用量卡片**：DNS 记录单独页(只读,筛选,受保护标记)；设置页 API 用量卡片。
- **阶段 G 构建部署验证**：Trivy0/npm0 → 推 localhost:8070(+marks26) → 部署 30829 → 端到端验证 → 推 github。

每阶段 go build / npm build 编译过，关键阶段构镜像部署 30829 手工验证（沿用一期习惯，本地 mysql-deploy 即测试环境）。

## 6. 兼容/迁移

- 一期已有的 `domains`(扁平) 数据：迁成域名层 + 把原有信息转成一条默认解析（或保留）。
- `registrars` → `domain_sources`（provider/凭据搬过去）。
- 证书申请的"关联域名"逻辑不变（取 source 凭据做 DNS-01）。

# CMDB 一期设计定稿

> 状态：**定稿 v1** ｜ 日期：2026-06-16
> 范围：CMDB 第一期 = **域名 + 证书 + CI 骨架（模型/关系）+ 证书全自动 ACME + 到期提醒 + Prometheus 指标 + 内置展示台**
> 上游：[cmdb-功能规划.md](cmdb-功能规划.md)。本文件为一期开发唯一基线。

---

## 1. 目标

统一管 IT 资产（CI）+ 关系的库，第一期落地域名/证书两类 CI，并满足核心诉求：**免费证书自动签发/续期、到期提醒、证书到期数据进 Prometheus、内置展示台**。

## 2. 架构与工程约定

- 新建 **`opsplatform-cmdb-backend`**（Go+Gin）/ **`opsplatform-cmdb-frontend`**（Vue3+ElementPlus+Pinia）。
- 现 `opsplatform-domain-backend` 的域名/注册商能力**重写迁入** CMDB，旧项目后续废弃。
- DB：本地 mysql-deploy:13307 的 `cmdb` 库（独立账号）；集群内经 `host.docker.internal:13307`。
- 镜像 `localhost:8070/opsplatform/opsplatform-cmdb-backend|frontend`；K8s namespace `opsplatform`；**NodePort 30829**。
- 业务端口 8080 + **独立 8088 健康/`/metrics` 端口**（与 k8sinsight/gke-version 对齐）。
- 登录：**本地 admin 密码登录**（users 表 bcrypt + JWT，首启 seed admin/见环境变量）。RBAC/SSO 第一期延后。
- 构建流程：`docker build --no-cache`（前端注入 VERSION 徽章）→ Trivy 全严重度 0 → npm audit 0 → 推 localhost:8070 → kubectl apply 到 30829。

## 3. 数据模型（混合：通用 CI 表 + 类型专属表）

```
ci_types        CI 类型(domain/certificate，预置，可扩展)
cis             CI 通用表: id, type, name, project, env, module, owner, status, remark, created_at, updated_at
ci_labels       自定义键值标签: ci_id, k, v           （自由加，配合导出白名单）
ci_relations    关系: src_ci_id, dst_ci_id, rel_type  （certificate -protects-> domain 等）
domains         域名专属: ci_id, registrar_id, dns_provider, expiry_at, resolve_status, ...
certificates    证书专属: ci_id, cn, sans(JSON), ca, challenge(dns-01/http-01),
                          status, issued_at, expiry_at, auto_renew, renew_days(默认30),
                          cert_pem, chain_pem, key_pem_enc(AES加密), deploy_token, last_error
cert_history    签发/续期历史: cert_ci_id, action, result, at, detail
registrars      注册商/DNS 凭据(迁自 domain, 凭据 AES 加密): id, name, provider, credential_enc
acme_accounts   ACME 账户: id, email, ca(letsencrypt/zerossl), account_key_enc, status
settings        飞书 webhook、提醒天数[30,15,7,1]、可导出 label 白名单、ACME 默认账户...
audit_logs      who/action/target/at/ip
users           admin: username, password_hash(bcrypt), ...
```

- **通用维度** `project / env / module` 是 cis 表固定字段（页面下拉/填写），所有 CI 都带。
- **私钥 `key_pem_enc`**：AES-GCM 加密入库，密钥从 K8s Secret/env 注入，绝不明文落库/返回。

## 4. 证书 ACME 流程

- CA：**Let's Encrypt 默认**，ZeroSSL 备选（acme_accounts 配账户邮箱，邮箱设置页单独配）。
- 验证：**DNS-01 默认**（复用 registrars 的 DNS 厂商凭据，支持泛域名 `*.x.com`）；**HTTP-01 兜底**（手动域名无凭据时，或手动加 TXT 记录）。
- 库：Go 用 `github.com/go-acme/lego/v4`（成熟、支持多 DNS provider + ACME）。
- 流程：申请 → 选域名(来自域名 CI) → 验证 → 签发 → 存库(证书+加密私钥) → 建 `certificate -protects-> domain` 关系。
- **自动续期**：每日定时任务扫 `expiry_at - now < renew_days` 且 `auto_renew=1` 的证书，自动续签更新库；**ACME 限速**做退避，失败写 last_error + 告警。
- 吊销：调 ACME revoke。

## 5. A+ 取证书（拉取式落地，不主动推送）

- 每证书一个 `deploy_token`。后端暴露：
  - `GET /api/certs/:id/bundle?token=xxx` → 返回 `fullchain.pem` + `key.pem`（或 tar），带 `If-None-Match`/版本号，证书没变返回 304。
- **目标端自助拉**（一份示例脚本随文档/前端提供）：
  - 服务器：cron 每天 `curl` 拉取，比对版本，变了就覆盖 + `nginx -s reload`。
  - K8s：CronJob 拉取写入 `Secret`，工作负载挂载该 Secret。
- 平台**不持有目标 SSH/kubeconfig**（最小权限）。主动推送(B)留二期。

## 6. Prometheus 指标（/metrics，独立 8088 端口）

gauge，值 = unix 秒，维度齐全（你点名的 项目/环境/模块/创建/到期 全覆盖）：
```
cmdb_cert_expiry_timestamp_seconds{project,env,module,name,cn,ca,domain, <白名单自定义label>}
cmdb_cert_created_timestamp_seconds{project,env,module,name,ca, ...}
cmdb_domain_expiry_timestamp_seconds{project,env,module,name,registrar, ...}
```
- **自定义 label**：cis 的 ci_labels 里，**仅导出 settings 白名单勾选的 label key**（控高基数，防 VM 写爆）。默认导出 `project/env/module/name/ca/registrar`。
- Grafana 算剩余天数 `(指标 - time())/86400`；告警 `指标 - time() < 7*86400`。
- 一份到期数据，两个出口：DB（展示台用）+ /metrics（Prometheus 用）。

## 7. 到期提醒

- 每日任务：证书 + 域名，到期前 **30/15/7/1 天** 触发，经 **飞书 webhook**（settings 配）推送；续期失败也告警。

## 8. 前端页面 / 菜单

`总览 / 域名 / 证书 / 关系图谱 / 展示台 / 模型管理 / 设置`
- **总览**：资产统计卡片 + 即将到期列表（证书/域名，点进处理）。
- **域名**：列表（手动录入 + 从注册商同步）、到期、关联证书、解析状态。
- **证书**：列表（CN/SAN/CA/到期/自动续期开关/状态）、**申请证书向导**（选域名→验证方式→CA→自动续期→生成 deploy_token）、续期/下载/吊销/详情。
- **配置项详情**：基本属性 + **关系**（证书↔域名）+ 续期历史。
- **关系图谱**：证书—域名链路可视化（echarts）。
- **展示台**：深色大屏，资产总数/按环境分布/到期排行/30 天倒计时，可投屏，数据来自 CMDB。
- **模型管理**：CI 类型/字段（地基，第一期预置域名/证书两类）。
- **设置**：注册商/DNS 凭据、ACME 账户(邮箱)、飞书 webhook、提醒天数、**可导出 label 白名单**。
- 前端禁用浏览器原生弹窗，统一 appStore.showConfirm。

## 9. 后端 API（主要）

```
POST /api/login                              admin 登录
GET/POST/PUT/DELETE /api/cis                 CI 通用增删改查(按 type 筛)
GET/POST/PUT/DELETE /api/domains             域名(迁移 domain 能力)
POST /api/domains/sync                       从注册商同步
GET/POST/DELETE /api/certs                   证书列表/申请/吊销
POST /api/certs/:id/renew                    手动续期
GET  /api/certs/:id/bundle?token=            A+ 取证书(目标端拉取)
GET  /api/certs/:id/download                 前端下载(登录态)
GET/POST/PUT/DELETE /api/registrars          注册商/DNS 凭据
GET/PUT /api/settings                        飞书/提醒/白名单/ACME 账户
GET  /api/dashboard                          展示台聚合数据
GET  /api/relations                          关系图谱数据
GET  /metrics                                Prometheus(8088 端口)
```

## 10. 一期边界（不做）

- RBAC/SSO（admin 直登）｜主机/应用/数据库等更多 CI 类型｜B 主动推送部署｜云/K8s 自动发现｜影响分析/变更版本回溯/数据质量巡检｜Grafana 大屏（数据已进 Prometheus，可自建）。

## 11. 交付物

独立 cmdb 前后端 + 域名迁入 + 证书全自动 ACME(DNS-01/HTTP-01, LE/ZeroSSL) + 自动续期 + 飞书到期提醒(30/15/7/1) + A+ 取证书 API/脚本 + /metrics(自定义 label 白名单) + 内置展示台 + 私钥加密。NodePort 30829，admin 密码登录。

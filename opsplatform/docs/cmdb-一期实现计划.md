# CMDB 一期实现计划

> 依据：[cmdb-一期设计.md](cmdb-一期设计.md)。本计划是执行路线图，**重启/换会话后据此续接**。
> 验证方式（按用户习惯）：不强制 TDD 单测；本地 mysql-deploy:13307 即测试环境，每阶段「go build / npm build 编译过 → 关键阶段构镜像部署 30829 手工验证」。勾选 `- [ ]` 跟踪进度。

**Goal:** 落地 CMDB 第一期：域名+证书 CI、证书全自动 ACME、A+ 拉取式落地、Prometheus 指标、内置展示台、飞书到期提醒。

**Architecture:** 独立 `opsplatform-cmdb-backend`(Go+Gin) / `opsplatform-cmdb-frontend`(Vue3+ElementPlus+Pinia)，MySQL `cmdb` 库，K8s namespace opsplatform，NodePort 30829，admin 密码登录。

**Tech Stack:** Go+Gin、go-acme/lego v4、MySQL、Vue3+ElementPlus+ECharts、Prometheus client_golang。

---

## 文件结构（后端 opsplatform-cmdb-backend）

```
main.go                      路由装配 + 8080 业务 / 8088 健康+metrics
config/config.go             env 配置(DSN/JWT/AES key/端口)
database/db.go               连库
database/migrations/*.sql    建表 + runner(embed)
crypto/aes.go                AES-GCM 加解密(凭据/私钥)
handlers/auth.go             admin 登录 + JWT 中间件 + seed
handlers/ci.go               CI 通用 CRUD + 标签 + 关系
handlers/domains.go          域名(迁自 domain-backend)
handlers/registrars.go       注册商/DNS 凭据(加密)
handlers/certs.go            证书 列表/申请/续期/吊销/下载
handlers/bundle.go           A+ 取证书 /certs/:id/bundle?token=
handlers/settings.go         飞书/提醒/白名单/ACME 账户
handlers/dashboard.go        展示台聚合
handlers/relations.go        关系图谱数据
handlers/audit.go            审计
handlers/metrics.go          /metrics 注册 + 采集器
acme/issuer.go               lego 封装: 签发/续期/吊销 (DNS-01/HTTP-01)
acme/providers.go            注册商凭据 -> lego DNS provider 映射
notify/feishu.go             飞书 webhook
jobs/scheduler.go            每日任务: 自动续期 + 到期提醒
models/models.go             结构体
k8s-deploy.yaml              Deployment/Service/Secret(NodePort 30829)
Dockerfile
```

前端 `opsplatform-cmdb-frontend`（沿用 k8sinsight 结构）：
```
src/App.vue                  布局+菜单(总览/域名/证书/关系图谱/展示台/模型管理/设置)
src/router, src/stores       路由 + pinia(auth/app)
src/api/*.js                 http + 各模块 api
src/views/                   Overview Domains Certs CertDetail Relations Dashboard Models Settings Login
src/components/CertApplyDialog.vue   申请证书向导
Dockerfile, nginx.conf, k8s-deploy.yaml(30829)
```

---

## 阶段 0：后端脚手架 + admin 登录

**Files:** config/config.go, database/db.go+migrations/runner.go, crypto/aes.go, handlers/auth.go, handlers/health.go, main.go, go.mod, Dockerfile

- [ ] 初始化 go module `opsplatform-cmdb-backend`，引入 gin / mysql / jwt / bcrypt
- [ ] config.Load：MYSQL_*、PORT(:8080)、HEALTH_PORT(:8088)、JWT_SECRET、CMDB_AES_KEY
- [ ] database.Open + migrations.Run(embed `*.sql`)（复用 k8sinsight runner 模式）
- [ ] crypto/aes.go：AES-GCM Encrypt/Decrypt(key 来自 CMDB_AES_KEY)
- [ ] migration 001：`users` 表；auth.EnsureAdmin seed admin（密码 env 或默认 admin123 bcrypt）
- [ ] handlers/auth.go：POST /api/login → JWT；Middleware 校验
- [ ] health.go：8088 /health /ready；main 装配 /api 分组
- [ ] **验证**：`go build ./...` 通过；本地 env 连 mysql-deploy 跑起来，curl /api/login 拿到 token

## 阶段 1：DB 全表 migrations

**Files:** database/migrations/002_cmdb_core.sql

- [ ] 建表：ci_types, cis, ci_labels, ci_relations, domains, certificates, cert_history, registrars, acme_accounts, settings, audit_logs（字段见设计 §3）
- [ ] 预置 ci_types：domain / certificate
- [ ] settings 默认行：提醒天数[30,15,7,1]、可导出 label 白名单默认 project/env/module/name/ca/registrar
- [ ] **验证**：启动跑完 migration，`SHOW TABLES` 全部存在

## 阶段 2：CI 通用 + 标签 + 关系

**Files:** handlers/ci.go, handlers/relations.go, models/models.go

- [ ] CI 通用 CRUD（按 type 筛，含 project/env/module/owner/status/标签）
- [ ] ci_labels 增删（键值）；ci_relations 增删查（certificate-protects-domain）
- [ ] 审计埋点（WriteAudit）
- [ ] **验证**：go build；curl 建一个 CI + 加 label + 建关系

## 阶段 3：域名迁入（重写 domain-backend 能力）

**Files:** handlers/domains.go, handlers/registrars.go, acme/providers.go

- [ ] registrars：注册商/DNS 凭据 CRUD，credential AES 加密存（参考 domain-backend）
- [ ] domains：手动录入 + 列表 + 到期 + 关联证书；每个域名是一个 domain 类型 CI
- [ ] domains/sync：从注册商拉域名（迁移 domain-backend sync 逻辑）
- [ ] **验证**：go build；手动加域名 + 配一个 DNS 凭据 + 同步

## 阶段 4：证书 ACME（核心）

**Files:** acme/issuer.go, acme/providers.go, handlers/certs.go, handlers/settings.go(ACME账户)

- [ ] 引入 `github.com/go-acme/lego/v4`
- [ ] acme_accounts：邮箱+CA 注册（LE/ZeroSSL），account_key 加密存
- [ ] issuer.Issue：选域名→DNS-01(注册商凭据映射 lego provider)/HTTP-01→签发→返回 cert/chain/key
- [ ] certs：申请(存证书+AES加密私钥+建 protects 关系+生成 deploy_token)、列表、详情、吊销、手动续期、登录态下载
- [ ] **验证**：构镜像部署到 docker-desktop（或本地跑），用一个真实域名+DNS凭据签一张证书（LE staging 环境先试），确认入库+私钥加密

## 阶段 5：A+ 取证书 API + 拉取脚本

**Files:** handlers/bundle.go, 文档/前端附 deploy 脚本

- [ ] GET /certs/:id/bundle?token= → fullchain.pem+key.pem，带版本号/ETag，未变 304
- [ ] 提供目标端脚本：服务器 cron(curl+比对+nginx reload)、K8s CronJob(拉取写 Secret) 示例
- [ ] **验证**：curl 带 token 拉取 bundle；改证书后版本号变化

## 阶段 6：Prometheus /metrics

**Files:** handlers/metrics.go

- [ ] client_golang 注册自定义采集器，每次 scrape 查 DB 输出 gauge：
      cmdb_cert_expiry_timestamp_seconds / cmdb_cert_created_timestamp_seconds / cmdb_domain_expiry_timestamp_seconds
- [ ] 固定 label project/env/module/name/ca/domain/registrar + settings 白名单内的自定义 label
- [ ] 挂 8088 端口 /metrics
- [ ] **验证**：curl :8088/metrics 看到证书/域名到期指标 + label 正确；白名单外的 label 不出现

## 阶段 7：自动续期 + 飞书到期提醒

**Files:** jobs/scheduler.go, notify/feishu.go

- [ ] 每日 ticker：扫 auto_renew 且将到期(<renew_days) → issuer.Renew，失败退避+告警
- [ ] 到期前 30/15/7/1 天（证书+域名）→ feishu webhook 推送
- [ ] **验证**：手动触发任务，飞书收到测试消息；续期更新 expiry_at

## 阶段 8：前端脚手架 + 布局

**Files:** 整个 opsplatform-cmdb-frontend 骨架（拷 k8sinsight 结构改造）

- [ ] Vue3+Vite+ElementPlus+Pinia+Router；App.vue 暗色侧栏菜单(用 el-menu 分组)
- [ ] Login 页 + auth store + http 拦截器(JWT)
- [ ] **验证**：npm run build 过；登录进主框架

## 阶段 9：前端业务页面

**Files:** src/views/* + components/CertApplyDialog.vue + api/*

- [ ] 总览(统计卡+到期列表)、域名(列表+录入+同步)、证书(列表+申请向导+续期/下载/吊销)
- [ ] 配置项详情(属性+关系+续期历史)、关系图谱(echarts)、展示台(深色大屏 echarts)
- [ ] 模型管理(CI 类型/字段)、设置(注册商/ACME邮箱/飞书/提醒天数/可导出label白名单)
- [ ] 禁用原生弹窗，用 appStore.showConfirm
- [ ] **验证**：npm build；各页面跑通连后端

## 阶段 10：构建 + 部署 + 验证

- [ ] 后端 Trivy 全严重度 0（Go 依赖 + alpine）；前端 npm audit 0 + Trivy 0
- [ ] 镜像推 localhost:8070；k8s-deploy.yaml(Secret 含 JWT/AES key/DB) apply 到 namespace opsplatform，NodePort 30829
- [ ] k8s-proxy nginx 加 30829 转发（参考 30828）
- [ ] **端到端验证**：浏览器 30829 登录 → 加域名 → 配 DNS 凭据 → 签证书(LE staging→正式) → 看展示台 → curl :8088/metrics → 飞书提醒

---

## 自检（spec 覆盖）

- 架构/模型/admin登录/30829 → 阶段0-1 ✓
- CI+标签+关系 → 阶段2 ✓
- 域名迁入+注册商 → 阶段3 ✓
- 证书 ACME(DNS-01/HTTP-01,LE/ZeroSSL,续期,吊销,私钥加密) → 阶段4 ✓
- A+ 取证书+脚本 → 阶段5 ✓
- /metrics+自定义label白名单 → 阶段6 ✓
- 自动续期+飞书提醒(30/15/7/1) → 阶段7 ✓
- 前端全页面+展示台+关系图谱 → 阶段8-9 ✓
- Trivy0/npm0/部署30829/端到端 → 阶段10 ✓
- 一期边界(无RBAC/SSO、无B推送、无更多CI类型) 保持 ✓

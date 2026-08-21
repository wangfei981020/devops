# OpsAlert 生产部署

**当前版本 `v0.9.8`**，前后端同一个 tag，linux/amd64。

| 镜像 | digest |
|---|---|
| `marks26/ops-alert-backend:v0.9.8` | `sha256:bf2846bed2e211eaeee35b8aa28e807292a99b88dfd2e44d229b8aacf652adb0` |
| `marks26/ops-alert-frontend:v0.9.8` | `sha256:9fa0ec42702e7729bdb0d4a699bc1d9ab278a027146f9987d165c3193cd50873` |

Trivy 全严重度扫描：前端 0，后端 1 条**已显式豁免**的信息类告示
（`GO-2026-5932`：`golang.org/x/crypto/openpgp` 子包停止维护，非 CVE、无修复版本；
本项目只用 `bcrypt`，依赖图里没有 openpgp，豁免理由见 `backend/.trivyignore`）。

> 敏感配置**只在 Secret 一个地方**。values 的 `backend.env` 里只有两个非敏感项：`OPS_LICENSE_ENV` 和 `TZ`。

---

## 🔴 部署前必读

**告警认领和值班排班没有做**（是你定的"先不做"）。认领接口和权限码保留着，界面上没有入口。

**企业版功能需要激活。** 未激活时（`status: not_activated`）：

| 功能 | 表现 |
|---|---|
| 噪音治理 / 回放实验室 | 菜单可见带 EE 角标，点进去是"这是企业版功能"说明页 |
| AI 接入（MCP） | **菜单不显示，路由 404** —— 未授权时不希望对方知道有这个功能 |
| 其余全部功能 | 正常可用 |

未激活时的容量上限：规则 100 条 / 数据源 100 个 / 用户 100 个。激活后不限。

激活方式见步骤 8。**没有授权也能正常用**——配规则、收告警、管账号都不受影响。

---

## 前置

| 项 | 说明 |
|---|---|
| 命名空间 | 下文用 `devops`，按实际改 |
| 数据库 | MySQL 8，**库要先手工建**，表由程序启动时自动迁移（当前 16 个迁移文件） |
| 镜像拉取 | 生产 Harbor 用现成的 `ops-harbor-login-secret`；直连 marks26 要另建 |
| 入口 | Ingress(nginx) 或 Istio VirtualService，**两者只开一个** |
| 监控 | ServiceMonitor / VMServiceScrape（可选） |
| 日志数据源 | Loki 或 Elasticsearch/OpenSearch，**部署后才配**，见步骤 8 |

---

## 步骤 1 · 建库和账号

```sql
CREATE DATABASE opsalert DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci;

-- 独立账号，只给这一个库的权限
CREATE USER 'alert_user'@'%' IDENTIFIED BY '换成真实密码';
GRANT ALL PRIVILEGES ON opsalert.* TO 'alert_user'@'%';
FLUSH PRIVILEGES;
```

> 账号需要 `CREATE TABLE` / `ALTER TABLE` 权限——迁移在应用启动时跑。
> 只给 DML 权限的话，Pod 会在迁移那步失败退出，日志里写着具体缺哪个权限。

验证账号能连：

```bash
kubectl -n devops run mysql-check --rm -it --restart=Never --image=mysql:8 -- \
  mysql -h<mysql地址> -ualert_user -p'密码' -e "SELECT 1" opsalert
```

---

## 步骤 2 · 建 Secret（所有敏感配置都在这）

```bash
kubectl -n devops create secret generic ops-alert-backend-secret \
  --from-literal=MYSQL_HOST='<主机>' \
  --from-literal=MYSQL_PORT='3306' \
  --from-literal=MYSQL_USER='alert_user' \
  --from-literal=MYSQL_PASSWORD='<密码>' \
  --from-literal=MYSQL_DATABASE='opsalert' \
  --from-literal=JWT_SECRET="$(openssl rand -hex 32)" \
  --from-literal=ALERT_AES_KEY="$(openssl rand -hex 16)" \
  --from-literal=ADMIN_PASSWORD='<初始管理员密码>'
```

也可以 `cp deploy/secret.example.yaml secret.yaml` 改完 apply——但 **secret.yaml 别提交进 git，apply 完就删**。

### 全部 8 个变量

| 键 | 必填 | 说明 |
|---|---|---|
| `MYSQL_HOST` | ✓ | 数据库主机 |
| `MYSQL_PORT` | ✓ | 通常 3306 |
| `MYSQL_USER` | ✓ | 步骤 1 建的账号 |
| `MYSQL_PASSWORD` | ✓ | 可以含任意字符（`@ # : /` 都行）——DSN 由后端用参数构造，不是字符串拼接 |
| `MYSQL_DATABASE` | ✓ | 步骤 1 建的库 |
| `JWT_SECRET` | ✓ | `openssl rand -hex 32`。⚠️ 换掉会让所有在线会话立即失效（所有人被登出）。这是它该有的行为，但别在业务高峰做 |
| `ALERT_AES_KEY` | ✓ | `openssl rand -hex 16`，**必须正好 32 个字符**。数据源凭据的加密钥匙 |
| `ADMIN_PASSWORD` | ✓ | 首个管理员的初始密码。**只在 admin 账号不存在时生效**，已存在的账号不会被它改回去 |

### 时区（在 values 里，不在 Secret）

`values-prod.yaml` 的 `backend.env` 里有 `TZ`，默认 `Asia/Shanghai`。

它影响的是**墙上时钟判定**：日报几点发、日报里「昨天」的边界。
界面上的时间显示**不看它**，走浏览器本地时区——在马尼拉值班的人该看到马尼拉时间。

- 必须是 IANA 名字（`Asia/Shanghai` / `Asia/Manila` / `Europe/London`）
- 🔴 **名字拼错时进程直接退出**，不会退回 UTC。退回 UTC 的后果是日报在错误的时刻发出去，而没有任何一处会说出来
- ⚠️ 选了**有夏令时**的时区时，启动日志会警告：MySQL 会话的偏移量是启动时算定的，夏令时切换后会与实际差一小时，需要重启修正。`Asia/Shanghai` 不过夏令时，默认配置没有这个问题

> 🔴 **`ALERT_AES_KEY` 换掉之后，已存的数据源凭据全部解不开。**
> 表现是**所有规则查询突然失败，而配置看着完全没动过**——没有任何一处会说"是密钥换了"。
> 轮换它必须连带重新填一遍所有数据源的凭据。

> ⚠️ 数据库拆成五个独立的键，**不要**自己拼成一行 DSN。
> 连接超时、读写超时、时区、字符集由后端的 `buildDSN` 统一负责——
> 漏掉 `readTimeout` 的后果是数据库卡住时连接池被占满，每个查库接口一起卡死，对外表现成网关 524。

---

## 步骤 3 · 确认镜像拉取密钥

`values-prod.yaml` 默认用生产 Harbor 现成的 `ops-harbor-login-secret`：

```bash
kubectl -n devops get secret ops-harbor-login-secret
```

直连 marks26（私有仓库）的话另建一个，并改 `global.imagePullSecrets`：

```bash
kubectl -n devops create secret docker-registry marks26-pull \
  --docker-server=https://index.docker.io/v1/ \
  --docker-username=marks26 --docker-password='<token>'
```

---

## 步骤 4 · 选入口：Ingress 还是 Istio

`values-prod.yaml` 默认 **Ingress(nginx) 开、Istio 关**。两者只开一个。

### 走 Ingress（默认）

改 `values-prod.yaml` 里两处域名：

```yaml
ingress:
  hosts:
    - host: alert.你的域名           # ← 改
  tls:
    - secretName: opsalert-tls
      hosts:
        - alert.你的域名             # ← 与上面保持一致
```

证书走 `cert-manager.io/cluster-issuer: letsencrypt-prod`。确认签发链路是通的：

```bash
kubectl get clusterissuer letsencrypt-prod
```

> ⚠️ UAT 集群曾因缺 `clouddns-dns01-solver-svc-acct` 导致证书签发断链卡了 13 个月。
> 上线前先确认这个 issuer 真的能签出证书，别等域名解析完了才发现。

### 走 Istio

```yaml
ingress:
  enabled: false
istio:
  enabled: true
  gateways:
    - istio-system/infra-istio-ingressgateway-inner   # ← 确认网关名
  hosts:
    - alert.你的域名
```

```bash
# 网关存在吗
kubectl -n istio-system get gateway
```

> 🔴 **域名必须落在网关 listener 的 hosts 通配范围内，否则 Istio 静默丢弃**：
> VirtualService 创建成功、`kubectl get vs` 一切正常、访问就是不通、**没有任何报错**。

> ⚠️ 跨 namespace 的网关必须写成 `<ns>/<gateway名>`。只写名字时 Istio 在本 namespace 找，
> 找不到就静默不生效——同样是对象创建成功、域名不通、没有报错。

---

## 步骤 5 · 监控采集（可选）

集群里有 kube-prometheus-stack 或 VictoriaMetrics 时打开：

```yaml
metrics:
  enabled: true
  scrapeKind: ServiceMonitor      # VM 集群填 VMServiceScrape
  interval: 30s
  labels:
    release: kube-prometheus-stack   # ← 必须匹配你集群的选择器
```

确认选择器：

```bash
# Prometheus Operator
kubectl get prometheus -A -o jsonpath='{.items[*].spec.serviceMonitorSelector}'; echo
# VictoriaMetrics
kubectl get vmagent -A -o jsonpath='{.items[*].spec.serviceScrapeSelector}'; echo
```

> `labels` 填错不会报错，只是**永远抓不到**——`metrics.enabled: true` 看着是开的，
> 而 Prometheus 里查不到任何 `opsalert_*` 指标。装完请按步骤 7 实际查一次。

---

## 步骤 6 · 安装

```bash
cd ops/ops-alert

helm upgrade --install opsalert ./deploy/helm \
  -n devops --create-namespace \
  -f ./deploy/helm/values-prod.yaml \
  --set global.tag=<版本> \
  --wait --timeout 10m
```

> 🔴 **是 `global.tag`，不是 `global.imageTag`。**
> 这个 chart 早期用的是后者，现在已与 ops-version / ops-cmdb 对齐。
> 传旧名字会**直接报错**（刻意的）——因为静默忽略的后果是回落到 Chart.AppVersion，
> 装出一个你根本没构建过的 tag。

> 前后端 tag 不一致时 helm 会在渲染期失败并说明原因。这道守卫是必要的：
> 版本错配不报错，只表现为某个接口 404 或字段缺失，最难排查。

### 生产规格（values-prod.yaml 已配好）

| | 副本 | HPA | PDB | requests | limits |
|---|---|---|---|---|---|
| frontend | 3 | 3→8 | minAvailable 2 | 50m / 64Mi | 200m / 192Mi |
| backend | 3 | 无 | minAvailable 2 | 200m / 512Mi | 1000m / 2Gi |

反亲和是 `required: true`——强制副本分散到不同节点。全落在同一节点时，那个节点一挂服务整体不可用，PDB 也救不了。

> backend 不配 HPA 是有意的：**检测引擎靠数据库租约选主**，只有持有租约的那个副本在跑规则。
> 扩副本增加的是 API 承载能力，不会让检测变快——按 CPU 自动扩容会扩出一堆只做冷备的 Pod。

---

## 步骤 7 · 验证

```bash
# ① 镜像 digest 与 registry 一致（防"同名 tag 装的是旧镜像"）
bash ../tooling/scripts/verify-deploy.sh ops-alert devops <版本>

# ② 迁移跑完了吗——应该到 016
kubectl -n devops logs deploy/opsalert-ops-alert-backend | grep migration | tail -3
# 期望看到：migration 016_oidc: OK

# ③ 健康
kubectl -n devops exec deploy/opsalert-ops-alert-backend -- wget -qO- http://127.0.0.1:8088/readyz
kubectl -n devops exec deploy/opsalert-ops-alert-backend -- wget -qO- http://127.0.0.1:8088/version
# 期望：与你装的版本一致

# ④ 时区解析结果（确认是你要的那个，不是默认值）
kubectl -n devops logs deploy/opsalert-ops-alert-backend | grep timezone | tail -1

# ⑤ 指标端点
kubectl -n devops exec deploy/opsalert-ops-alert-backend -- \
  wget -qO- http://127.0.0.1:8088/metrics | head -5

# ⑥ 登录（admin / 你在 ADMIN_PASSWORD 里设的密码）
curl -s https://alert.你的域名/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<ADMIN_PASSWORD>"}' | head -c 200
```

> 🔴 **`verify-deploy.sh` 这一步别跳。** 节点缓存了同名 tag 的旧镜像时
>（tag 被覆盖过 + `IfNotPresent`），界面上表现为"新功能一个都没有"，**没有任何报错**。
> 只有 digest 能证伪。真遇到了：`crictl rmi` 那份 + `kubectl rollout restart`。

### 三个迁移一定要跑到

| 迁移 | 内容 | 没跑到的后果 |
|---|---|---|
| `013_metrics_report` | 指标导出、日报两张表 + 权限种子 | **除 admin 外所有角色打开「日报」「日志检索」都是 403** |
| `014_license` | 授权与安装指纹 | 授权页打不开，无法激活 |
| `015_message_template` | 文案模板 + 内置模板种子 | 模板页空着；告警仍能正常发（回落到代码内兜底） |
| `016_oidc` | SSO 配置 | SSO 页打不开 |

权限种子漏了的话，**admin 自己测发现不了**（admin 靠 `unrestricted` 生效）。装完连库确认一次：

```sql
-- 期望 ≥ 7
SELECT COUNT(*) FROM role_permissions
WHERE perm_code LIKE '%report%' OR perm_code LIKE '%explore%';
-- 期望 2（两条内置文案模板，每个租户各一份）
SELECT name, is_builtin FROM message_templates WHERE is_builtin = 1;
```

> 🔴 **`014_license` 会 DROP 并重建 `licenses` 和 `install_identity` 两张表。**
> 这在当前是安全的——那两张表是 `001` 里预留的、从没有任何代码写过，实测 0 行。
> 但如果你的部署里 `licenses` 已经有数据（比如从更早的版本升上来），
> **不要直接升**：重建 `install_identity` 会换掉安装指纹，
> 已签发的授权全部对不上，表现是"好好的突然提示指纹不符"。那种情况先找我。

## 步骤 8 · 装完之后要配的东西

程序装上了不等于能告警。按顺序配：

### ⓪ 激活授权（可选，但企业版功能需要）

界面 → 平台 → 授权。

1. 复制页面上的**安装指纹**（完整一串，别截断——截断的指纹会签出一份永远对不上的授权）
2. 把指纹发给我们，拿到激活码
3. 粘贴进"激活"框

> ⚠️ 指纹由**数据库**派生（install_uuid + MySQL 的 server_id）。
> 换库、主从切换、把库恢复到新实例都会让它变，届时授权会进入 14 天宽限期，
> 需要重新签发。这不是 bug，它就是用来发现"这套安装被复制了"的。

> 激活后本副本立即生效，**其余副本在 20 秒内收敛**。
> 在另一个标签页（可能打到别的 Pod）看到还是未激活时，等一下再刷新，别反复重贴。

### ① 数据源（必须）

界面 → 接入 → 数据源 → 新增。一期支持 `loki` / `elasticsearch` / `opensearch`。

填完**一定要点「测试连接」**。不点的话，坏掉的数据源在没人点的那几天里
表现为「一直没有告警」——正是这个产品要根治的静默失明。

配好后在「系统自检」页确认结论是：
> 检测链路完整：此时「没有告警」可以理解为「确实没有问题」

### ② 通知渠道（必须）

界面 → 接入 → 通知渠道。飞书 / Webhook。同样**点「测试」确认能收到**。

### ③ 通知路由

界面 → 接入 → 通知路由。没有路由的话事件会走兜底路由；
可以先不配，但生产建议至少分出「紧急 → 值班手机」这一档。

### ④ 规则

两条路：

- **从零建**：界面 → 检测 → 场景模板。三个模板（错误码 / 日志心跳 / 耗时阈值）覆盖了你现有的 7 条生产规则形态。
- **从旧系统导入**：界面 → 检测 → 规则 → 导入。先跑「预检」，它会逐条告诉你哪些能直接搬、哪些要确认。

> ⚠️ 导入的规则**默认是停用的**（`enabled=0`）。这是刻意的——
> 一次导入几十条直接开跑，第一个周期就可能刷屏。逐条确认后再启用。

> ⚠️ 预检里带 `confirm` 的那些必须逐条看。特别是：
> **旧系统的指标名和这里不同**（新的是 `opsalert_rule_hits_total` 等），
> 依赖旧指标名的看板和二次告警要改写 PromQL——标签迁过来了不等于看板能直接用。

### ⑤ 告警文案模板（可选）

界面 → 接入 → 文案模板。

默认用内置文案，不配也能正常收告警。要改措辞、加字段、换成自己团队的叫法时来这里。

- 变量语法是 `{{name}}`（不是 `${name}`）。右侧列出全部可用变量，点一下插入
- **改完一定要点「预览」**——它拿一条真实事件渲染，能看出"这个变量在我的规则里其实取不到"
- 模板可以按渠道分（飞书是卡片、webhook 是任意 JSON，结构差异大时分开写）
- 在「通知渠道」里把模板绑到某个渠道上；不绑就用内置的

> 🔴 **模板写错不会让告警发不出去。** 系统会回落到内置文案照常发送，
> 并在消息里附一句"模板里有 N 个变量不存在"。一次文案手误变成一次静默失明，
> 是这套系统最不能出的故障。

> ⚠️ 内置模板不能改也不能删——它是渲染失败时的兜底，兜底必须永远可用。要改请复制一份。

### ⑥ 单点登录（可选）

界面 → 平台 → 单点登录。支持任意 OIDC 身份源（Azure AD / Keycloak / Okta / 自研）。

1. 页面顶部有**回调地址**，复制它，一字不差地填进身份源的应用配置
2. 填 issuer、client_id、client_secret
3. 点「测试连通性」
4. 保存后勾选「启用」，登录页就会出现 SSO 按钮

**几个必须知道的点：**

- 🔴 **回调地址差一个字符就被拒**（少个端口、http 写成 https），而身份源只回一句
  `redirect_uri mismatch`，不告诉你差在哪。直接复制页面上那一串，别手打
- 🔴 **反向代理必须转 `$http_host` 而不是 `$host`**——`$host` 不含端口，
  非标准端口的部署会算出一个少了端口的回调地址
- 🔴 **「字段来源」保持默认的「两处都取」**。有的身份源把 name/email
  只放在 userinfo 里不放进 id_token，只读一处会报 "Claim not found"，
  而身份源那侧看起来完全正常
- ⚠️ **授权端点要浏览器可达，令牌/userinfo 端点要后端可达——两者可以是不同地址。**
  这两端网络视角不同时，自动发现给的同一套地址就用不了，要关掉自动发现手填
- 🔴 **默认角色不能选管理员**（后端会拒绝）。给了的话，身份源里任何一个人
  登录一次就成了这套系统的管理员。用 viewer 打底，再通过群组映射给少数人更高权限
- ⚠️ **群组映射每次登录都重新套用**。人在身份源里被移出某个组之后，
  只有这样才会真的降权
- ⚠️ **同名的本地账号不会被 SSO 接管**。本地 admin 始终是逃生通道，
  登录页上的密码框不会因为开了 SSO 就消失
- ⚠️ 「测试连通性」**验不了 client_secret 和 claim 名对不对**，只验端点可达。
  真正的验证是登录一次——失败时的提示里会列出身份源实际下发了哪些字段

### ⑦ 日报（可选）

界面 → 战情 → 日报。开开关、选发送时刻和投递渠道，**点「立即试发」确认真能收到**。

> 日报一天只有一次验证机会。不试发的话，配错了要等到第二天早上才知道，然后再等一天。

---

## 步骤 9 · 给它自己配告警（强烈建议）

告警系统自己坏了是最危险的情况——它坏了的表现就是"很安静"。

```promql
# 🔴 最该配的一条：数据源不可达。
# 它为 0 时，依赖它的规则全部静默失效，而事件数会归零 ——
# 看板上和"一切正常"长得完全一样
opsalert_datasource_up == 0

# 规则连续执行失败：大于 0 时这条规则已经不产生告警了
opsalert_rule_consecutive_failures > 0

# 规则停止执行：上次执行时刻不再推进（超过 10 分钟没动）
time() - opsalert_rule_last_run_timestamp_seconds > 600

# 后端整个不见了
up{job=~".*ops-alert.*"} == 0
```

> `/metrics` 挂在**健康端口 8088**，不在业务端口——抓取端用不了登录态，
> 而业务端口在 Ingress 后面，放那儿等于把规则名和你填的静态标签暴露到公网。

> ⚠️ **`METRICS_TOKEN` 当前没配，`/metrics` 匿名可读。**
> 8088 只在集群内可达时这是安全的。**哪天把它通过 Ingress 或 NodePort 暴露出去之前，
> 必须先加这个变量到 Secret 里**（值随便一个长随机串），抓取端配 `Authorization: Bearer <值>`。

---

## 回滚

```bash
helm rollback opsalert -n devops
# 或指到上一个版本
helm upgrade opsalert ./deploy/helm -n devops \
  -f ./deploy/helm/values-prod.yaml --set global.tag=<上一个版本> --wait
```

> ⚠️ **迁移不会自动回退。** 013 只做加列加表，对旧版本后端是兼容的
>（旧代码不认识新列，但不会因此报错），所以回滚到 v0.7.x 是安全的。
> 再往前回滚要先确认那个版本的迁移基线。

> ⚠️ 前后端必须一起回滚——它们是同一个 chart、同一个 tag。

---

## 日常运维

```bash
# 看后端日志（结构化 JSON，一行一事件）
kubectl -n devops logs -f deploy/opsalert-ops-alert-backend

# 只看规则执行失败
kubectl -n devops logs deploy/opsalert-ops-alert-backend | grep rule_failed

# 只看投递失败
kubectl -n devops logs deploy/opsalert-ops-alert-backend | grep notify

# 改了 Secret（比如轮换密码）之后必须重启才生效
kubectl -n devops rollout restart deploy/opsalert-ops-alert-backend
```

> ⚠️ `helm upgrade` **不会因为 Secret 内容变了就重建 Pod**。
> 改完 Secret 一定要手动 `rollout restart`，否则改动看着生效了实际没生效。

---

## 已知限制（v0.9.8）

| 项 | 状态 |
|---|---|
| 告警认领 | 接口和权限码保留，界面无入口（你定的"先不做"） |
| 值班排班 | 未做，升级链指向联系人组 |
| 通知渠道 | 只有**飞书**和**通用 Webhook**。邮件 / Teams / Telegram / WhatsApp 都还没做 |
| 指标型数据源 | Prometheus / VictoriaMetrics / ClickHouse / MySQL 属二期。选了会明确报"尚未实现"，不会静默失败 |
| 指标阈值 / SLO 规则 | 同上，二期 |
| 多租户 / 品牌定制 | 授权里含着但这版没实现。授权页会显示"授权已含，本版未实现"，不会让人误以为是没买 |
| ES 多节点 | 填多个地址时只用第一个，没有健康探测与摘除 |
| 窄屏侧栏 | 390px 下不自动收起，内容区只剩约 175px（共享外壳的行为） |
| `METRICS_TOKEN` | 未配置时 `/metrics` 匿名可读。健康端口只在集群内可达时安全，见步骤 9 |

### 🔴 一处已知的显示不一致，等你定夺

授权页把 **`advanced_detect`（高级检测）** 和 **`multi_datasource`（多数据源）**
显示成"需要授权"，但它们**实际没有门控**——未激活也能建 `log_spike` /
`log_field_threshold` 规则，也能加多个数据源（实测确认）。

两个原因：`multi_datasource` 的上限已按要求放宽到 100，事实上不再是付费项；
`advanced_detect` 从来没接过门控。

这不影响使用（只多不少），但**界面说的和实际不一致**，正是这套系统要根治的那类问题。
处理方式（收费 / 不收费）待定，定了之后我改一版。

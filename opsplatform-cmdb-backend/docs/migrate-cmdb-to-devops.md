# CMDB 迁移到 devops：数据库迁移 + 新域名双跑验证 + 切换

集群 `infra-k8s-cluster-01`（cluster_id=3，GKE csc5002-infra / asia-east2）。

**进度**：MySQL 已就绪（`devops/opsplatform-mysql-0` Running）✅ → 镜像已推 harbor ✅ → **本篇：数据库 + 部署 + 灰度验证 + 切换**。

**策略**（按你的思路）：devops 那套挂**新域名**先跑起来，cesar 那套挂旧域名继续服务；对比验证通过后，把旧域名切到 devops。

因为要双跑，数据库要 **dump 两次**，原因见第一节。

---

## 零、双跑的头号风险：证书自动续期会打架

这个必须在启动 devops 那套之前处理掉。

`main.go:44` 起的调度器里有个 `auto_renew` → `renewDue(db, cipher)`（`handlers/scheduler.go:65`），这是**真正的写操作**：向 Let's Encrypt 申请证书，并往域名 zone 里写 `_acme-challenge` TXT 记录。

两套 CMDB 同时跑会出事：

- 两边用**同一套 DNS 凭证**、对**同一批域名**发起 ACME challenge，往同一个 zone 写 `_acme-challenge` TXT，互相覆盖和清理对方的记录 → **双方都验证失败**
- 反复失败会撞 Let's Encrypt 的 failed-validation rate limit，撞了要等，本来能正常续的证书续不下来
- devops 的库是从 cesar dump 来的，`scheduled_tasks` 配置一模一样：`auto_renew` 每天 `0 3 * * *`、`remind` `0 9 * * *`、`refresh_expiry` `0 3,15 * * *` —— **两套在完全相同的时间点触发**，冲突概率拉满

另外 `remind`（到期提醒推送）双跑会**给所有人发两份通知**。

`main.go` 里四个调度器（`:44` 主调度、`:104` K8s 采集、`:106` 成本快照、`:108` CDN）都是无条件 `go` 启动的，**没有任何环境变量开关**。所以只能从库里禁用 —— 见步骤 3，且**必须在 Pod 启动之前做完**。

> K8s 采集（`:104`）不走 `scheduled_tasks` 表，会照常跑。这个无所谓，它是只读刷快照，正好用来验证采集功能是否正常。

---

## 一、为什么要 dump 两次

- **dump #1**：现在做，给 devops 灌一份数据用于验证。验证期间 cesar 还在正常服务，用户可能改接入配置、加域名、跑任务，两边数据会逐渐分叉。
- **dump #2**：正式切换那一刻做，停掉 cesar 后端再 dump 一次覆盖导入，保证**零分叉**。

只 dump 一次的话，验证期内旧系统上的所有变更都会丢。

---

## 二、阶段一：部署 + 新域名跑起来

### 步骤 1：dump #1

```bash
mkdir -p ~/cmdb-migrate && cd ~/cmdb-migrate

kubectl -n cesar exec opsplatform-mysql-0 -- \
  mysqldump -uroot -p'<cesar_root密码>' \
  --single-transaction --routines --triggers --events \
  --set-gtid-purged=OFF --default-character-set=utf8mb4 \
  cmdb > cmdb-dump1.sql

ls -lh cmdb-dump1.sql        # 确认不是 0 字节
head -5 cmdb-dump1.sql       # 确认不是一行报错信息
```

> cesar 的 MySQL 是 6 个 opsplatform 子系统共享的（主平台/alert/confluence/deploy/cmdb），**只能 dump `cmdb` 单库**，那个实例全程不能停。这次是热 dump，cesar 的 CMDB 不用停 —— `--single-transaction` 保证 InnoDB 快照一致，验证用足够了。

### 步骤 2：导入 devops

```bash
# 确保是干净的库（之前 PVC 反复重建过，先确认没残留）
kubectl -n devops exec opsplatform-mysql-0 -- \
  mysql -uroot -p'<新root密码>' -e "SHOW DATABASES;"

kubectl -n devops exec -i opsplatform-mysql-0 -- \
  mysql -uroot -p'<新root密码>' cmdb < cmdb-dump1.sql
```

校验（两边数字必须一致）：

```bash
for X in "cesar <cesar_root密码>" "devops <新root密码>"; do
  set -- $X
  echo "== $1"
  kubectl -n $1 exec opsplatform-mysql-0 -- mysql -uroot -p"$2" -N -e "
    SELECT 'tables',       COUNT(*) FROM information_schema.tables WHERE table_schema='cmdb'
    UNION ALL SELECT 'migrations',   COUNT(*) FROM cmdb.schema_migrations
    UNION ALL SELECT 'k8s_clusters', COUNT(*) FROM cmdb.k8s_clusters
    UNION ALL SELECT 'domains',      COUNT(*) FROM cmdb.domains
    UNION ALL SELECT 'certificates', COUNT(*) FROM cmdb.certificates
    UNION ALL SELECT 'users',        COUNT(*) FROM cmdb.users;" 2>/dev/null
done
```

`schema_migrations` 应该是 64 行（当前迁移文件数）。数字对不上就别往下走。

### 步骤 3：禁用 devops 侧的定时任务（在部署之前！）

```bash
kubectl -n devops exec -i opsplatform-mysql-0 -- mysql -uroot -p'<新root密码>' <<'SQL'
USE cmdb;
-- 备份原始 enabled 状态，切换时要按它恢复
CREATE TABLE scheduled_tasks_bak AS SELECT task_key, enabled FROM scheduled_tasks;
UPDATE scheduled_tasks SET enabled=0;
SELECT task_key, name, enabled, schedule FROM scheduled_tasks;
SQL
```

输出里 `enabled` 必须全是 0，尤其 `auto_renew`。

> 顺序很重要：Pod 启动时 `sched.reload()` 会读表注册 cron。先起 Pod 再改表的话，得调用 reload API 或重启 Pod 才生效。

### 步骤 4：建 Secret

```bash
kubectl -n cesar get secret opsplatform-cmdb-secret -o yaml > cmdb-secret.yaml
sed 's/namespace: cesar/namespace: devops/' cmdb-secret.yaml \
  | grep -vE 'uid:|resourceVersion:|creationTimestamp:|selfLink:' > new-secret.yaml
```

编辑 `new-secret.yaml`（`data` 是 base64，整段换成 `stringData` 写明文更省事），只改两项：

| 字段 | 值 |
|---|---|
| `MYSQL_HOST` | `opsplatform-mysql.devops.svc.cluster.local` |
| `MYSQL_PASSWORD` | devops 侧 cmdb_user 的密码 |
| `MYSQL_PORT` / `MYSQL_DATABASE` | 照旧 |
| `CMDB_AES_KEY` / `JWT_SECRET` / `ADMIN_PASSWORD` | **原样，一个字符不动** |

`CMDB_AES_KEY` 换了的话，集群 token、Harbor、CDN、注册商凭证全部解不开，而且 `/ready` 和登录都正常，要到实际调用才报错。

```bash
kubectl apply -f new-secret.yaml
```

### 步骤 5：部署前后端

```bash
kubectl -n cesar get deploy opsplatform-cmdb-backend opsplatform-cmdb-frontend -o yaml > cmdb-deploy.yaml
kubectl -n cesar get svc    opsplatform-cmdb-backend opsplatform-cmdb-frontend -o yaml > cmdb-svc.yaml
kubectl -n cesar get cm     opsplatform-cmdb-frontend-nginx -o yaml > cmdb-cm.yaml

sed -e 's/namespace: cesar/namespace: devops/' \
    -e 's#marks26/opsplatform-cmdb-#harbor.slileisure.com/devops/opsplatform-cmdb-#' \
    cmdb-deploy.yaml > new-deploy.yaml
sed 's/namespace: cesar/namespace: devops/' cmdb-svc.yaml > new-svc.yaml
sed 's/namespace: cesar/namespace: devops/' cmdb-cm.yaml  > new-cm.yaml
```

手工再改 `new-deploy.yaml`：

1. 清掉 `status:` / `uid` / `resourceVersion` / `creationTimestamp` / `deployment.kubernetes.io/revision`
2. 两个 Deployment 的 `spec.template.spec` 补上 imagePullSecrets（换 harbor 后必须有，否则复现你 MySQL 那个 `no basic auth credentials`）：
   ```bash
   kubectl -n devops get deploy cmdb-backend -o jsonpath='{.spec.template.spec.imagePullSecrets}'; echo
   ```
   ```yaml
   imagePullSecrets:
     - name: <上面查到的名字>
   ```

```bash
kubectl apply -f new-cm.yaml -f new-svc.yaml -f new-deploy.yaml
kubectl -n devops rollout status deploy/opsplatform-cmdb-backend
kubectl -n devops logs deploy/opsplatform-cmdb-backend --tail=80
```

日志重点看：连库成功、migration 显示已最新（不是从 001 重跑）、scheduler 注册的任务数为 0。

### 步骤 6：新域名 VirtualService

先确认对外网关的证书覆盖新域名（CMDB 没采到 Istio Gateway，这条得手工查）：

```bash
kubectl -n istio-system get gateway infra-istio-ingressgateway-extra -o yaml | grep -A6 tls
```

新域名如果不在现有证书的 SAN 里（也不被通配符覆盖），要先签发证书挂上去，否则 https 打不开。

新建 VS（**新名字、新域名，不要动 cesar 的两条**）：

```yaml
# new-domain-vs.yaml
apiVersion: networking.istio.io/v1beta1
kind: VirtualService
metadata:
  name: opsplatform-cmdb-frontend-devops
  namespace: devops
spec:
  hosts:
    - <你的新域名>
  gateways:
    - istio-system/infra-istio-ingressgateway-extra    # 对外
  http:
    - route:
        - destination:
            host: opsplatform-cmdb-frontend.devops.svc.cluster.local
            port: { number: 80 }
```

```bash
kubectl apply -f new-domain-vs.yaml
kubectl -n devops get vs
```

**到这里两套并行跑**：旧域名 → cesar（正常服务），新域名 → devops（待验证）。用户无感。

---

## 三、对比验证

新域名和证书就绪后告诉我，我用两个域名各跑一遍接口做数据对比。需要你给我：

- 新域名
- 一个可用账号（或者你自己登录后把两边的 JWT 给我）

我会对比这几组：集群列表与各集群资源数、域名/证书数量与到期时间、CDN 站点与解析记录、主机、Harbor 项目、成本概览，逐项列差异。

你自己先过的手工清单：

1. `kubectl -n devops exec deploy/opsplatform-cmdb-backend -- wget -qO- localhost:8088/ready`
2. 新域名能打开、能登录（验证 `JWT_SECRET` + users 表）
3. **接入管理页把 5 个集群 + Harbor + CDN + 域名注册商配置逐个点开** —— 这是验证 `CMDB_AES_KEY` 的唯一可靠方式，能正常显示不报解密失败才算过
4. 手动触发一次 K8s 全量同步，执行记录成功，资源数与旧系统一致
5. 定时任务页确认**全部是禁用状态**（这是故意的，别手贱开）
6. 验证期间**不要点 `auto_renew` 的「立即运行」** —— 会绕过 enabled 直接跑，跟 cesar 那套撞车

> 验证期间尽量别在新系统里改配置，那些改动会被 dump #2 覆盖掉。

---

## 四、阶段二：正式切换

验证通过后执行。停机窗口约 10~15 分钟。

### 步骤 1：停 cesar 后端（停机开始）

```bash
kubectl -n cesar scale deploy/opsplatform-cmdb-backend --replicas=0
kubectl -n cesar get pod -l app=opsplatform-cmdb-backend    # 确认没有 Running
```

### 步骤 2：dump #2，覆盖导入

```bash
cd ~/cmdb-migrate
kubectl -n cesar exec opsplatform-mysql-0 -- \
  mysqldump -uroot -p'<cesar_root密码>' \
  --single-transaction --routines --triggers --events \
  --set-gtid-purged=OFF --default-character-set=utf8mb4 \
  cmdb > cmdb-dump2.sql

ls -lh cmdb-dump2.sql

# 先停 devops 后端，避免导入过程中它在写
kubectl -n devops scale deploy/opsplatform-cmdb-backend --replicas=0

# 整库重建再导，避免验证期残留数据
kubectl -n devops exec -i opsplatform-mysql-0 -- mysql -uroot -p'<新root密码>' <<'SQL'
DROP DATABASE cmdb;
CREATE DATABASE cmdb DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci;
SQL

kubectl -n devops exec -i opsplatform-mysql-0 -- \
  mysql -uroot -p'<新root密码>' cmdb < cmdb-dump2.sql
```

> `DROP DATABASE` 会把步骤 3 建的 `scheduled_tasks_bak` 一起删掉，没关系 —— dump #2 里的 `scheduled_tasks` 就是 cesar 的原始状态，直接就是对的，不用再恢复。

### 步骤 3：起新后端

```bash
kubectl -n devops scale deploy/opsplatform-cmdb-backend --replicas=1
kubectl -n devops rollout status deploy/opsplatform-cmdb-backend
kubectl -n devops exec -i opsplatform-mysql-0 -- \
  mysql -uroot -p'<新root密码>' -e "SELECT task_key,enabled,schedule FROM cmdb.scheduled_tasks;"
```

这次 `enabled` 应该恢复成 cesar 的原始值（`auto_renew`=1 等）。此刻 cesar 后端已停，**只有一套在跑定时任务**，不会打架。

### 步骤 4：切旧域名

两条旧 VS 都要搬，VS 必须和 Service 同 ns，所以是删旧建新：

| VS 名 | 域名 | Gateway |
|---|---|---|
| opsplatform-cmdb-frontend | `opsplatform-cmdb.slileisure.com` | istio-system/infra-istio-ingressgateway-inner |
| opsplatform-cmdb-frontend-w | `cmdb-w.sl-devops.com` | istio-system/infra-istio-ingressgateway-extra |

```bash
kubectl -n cesar get vs opsplatform-cmdb-frontend opsplatform-cmdb-frontend-w -o yaml > cmdb-vs.yaml

sed -e 's/namespace: cesar/namespace: devops/' \
    -e 's/opsplatform-cmdb-frontend\.cesar\.svc\.cluster\.local/opsplatform-cmdb-frontend.devops.svc.cluster.local/' \
    cmdb-vs.yaml | grep -vE 'uid:|resourceVersion:|creationTimestamp:' > new-vs.yaml

kubectl -n cesar delete vs opsplatform-cmdb-frontend opsplatform-cmdb-frontend-w
kubectl apply -f new-vs.yaml
kubectl -n devops get vs | grep cmdb
```

断流就是这两条命令之间的几秒。Gateway 在 `istio-system` 不用动，DNS 不用改。

### 步骤 5：停 cesar 前端

```bash
kubectl -n cesar scale deploy/opsplatform-cmdb-frontend --replicas=0
```

---

## 五、切换后复验

1. `https://opsplatform-cmdb.slileisure.com`（inner 链路）
2. `https://cmdb-w.sl-devops.com`（extra 链路，与 inner 是两条独立链路，必须单独验）
3. 新域名也还能访问（三条 VS 并存，不冲突）
4. 定时任务页确认全部恢复启用，`auto_renew` 是 1
5. 等到次日 03:00 后确认 `auto_renew` 正常执行过一次（任务执行记录里看）
6. 等一个 K8s 采集周期，确认自动同步跑起来了
7. 证书 bundle 接口用旧 token 还能拉：`/api/certs/<id>/bundle?token=…`
8. Playwright 打开页面确认渲染 + curl 校验接口数据

顺手改掉这些指向：AI 助手（opsplatform-ai-backend）里配的 CMDB MCP 地址、本机 `.mcp.json`。infra 集群里没有证书拉取 CronJob（已查），其他集群若部了 `k8s-cert-cronjob.yaml`，检查 `CMDB_URL` —— 写域名的不用改，写 `xxx.cesar.svc.cluster.local` 的必须改。

---

## 六、回滚

阶段二任何一步不对：

```bash
kubectl -n devops scale deploy/opsplatform-cmdb-backend --replicas=0
kubectl -n cesar  scale deploy/opsplatform-cmdb-backend --replicas=1
kubectl -n cesar  scale deploy/opsplatform-cmdb-frontend --replicas=1
kubectl -n devops delete vs opsplatform-cmdb-frontend opsplatform-cmdb-frontend-w
kubectl apply -f ~/cmdb-migrate/cmdb-vs.yaml     # 恢复到 cesar
```

cesar 的库从阶段二步骤 1 起就没有写入了，回滚不丢数据。

---

## 七、收尾

| 时间 | 动作 |
|---|---|
| T+0 | cesar 的 cmdb 前后端 replicas=0，Deployment 定义保留 |
| T+3 天 | 确认采集、证书续期（跨过一次 03:00）、通知推送、AI MCP 调用全部正常 |
| T+7 天 | 删 cesar 的 cmdb Deployment/Service/Secret/ConfigMap；在 cesar 的 MySQL 上 `DROP DATABASE cmdb` —— **只删这一个库，另外 5 个 opsplatform 子系统还在用那个实例**；决定新域名保留还是删掉 |
| 长期 | `cmdb-dump2.sql` 归档留存 |

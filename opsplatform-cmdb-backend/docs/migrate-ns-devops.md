# CMDB 从 cesar 迁移到 devops 命名空间（infra-k8s-cluster-01）

现状数据来自 CMDB 实采（集群 `infra-k8s-cluster-01`，cluster_id=3，GKE `csc5002-infra` / asia-east2），采集时间 2026-07-29。

---

## 一、实际现状

### 源：`cesar` 命名空间

`cesar` 里跑的不只是 CMDB，是**整套 opsplatform**，共用一个 MySQL：

| 组件 | 镜像 | 副本 |
|---|---|---|
| opsplatform-backend | marks26/opsplatform-backend:v765 | 2/2 |
| opsplatform-frontend | marks26/opsplatform-frontend-vue:v614 | 2/2 |
| **opsplatform-cmdb-backend** | **marks26/opsplatform-cmdb-backend:v120** | **1/1** |
| **opsplatform-cmdb-frontend** | **marks26/opsplatform-cmdb-frontend:v137** | **1/1** |
| opsplatform-alert-backend / frontend | marks26/…:v107 / v86 | 1/1 |
| opsplatform-confluence-backend / frontend | marks26/…:v69 / v94 | 1/1 |
| opsplatform-deploy-backend / frontend | marks26/…:v176 / v178 | 1/1 |
| opsplatform-grafana-renderer / minio | — | 1/1 |
| **opsplatform-mysql** (StatefulSet) | bitnami/mysql:8.4.2-debian-12-r4 | 1/1 |
| opsplatform-redis (StatefulSet) | bitnami/redis | 1/1 |

- MySQL：Service `opsplatform-mysql:3306`（ClusterIP）+ headless，PVC `data-opsplatform-mysql-0` 10Gi premium-rwo
- CMDB Service：`opsplatform-cmdb-backend`（8080/8088）、`opsplatform-cmdb-frontend`（80），均 ClusterIP
- 入口是 **Istio VirtualService，没有 Ingress**，CMDB 前端挂了两条：

| VS 名 | 域名 | Gateway |
|---|---|---|
| opsplatform-cmdb-frontend | `opsplatform-cmdb.slileisure.com` | istio-system/infra-istio-ingressgateway-**inner** |
| opsplatform-cmdb-frontend-w | `cmdb-w.sl-devops.com` | istio-system/infra-istio-ingressgateway-**extra** |

### 目标：`devops` 命名空间（同集群）

- **`opsplatform-mysql` StatefulSet 已经建好了**：镜像 `harbor.slileisure.com/devops/mysql:8.4.2-debian-12-r4`，PVC `data-opsplatform-mysql-0` 10Gi premium-rwo 已 Bound，Service `devops-mysql` 之外单独存在。Pod `opsplatform-mysql-0` 今天 14:18:21 重建，此前一路 `ImagePullBackOff`（`no basic auth credentials`），14:18:44 拉取成功，容器已 Started。
- devops ns 里**已存在另一套完全不同的 CMDB**（不是你的）：`cmdb-backend` / `cmdb-frontend` / `cmdb-backend-worker` / `cmdb-backend-cert-worker`（`harbor.slileisure.com/devops/cmdb-backend:v1.12-2`）+ `cmdb-postgres` + `cmdb-redis`，域名 `cmdb.slileisure.com`。
- devops ns 还有：harbor 全套、doris、nacos、kafka-ui、ticketdesk、vaultwarden、devops-mysql。

---

## 二、这次迁移的 5 个真实风险点

1. **MySQL 是 6 个子系统共享的，只能单库 dump。** `cesar/opsplatform-mysql` 同时服务主平台、alert、confluence、deploy、cmdb。不能整实例搬，只能 `mysqldump` CMDB 那一个 database，且 **cesar 的 MySQL 迁移后绝对不能停**。

2. **`CMDB_AES_KEY` 必须原样带过去。** 集群 kubeconfig/token、Harbor、CDN、域名注册商、观测端点凭证全部用它加密后存库（`crypto/aes.go`）。换 key = 所有接入凭证解不开，而且要到实际调用时才报错，验证阶段容易漏掉。

3. **RBAC 不用动。** CMDB 纳管的 5 个集群没有一个是 `in-cluster` 接入方式：cluster 2 走 kubeconfig，cluster 1/3/4/5 全是 `provider=gke` + `cloud_account_id=1`(skttech23)，走云账号 SA key 换 token（`k8ssource/pool.go:68`）。ClusterRoleBinding `cmdb-k8s-readonly-gcp-sa` 绑的是 GCP SA User，与 Pod 所在 ns 无关 —— 换 ns 不影响采集。

4. **devops ns 拉镜像有前科。** 新 MySQL 刚才卡了 11 分钟的 `no basic auth credentials`。你的 CMDB 镜像在 `marks26/*`（Docker Hub），而 devops ns 其他服务都走 `harbor.slileisure.com`。建议迁移前先把镜像同步到 harbor，与 ns 内其他服务保持一致，避免又踩拉取凭证。

5. **同 ns 两套 CMDB。** 好消息是名字和域名都不撞（`opsplatform-cmdb-*` vs `cmdb-*`，`opsplatform-cmdb.slileisure.com` vs `cmdb.slileisure.com`），apply 不会冲突。坏消息是以后 `kubectl -n devops get pod | grep cmdb` 会出来两套，删错东西的风险实打实存在，操作时务必带全名。

---

## 三、动手前先取这 3 个值

```bash
# 1. CMDB 现在的库名/账号/密码 + 密钥（迁移全靠它）
kubectl -n cesar get secret opsplatform-cmdb-secret -o json \
  | jq -r '.data | map_values(@base64d)'

# 2. cesar MySQL 的 root 密码
kubectl -n cesar get secret opsplatform-mysql -o jsonpath='{.data.mysql-root-password}' | base64 -d

# 3. devops 新 MySQL 的 root 密码 + 确认真的起来了
kubectl -n devops get pod opsplatform-mysql-0
kubectl -n devops get secret opsplatform-mysql -o jsonpath='{.data.mysql-root-password}' | base64 -d
```

把第 1 步查到的 `MYSQL_DATABASE`（本地默认是 `cmdb`）记为 `$DB`，下文直接用。

---

## 四、迁移步骤

### 步骤 0：确认新 MySQL 健康 + PVC 是干净的

新 MySQL 的 PVC 之前建过、Pod 反复重建过，先确认里面没有残留：

```bash
kubectl -n devops exec -it opsplatform-mysql-0 -- \
  mysql -uroot -p<新root密码> -e "SHOW DATABASES;"
```

只该看到 `information_schema/mysql/performance_schema/sys`。若有残留的 `cmdb` 库，先 `DROP DATABASE cmdb;` 再往下走。

### 步骤 1：建库和账号

```bash
kubectl -n devops exec -it opsplatform-mysql-0 -- mysql -uroot -p<新root密码> <<'SQL'
CREATE DATABASE cmdb DEFAULT CHARSET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'cmdb_user'@'%' IDENTIFIED BY '<新密码>';
GRANT ALL PRIVILEGES ON cmdb.* TO 'cmdb_user'@'%';
FLUSH PRIVILEGES;
SQL
```

### 步骤 2：镜像同步到 harbor（建议做，规避拉取凭证问题）

```bash
docker pull marks26/opsplatform-cmdb-backend:v120
docker pull marks26/opsplatform-cmdb-frontend:v137
docker tag marks26/opsplatform-cmdb-backend:v120  harbor.slileisure.com/devops/opsplatform-cmdb-backend:v120
docker tag marks26/opsplatform-cmdb-frontend:v137 harbor.slileisure.com/devops/opsplatform-cmdb-frontend:v137
docker push harbor.slileisure.com/devops/opsplatform-cmdb-backend:v120
docker push harbor.slileisure.com/devops/opsplatform-cmdb-frontend:v137
```

> 本次**平迁不升级**。本地已经到 backend v124 / frontend v139，但迁移和升版本一起做会让排错变成猜谜。等 devops 环境跑稳了再单独发版。

### 步骤 3：导出旧 ns 清单留底

```bash
mkdir -p ~/cmdb-migrate && cd ~/cmdb-migrate
kubectl -n cesar get deploy opsplatform-cmdb-backend opsplatform-cmdb-frontend -o yaml > cmdb-deploy.yaml
kubectl -n cesar get svc opsplatform-cmdb-backend opsplatform-cmdb-frontend -o yaml > cmdb-svc.yaml
kubectl -n cesar get cm opsplatform-cmdb-frontend-nginx -o yaml > cmdb-cm.yaml
kubectl -n cesar get secret opsplatform-cmdb-secret -o yaml > cmdb-secret.yaml
kubectl -n cesar get vs opsplatform-cmdb-frontend opsplatform-cmdb-frontend-w -o yaml > cmdb-vs.yaml
```

### 步骤 4：停写（停机窗口开始）

```bash
kubectl -n cesar scale deploy/opsplatform-cmdb-backend --replicas=0
kubectl -n cesar get pod -l app=opsplatform-cmdb-backend    # 确认已经没有 Running
```

前端先不停，留着显示旧数据，切流量时再一起换。

### 步骤 5：单库 dump + 导入

```bash
# 从 cesar 的 MySQL Pod 里导出 cmdb 单库
kubectl -n cesar exec opsplatform-mysql-0 -- \
  mysqldump -uroot -p<cesar_root密码> \
  --single-transaction --routines --triggers --events \
  --set-gtid-purged=OFF --default-character-set=utf8mb4 \
  cmdb > cmdb-20260729.sql

ls -lh cmdb-20260729.sql     # 确认不是 0 字节

# 导入 devops 的新 MySQL
kubectl -n devops exec -i opsplatform-mysql-0 -- \
  mysql -uroot -p<新root密码> cmdb < cmdb-20260729.sql
```

校验（两边数字必须一致）：

```bash
for NS_POD in "cesar opsplatform-mysql-0 <cesar_root密码>" "devops opsplatform-mysql-0 <新root密码>"; do
  set -- $NS_POD
  echo "== $1"
  kubectl -n $1 exec $2 -- mysql -uroot -p$3 -N -e "
    SELECT 'tables', COUNT(*) FROM information_schema.tables WHERE table_schema='cmdb'
    UNION ALL SELECT 'migrations', COUNT(*) FROM cmdb.schema_migrations
    UNION ALL SELECT 'k8s_clusters', COUNT(*) FROM cmdb.k8s_clusters
    UNION ALL SELECT 'users', COUNT(*) FROM cmdb.users;" 2>/dev/null
done
```

`schema_migrations` 必须一起过去（全库 dump 默认包含，当前 64 个迁移文件），否则新 Pod 启动会把迁移重跑一遍。runner 幂等，但没必要冒这个险。

### 步骤 6：在 devops 建 Secret

拿旧的改，**只改数据库两项，密钥项一个字符都不动**：

```bash
sed -e 's/namespace: cesar/namespace: devops/' cmdb-secret.yaml \
  | grep -v -E 'uid:|resourceVersion:|creationTimestamp:|selfLink:' > new-secret.yaml
```

编辑 `new-secret.yaml`（注意 `data` 是 base64，改成 `stringData` 明文更省事）：
- `MYSQL_HOST` → `opsplatform-mysql.devops.svc.cluster.local`
- `MYSQL_PASSWORD` → 步骤 1 设的新密码
- `MYSQL_PORT` / `MYSQL_DATABASE` → 照旧
- `CMDB_AES_KEY` / `JWT_SECRET` / `ADMIN_PASSWORD` → **原样**

```bash
kubectl apply -f new-secret.yaml
```

### 步骤 7：部署前后端

```bash
sed -e 's/namespace: cesar/namespace: devops/' \
    -e 's#marks26/opsplatform-cmdb-#harbor.slileisure.com/devops/opsplatform-cmdb-#' \
    cmdb-deploy.yaml > new-deploy.yaml
sed 's/namespace: cesar/namespace: devops/' cmdb-svc.yaml > new-svc.yaml
sed 's/namespace: cesar/namespace: devops/' cmdb-cm.yaml  > new-cm.yaml
# 三个文件都清掉 status/uid/resourceVersion/creationTimestamp
kubectl apply -f new-cm.yaml -f new-svc.yaml -f new-deploy.yaml
kubectl -n devops rollout status deploy/opsplatform-cmdb-backend
kubectl -n devops logs deploy/opsplatform-cmdb-backend | tail -50
```

前端 nginx 里的 `proxy_pass http://opsplatform-cmdb-backend:8080` 是同 ns 短名，跟着搬不用改。

**先不切流量**，在这里就地验证：

```bash
kubectl -n devops port-forward deploy/opsplatform-cmdb-frontend 18080:80
# 浏览器开 http://localhost:18080 走一遍下面的验证清单
```

### 步骤 8：切 Istio 流量

两条 VS 都要搬。**VirtualService 必须和它引用的 Service 同 ns**，所以是删旧建新，不是并存：

```bash
sed -e 's/namespace: cesar/namespace: devops/' \
    -e 's/opsplatform-cmdb-frontend.cesar.svc.cluster.local/opsplatform-cmdb-frontend.devops.svc.cluster.local/' \
    cmdb-vs.yaml > new-vs.yaml

kubectl -n cesar delete vs opsplatform-cmdb-frontend opsplatform-cmdb-frontend-w
kubectl apply -f new-vs.yaml
kubectl -n devops get vs | grep cmdb
```

Gateway 在 `istio-system`（inner + extra），不用动。域名不变，DNS 不用改。

### 步骤 9：清理与观察

```bash
# 前端也停掉，但保留 Deployment 定义随时能拉回来
kubectl -n cesar scale deploy/opsplatform-cmdb-frontend --replicas=0
```

cesar 的 MySQL、Redis 和其他 5 个 opsplatform 子系统**保持原样不动**。

---

## 五、验证清单

1. `kubectl -n devops exec deploy/opsplatform-cmdb-backend -- wget -qO- localhost:8088/ready`
2. 启动日志里 migration 没有重跑：`kubectl -n devops logs deploy/opsplatform-cmdb-backend | grep -i migration`
3. `https://opsplatform-cmdb.slileisure.com` 能登录（验证 `JWT_SECRET` + users 表）
4. **接入管理页把 5 个集群、Harbor、CDN、域名注册商配置逐个点开** —— 这是验证 `CMDB_AES_KEY` 生效的唯一可靠方式，能正常显示不报解密失败才算过
5. 手动触发一次全量同步，执行记录成功，资源数与迁移前一致
6. 等一个采集周期，确认定时任务自动跑起来了
7. 证书模块：`/api/certs/<id>/bundle?token=…` 用旧 token 能拉到证书
8. 外网入口 `https://cmdb-w.sl-devops.com` 也验一遍（走的是 extra gateway，与 inner 是两条链路）
9. Playwright 打开页面确认渲染 + curl 校验接口数据

> infra 集群里没有证书拉取 CronJob（已查，只有 fleet/rancher/logging 四个无关的）。如果别的集群部了 `deploy-scripts/k8s-cert-cronjob.yaml`，检查它的 `CMDB_URL`：写域名的不用改，写 `…cesar.svc.cluster.local` 的必须改成 devops。同样要检查 AI 助手（opsplatform-ai-backend）配的 CMDB MCP 地址、本机 `.mcp.json`。

---

## 六、回滚

新环境不行，5 分钟退回去：

```bash
kubectl -n devops scale deploy/opsplatform-cmdb-backend --replicas=0
kubectl -n cesar scale deploy/opsplatform-cmdb-backend --replicas=1
kubectl -n cesar scale deploy/opsplatform-cmdb-frontend --replicas=1
kubectl -n devops delete vs opsplatform-cmdb-frontend opsplatform-cmdb-frontend-w
kubectl apply -f ~/cmdb-migrate/cmdb-vs.yaml     # 恢复到 cesar
```

旧库在停写后没有任何写入，与 dump 时刻完全一致，回滚不丢数据。

---

## 七、时间预估

| 阶段 | 耗时 | 占停机窗口 |
|---|---|---|
| 步骤 0-3（建库、同步镜像、导清单） | 20 分钟 | 否 |
| 停后端 | 1 分钟 | 是 |
| dump + 导入 + 校验 | 5～15 分钟 | 是 |
| 部署 + port-forward 验证 | 10 分钟 | 是 |
| 切 VS + 复验 | 10 分钟 | 是 |
| **停机合计** | **约 30～40 分钟** | |

---

## 八、观察期

| 时间 | 动作 |
|---|---|
| T+0 | 切换完成，cesar 的 CMDB 前后端 replicas=0 保留 |
| T+3 天 | 确认采集、证书续期、告警、AI MCP 调用全部正常 |
| T+7 天 | 删除 cesar 的 CMDB Deployment/Service/Secret；`DROP DATABASE cmdb` 在 cesar 的 MySQL 上执行 —— **注意别删整个实例，另外 5 个子系统还在用** |
| 长期 | `cmdb-20260729.sql` 归档留存 |

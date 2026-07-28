# CMDB 接入 KubeSphere（DEV 集群实操记录）

**目标**：让 CMDB / AI 能只读拉取 KubeSphere 的数据，重点是**流水线构建日志** —— Jenkins 的 `sh`/编译输出走 durable-task 回 Jenkins 控制台，不进 Pod stdout，Loki 采不到，构建脚本级报错只能靠 KubeSphere API 拿。

**环境**：DEV k3s（CMDB `cluster_id=2`），KubeSphere **4.x / ks-core**，内网 `10.146.40.x`，操作机 `g32-dev-manager`。

**结论先行**：CMDB 后端**无需改代码**，全程是配置动作。凭据用 KubeSphere ServiceAccount 签发的 `static_token`（**无 exp，永不过期**），不需要定时续期任务，也不用动 KubeSphere 全局会话策略。

---

## 一、原理：CMDB 怎么访问 KubeSphere

CMDB 已内置 KubeSphere 数据源支持，三处现成能力：

| 位置 | 说明 |
|---|---|
| `database/migrations/043_obs_endpoints.sql` | 表 `obs_endpoints` 支持 `type=kubesphere`，token 走 AES 加密存 |
| `handlers/obs_query.go` → `KubeSphere()` | 透传接口 `GET /api/obs/kubesphere?cluster_id=&env=&path=`，带 Bearer token 请求 kapis |
| `handlers/mcp.go` → `kubesphere_fetch` | MCP 工具，AI 可直接调，参数 `cluster_id` / `env` / `path` |

数据源按 `集群 > 环境 > 通用` 的优先级匹配（`resolveEndpoint`），所以同一套 CMDB 可以接多个集群的 KubeSphere。

链路：`AI → MCP kubesphere_fetch → CMDB /api/obs/kubesphere → ks-apiserver(NodePort) → kapis`

---

## 二、步骤 1：暴露 ks-apiserver 为 NodePort

DEV 的 NodePort 约定：`32767`=Prometheus(whizard)、`32766`=Loki、**`32765`=ks-apiserver**。

```bash
# 先核对 selector 和容器端口（不同版本 label 可能不同）
kubectl -n kubesphere-system get svc ks-apiserver -o jsonpath='{.spec.selector}{"\n"}{.spec.ports}{"\n"}'

kubectl apply -f deploy/dev-ks-apiserver-nodeport.yaml
curl -s -o /dev/null -w '%{http_code}\n' http://10.146.40.241:32765/kapis/version   # 期望 200
```

**为什么另建 Service 而不是 patch 自带的**：ks-installer / helm 会把自带的 `ks-apiserver` Service reconcile 回 ClusterIP，直接改会被打回去。独立 Service 升级重装都不受影响。

---

## 三、步骤 2：创建只读 ServiceAccount 并授权

```bash
kubectl apply -f deploy/dev-ks-cmdb-serviceaccount.yaml
```

包含三个对象：

1. `kubesphere.io/v1alpha1 ServiceAccount` — `kubesphere-system/cmdb-readonly`
2. `GlobalRoleBinding` → GlobalRole `platform-regular`（平台层普通用户，无管理权）
3. `ClusterRoleBinding` → ClusterRole `cluster-viewer`（集群只读，规则为 `*/*/(get,list,watch)`）

### ⚠️ 授权必须用 `kind: User`，不能用 `kind: ServiceAccount`

这是最容易踩的坑。SA 的身份在 token 里是：

```
kubesphere:serviceaccount:kubesphere-system:cmdb-readonly
```

而 RBAC 展开 `kind: ServiceAccount` 时拼的是 K8s 原生前缀 `system:serviceaccount:<ns>:<name>`，两者对不上 → 绑定形同虚设。表现为**权限规则明明是 `*/*` 却一直 403**，极具迷惑性。正确写法：

```yaml
subjects:
  - kind: User
    apiGroup: rbac.authorization.k8s.io
    name: "kubesphere:serviceaccount:kubesphere-system:cmdb-readonly"
```

### 另一个坑：DevOps 的鉴权判在 cluster scope

403 报错里写的是 `... at the cluster scope`，而 RoleTemplate `devops-view-pipelines` 的 `iam.kubesphere.io/scope` 标签是 `namespace`。两者层级对不上，**在 DevOps 项目 namespace 里建 RoleBinding 是无效的**，必须用集群层的 ClusterRoleBinding。

---

## 四、步骤 3：取 token 并确认它永久

```bash
kubectl -n kubesphere-system get secret | grep cmdb-readonly
# 找 type = kubesphere.io/service-account-token 的那条，如 cmdb-readonly-tvmbd

SATOKEN=$(kubectl -n kubesphere-system get secret cmdb-readonly-tvmbd -o jsonpath='{.data.token}' | base64 -d)

# 解 JWT payload —— 关键：不应出现 exp 字段
echo "$SATOKEN" | cut -d. -f2 | base64 -d 2>/dev/null; echo
```

期望输出（DEV 实测）：

```json
{"iss":"http://ks-console.kubesphere-system.svc:30880",
 "sub":"kubesphere:serviceaccount:kubesphere-system:cmdb-readonly",
 "iat":1785203688,"token_type":"static_token",
 "username":"kubesphere:serviceaccount:kubesphere-system:cmdb-readonly"}
```

`token_type: static_token` + **无 `exp`** = 永久有效。

Secret 一般由 controller 自动生成；万一 SA 的 `secrets` 字段为空且 grep 不到，手动建：

```bash
kubectl -n kubesphere-system create -f - <<'EOF'
apiVersion: v1
kind: Secret
metadata:
  name: cmdb-readonly-token
  namespace: kubesphere-system
  annotations:
    kubesphere.io/service-account-name: cmdb-readonly
type: kubesphere.io/service-account-token
EOF
```

---

## 五、步骤 4：验证权限

```bash
curl -s -o /dev/null -w 'nodes: %{http_code}\n' -H "Authorization: Bearer $SATOKEN" \
  'http://10.146.40.241:32765/api/v1/nodes?limit=1'
curl -s -o /dev/null -w 'namespaces: %{http_code}\n' -H "Authorization: Bearer $SATOKEN" \
  'http://10.146.40.241:32765/kapis/resources.kubesphere.io/v1alpha3/namespaces?limit=1'
```

两条均 **200** 即通（DEV 已验证）。

- **401** → token 没被认，回步骤 3
- **403** → 授权没匹配上，检查 subject 是否用了 `kind: User` + 完整用户名

---

## 六、步骤 5：CMDB 配置数据源

CMDB 前端 → 左侧 **数据源接入** → **+ 添加数据源**：

| 字段 | 值 |
|---|---|
| 名称 | `DEV KubeSphere` |
| 类型 | `KubeSphere` |
| URL | `http://10.146.40.241:32765`（不带尾斜杠，不带 /kapis） |
| 环境 | DEV |
| 集群 | dev-k8s-cluster-01（`cluster_id=2`） |
| Token | 步骤 3 的 `$SATOKEN` |

> ⚠️ **「测连通」按钮对 kubesphere 类型会误报失败**。`handlers/obs_endpoints.go` 的 `probePath()` 对 kubesphere 返回空串，探的是裸根路径，而 ks-apiserver 根路径本来就不返 2xx。以下一步的端到端验证为准。（待修：`case "kubesphere": return "/kapis/version"`）

---

## 七、步骤 6：端到端验证

```bash
# <CMDB_JWT> 由 POST /api/login 获得
curl -s -H "Authorization: Bearer <CMDB_JWT>" \
 'https://cmdb-w.sl-devops.com/api/obs/kubesphere?cluster_id=2&path=/kapis/resources.kubesphere.io/v1alpha3/namespaces?limit=1'
```

返回 `{"ok":true,"status":200,...}` 即全链路打通，AI 可以用 MCP 工具 `kubesphere_fetch` 拉数据。

---

## 八、常用 API 路径

> **⚠️ 待确认**：`/kapis/devops.kubesphere.io/v1alpha3/devops/{devops}/pipelines` 是 KubeSphere **3.x** 的路径，在本环境 4.x 上返回 404。APIService `v1alpha2/v1alpha3.devops.kubesphere.io` 确实注册着，后端是 `kubesphere-devops-system/devops-apiserver:9090`，所以是路径细节变了，不是扩展没装。确认方式：浏览器 F12 打开流水线页面抄实际请求 URL，或 `kubectl get apiservices.extensions.kubesphere.io v1alpha3.devops.kubesphere.io -o yaml` 看转发规则。

已验证可用（DEV 实测）：

| 用途 | path | 状态 |
|---|---|---|
| 节点列表 | `/api/v1/nodes` | 200 |
| 命名空间 | `/kapis/resources.kubesphere.io/v1alpha3/namespaces` | 200 |
| 版本信息 | `/kapis/version` | 200 |
| **流水线定义/状态** | `/apis/devops.kubesphere.io/v1alpha3/namespaces/{ns}/pipelines` | 200 |

已确认**不可用**的路径（4.x 上全 404，都是 3.x 老形态）：

- `/kapis/devops.kubesphere.io/v1alpha3/devops`
- `/kapis/devops.kubesphere.io/v1alpha3/devops/{ns}/pipelines`
- `/kapis/devops.kubesphere.io/v1alpha2/devops/{ns}/pipelines`

流水线相关（路径待确认后补全）：

| 用途 | path（3.x 形态，供参考） |
|---|---|
| 流水线列表 | `/kapis/devops.kubesphere.io/v1alpha3/devops/{devops}/pipelines` |
| 运行记录 | `.../pipelines/{pipeline}/runs` |
| **构建控制台日志** | `.../runs/{runId}/log?start=0` |
| 阶段/步骤明细 | `.../runs/{runId}/nodedetail/?limit=10000` |

备选：Pipeline 本身是 CRD，可走 K8s 原生接口 `/apis/devops.kubesphere.io/v1alpha3/namespaces/{ns}/pipelines` 读定义（`cluster-viewer` 覆盖得到），但**拿不到构建日志** —— 日志在 Jenkins 里，必须走 kapis。

---

## 九、方案取舍记录

接入过程中评估过三条路，记下来避免以后重复讨论：

| 方案 | 结论 |
|---|---|
| **A. CMDB 存账号密码，自动换 token** | 可行，需改代码（migration + `obs_query.go` 加 token 缓存 + 前端表单）。方案 C 成立后没必要。 |
| **B. 全局 token 永不过期** | 4.x 的 `authentication.issuer.accessTokenMaxAge` 是**全局**的（DEV 当前 168h），改 `0h` 会让所有 console 用户会话都不过期，安全性代价大；且 helm upgrade ks-core 会覆盖手改。**不采用**。 |
| **C. ServiceAccount 静态 token** | ✅ **采用**。只影响一个身份，永久有效，不动全局策略，不需要 cron。 |

4.x **没有** 3.x 那种 `authentication.oauthOptions.clients` 结构，所以无法按 client 单独设置 token 有效期。

密码模式换 token 在 4.x 仍可用（`POST /oauth/token`，`client_id=kubesphere&client_secret=kubesphere`，`expires_in=604800`），是方案 A/B 的基础，方案 C 下用不到。

---

## 十、清理与回滚

接入过程中的废弃对象（绑定写法不对或层级不对，留着无用且容易误导）：

```bash
kubectl delete clusterrolebinding.iam.kubesphere.io cmdb-readonly-sa-cluster-viewer
kubectl delete globalrolebinding.iam.kubesphere.io cmdb-readonly-sa-platform-regular
kubectl -n g66-test-devopsj2q22 delete rolebinding cmdb-readonly-sa-viewer cmdb-readonly-viewer
kubectl delete user.iam.kubesphere.io cmdb-readonly
kubectl delete globalrolebinding.iam.kubesphere.io cmdb-readonly-platform-regular
```

> 删 User 后**立刻重测一次 `$SATOKEN`**：User 和 ServiceAccount 同名但是不同资源类型，正常不受影响，但值得验一次。

**作废 token**：删掉对应的 Secret 重签（token 永久有效，泄漏无法靠等待过期兜底，不要贴进聊天/工单/截图）。

**整体回滚**：删掉上面的 SA 和两个绑定，再删 `ks-apiserver-nodeport` Service，KubeSphere 恢复原状。

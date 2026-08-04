# k8s-port-proxy —— 本地固定访问入口

Docker Desktop 的 K8s NodePort 不会直接 expose 到 `localhost`，这层 nginx 容器接在 kind 网络上，
把节点的 NodePort 转发到 Mac host 的同名端口。

**所有本地验证都走这里的固定端口，不要临时 `kubectl port-forward`**
（临时端口会跟别人占用的端口撞，2026-07-28 就撞过 CMDB 占着的 18080，请求打到了别的服务上）。

## 端口表

| 端口 | 服务 | 用途 |
|---|---|---|
| 30081 | opsplatform 主平台前端 | 页面 + **API：`localhost:30081/api/...`** |
| 30091 | MinIO Console | 对象存储控制台 |
| 30300 | Gitea | 本地 Git 服务 |
| 30319 | video-images 画廊看板 | 内网缩略图看板 |
| 30826 | 发布控制台前端 | 页面 + **API：`localhost:30826/api/...`** |
| 30829 | CMDB 前端 | 页面 + **API：`localhost:30829/api/...`** |
| 30830 | 运维 AI 助手前端 | 页面 + **API：`localhost:30830/api/...`** |
| 30825 | ES 告警平台前端 | 页面 + **API：`localhost:30825/api/...`** |
| 30840 | video-images web | 缩略图代理（根路径 404 是正常的，按 key 取图） |
| 30900 | SSO 后端 | |
| 30901 | SSO 前端 | |
| 30306 | opsplatform MySQL | TCP 直连调试，见下方 |

### 后端 API 不需要单独开端口

各系统的前端 nginx 里都有 `location /api/ { proxy_pass http://<后端服务>:8080; }`，
所以**前端端口就是后端 API 入口**，直接打：

```bash
curl "http://localhost:30081/api/schedule/export?start=2026-06" -H "Authorization: Bearer $TOKEN"
```

### MySQL 直连

```bash
mysql -h127.0.0.1 -P30306 -uroot -p'<MYSQL_ROOT_PASSWORD>' opsplatform --default-character-set=utf8mb4
```

密码在 `kubectl get secret mysql-secret -n opsplatform`。
走的是 [mysql-nodeport.yaml](mysql-nodeport.yaml) 这个独立 Service（NodePort 30306），
原来的 ClusterIP `mysql` 不动，集群内应用照常用 DNS 访问。

## 加 / 删服务

三处必须同步改，然后 `docker compose up -d`：

1. `nginx.conf` —— http 服务加 `server` 块；纯 TCP（数据库之类）加到 `stream` 块
2. `docker-compose.yml` —— `ports` 加映射
3. 本文件端口表加一行

端口清单要跟集群实际一致，核对命令：

```bash
kubectl get svc -A -o jsonpath='{range .items[?(@.spec.type=="NodePort")]}{.metadata.namespace}/{.metadata.name}{"\t"}{range .spec.ports[*]}{.nodePort}{" "}{end}{"\n"}{end}'
```

## 集群 IP 变了（全部端口 502）

Docker Desktop 重启后节点容器 IP 会被重新分配，`nginx.conf` 里写死的 IP 失效：

```bash
bash fix-cluster-ip.sh
```

脚本自动查当前 IP、替换、reload、验证关键端口。

## 已下线的映射

以下服务的 namespace / NodePort 已经不存在，2026-07-28 清理时从配置里移除了。要恢复先在集群里建对应 NodePort：

- `30443` ArgoCD UI —— `argocd-server` 现在是 ClusterIP
- `30825` ES 告警平台 —— `alert-ops` namespace 已不存在
- `30827` gke-version 前端
- `30828` k8sinsight 前端 —— `k8sinsight` namespace 已不存在
- `30082`–`30090` —— 无对应服务

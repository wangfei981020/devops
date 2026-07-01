# GitHub Actions Workflows

## Docker Build Workflow

自动构建并推送 Docker 镜像到 GitHub Packages。

### 触发条件

- **Push 到 main/master 分支**: 自动构建并推送镜像
- **创建版本标签 (v*)**: 构建并推送带版本标签的镜像
- **Pull Request**: 仅构建镜像（不推送），用于验证构建是否成功
- **手动触发**: 通过 GitHub Actions 界面手动触发

### 构建的镜像

工作流会构建两个 Docker 镜像：

1. **后端镜像**: `ghcr.io/OWNER/REPO/backend:tag`
2. **前端镜像**: `ghcr.io/OWNER/REPO/frontend:tag`

### 镜像标签

- `latest`: 主分支的最新构建
- `main-<sha>`: 基于 commit SHA 的标签
- `v1.0.0`: 语义化版本标签
- `v1.0`: 主版本.次版本标签
- `v1`: 主版本标签

### 使用镜像

#### 拉取镜像

```bash
# 拉取后端镜像
docker pull ghcr.io/OWNER/REPO/backend:latest

# 拉取前端镜像
docker pull ghcr.io/OWNER/REPO/frontend:latest
```

#### 在 docker-compose 中使用

更新 `docker-compose.prod.yml`:

```yaml
services:
  backend:
    image: ghcr.io/OWNER/REPO/backend:latest
    # ... 其他配置

  frontend:
    image: ghcr.io/OWNER/REPO/frontend:latest
    # ... 其他配置
```

#### 认证

如果镜像设置为私有，需要先登录：

```bash
echo $GITHUB_TOKEN | docker login ghcr.io -u USERNAME --password-stdin
```

### 权限要求

工作流需要以下权限：
- `contents: read` - 读取仓库内容
- `packages: write` - 推送镜像到 GitHub Packages

这些权限会自动从 `GITHUB_TOKEN` 获取。

### 缓存

工作流使用 GitHub Actions 缓存来加速构建：
- 构建缓存存储在 GitHub Actions 缓存中
- 后续构建会复用缓存层，显著加快构建速度

### 平台支持

当前仅构建 `linux/amd64` (x86_64) 架构的镜像。


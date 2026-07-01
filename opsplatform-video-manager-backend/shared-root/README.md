# Video Manager

视频管理系统 - CDN 线路管理模块

## 项目结构

```
video-manager/
├── backend/                    # Golang 后端服务
│   ├── cmd/                   # 应用入口
│   ├── internal/              # 内部代码（handlers, services, repositories, models）
│   ├── migrations/            # 数据库迁移脚本
│   ├── pkg/                   # 可复用的包
│   ├── Dockerfile             # 生产环境 Dockerfile
│   └── Dockerfile.dev         # 开发环境 Dockerfile
├── frontend/                  # React + TypeScript 前端应用
│   ├── src/                   # 源代码
│   ├── Dockerfile             # 生产环境 Dockerfile
│   ├── Dockerfile.dev         # 开发环境 Dockerfile
│   └── nginx.conf             # Nginx 配置文件
├── docker-compose.yml         # 开发环境 Docker Compose 配置
├── docker-compose.prod.yml    # 生产环境 Docker Compose 配置
├── deploy-prod.sh             # 生产环境部署脚本
├── .env.example               # 开发环境变量模板
├── .env.prod.example          # 生产环境变量模板
└── PRODUCTION.md              # 生产环境部署详细文档
```

## 技术栈

### 后端
- **语言**: Go 1.24.0
- **框架**: Gin v1.9.1
- **数据库**: PostgreSQL 16-alpine
- **数据库驱动**: pgx/v5 v5.5.1
- **认证**: JWT (golang-jwt/jwt/v5)
- **API 文档**: Swagger (swaggo/swag, swaggo/gin-swagger)
- **日志**: 结构化日志 (log/slog)

### 前端
- **框架**: React 19.2.0
- **语言**: TypeScript 5.9.3
- **UI 组件库**: Ant Design 6.1.0
- **构建工具**: Vite 7.2.4
- **路由**: React Router DOM 7.10.1
- **HTTP 客户端**: Axios 1.13.2
- **视频播放**: flv.js 1.6.2
- **样式**: Tailwind CSS 4.1.18

### 开发环境
- **容器化**: Docker Compose
- **数据库**: PostgreSQL 16-alpine
- **热重载**: Air (后端), Vite HMR (前端)

## 快速开始

### 开发环境

#### 1. 环境准备

确保已安装：
- Docker 和 Docker Compose
- Go 1.24+ (如果本地开发)
- Node.js 20+ 和 npm (如果本地开发)
- Make (可选，用于便捷命令)

#### 2. 配置环境变量

复制环境变量模板：
```bash
cp .env.example .env
```

根据需要修改 `.env` 文件中的配置。

#### 3. 启动开发环境

**使用 Docker Compose（推荐）**：

```bash
# 启动所有服务
docker-compose up -d

# 查看日志
docker-compose logs -f

# 停止服务
docker-compose down
```

**使用 Make 命令**：
```bash
make up          # 后台启动
make up-logs     # 前台启动并显示日志
make logs        # 查看所有日志
make down        # 停止服务
```

#### 4. 访问应用

- **前端**: http://localhost:3000
- **后端 API**: http://localhost:8080/api
- **Swagger UI**: http://localhost:3000/swagger/index.html
- **默认账号**: admin / admin123

### 生产环境

#### 1. 配置环境变量

```bash
# 复制生产环境变量模板
cp .env.prod.example .env.prod

# 编辑配置文件，设置强密码和密钥
nano .env.prod
```

**重要**: 必须修改以下配置：
- `POSTGRES_PASSWORD`: 强密码
- `JWT_SECRET`: 随机密钥（可使用 `openssl rand -base64 32` 生成）
- `ADMIN_PASSWORD`: 管理员强密码

#### 2. 部署

**使用部署脚本（推荐）**：
```bash
./deploy-prod.sh
```

**手动部署**：
```bash
# 构建镜像
docker-compose -f docker-compose.prod.yml build

# 启动服务
docker-compose -f docker-compose.prod.yml up -d

# 查看状态
docker-compose -f docker-compose.prod.yml ps

# 查看日志
docker-compose -f docker-compose.prod.yml logs -f
```

#### 3. 访问应用

- **前端**: http://localhost (或配置的端口)
- **后端 API**: http://localhost:8080/api
- **Swagger UI**: http://localhost/swagger/index.html

详细的生产环境部署说明请参考 [PRODUCTION.md](./PRODUCTION.md)

## 功能特性

### 核心功能
- ✅ CDN 提供商管理
- ✅ CDN 线路管理
- ✅ 域名管理
- ✅ 视频流区域管理
- ✅ 流路径管理（桌台号）
- ✅ 视频流端点自动生成
- ✅ 端点状态管理（启用/禁用）
- ✅ FLV 视频流播放
- ✅ 用户认证和授权（JWT）
- ✅ Token 管理（创建、查看、删除）
- ✅ 统计仪表板
- ✅ 批量删除功能
- ✅ 智能分页
- ✅ 多维度筛选

### 安全特性
- JWT Token 认证
- 密码加密存储（bcrypt）
- 非 root 用户运行（生产环境）
- 安全 HTTP 头（生产环境）
- 资源限制和健康检查

## API 端点

### 认证
- `POST /api/auth/login` - 用户登录
- `POST /api/auth/change-password` - 修改密码（需认证）
- `GET /api/auth/me` - 获取当前用户信息（需认证）
- `POST /api/auth/tokens` - 创建 Token（需认证）
- `GET /api/auth/tokens` - 获取 Token 列表（需认证）
- `DELETE /api/auth/tokens/:id` - 删除 Token（需认证）

### CDN 提供商
- `GET /api/cdn-providers` - 获取所有提供商
- `GET /api/cdn-providers/:id` - 获取指定提供商
- `POST /api/cdn-providers` - 创建提供商
- `PUT /api/cdn-providers/:id` - 更新提供商
- `DELETE /api/cdn-providers/:id` - 删除提供商

### CDN 线路
- `GET /api/cdn-lines` - 获取所有线路（支持 `?provider_id=` 过滤）
- `GET /api/cdn-lines/:id` - 获取指定线路
- `POST /api/cdn-lines` - 创建线路
- `PUT /api/cdn-lines/:id` - 更新线路
- `DELETE /api/cdn-lines/:id` - 删除线路

### 域名
- `GET /api/domains` - 获取所有域名
- `GET /api/domains/:id` - 获取指定域名
- `POST /api/domains` - 创建域名
- `PUT /api/domains/:id` - 更新域名
- `DELETE /api/domains/:id` - 删除域名

### 视频流区域
- `GET /api/stream-regions` - 获取所有视频流区域
- `GET /api/stream-regions/:id` - 获取指定视频流区域
- `POST /api/stream-regions` - 创建视频流区域
- `PUT /api/stream-regions/:id` - 更新视频流区域
- `DELETE /api/stream-regions/:id` - 删除视频流区域

### 流路径
- `GET /api/stream-paths` - 获取所有流路径（支持 `?stream_id=` 过滤）
- `GET /api/stream-paths/:id` - 获取指定流路径
- `POST /api/stream-paths` - 创建流路径
- `PUT /api/stream-paths/:id` - 更新流路径
- `DELETE /api/stream-paths/:id` - 删除流路径

### 视频流端点
- `GET /api/video-stream-endpoints` - 获取所有端点（支持多维度过滤）
  - 查询参数: `provider_id`, `line_id`, `domain_id`, `stream_id`, `table_id`, `status`
- `GET /api/video-stream-endpoints/:id` - 获取指定端点
- `PATCH /api/video-stream-endpoints/:id/status` - 更新端点状态
- `POST /api/video-stream-endpoints/generate` - 重新生成所有端点

### 统计
- `GET /api/stats` - 获取系统统计信息

### 健康检查
- `GET /health` - 健康检查端点

### API 文档
- `GET /swagger/index.html` - Swagger UI

## 开发

### Docker Compose 开发模式

使用 Docker Compose 时，支持热重载：
- **后端**: 使用 Air 自动重载，修改代码后自动重启
- **前端**: 使用 Vite HMR，修改代码后自动刷新
- **数据库**: 数据持久化在 `postgres_data` 卷中

### 本地开发

#### 后端开发

```bash
cd backend
go mod download
go run cmd/main.go
```

或者使用 Air 实现热重载：
```bash
cd backend
go install github.com/cosmtrek/air@latest
air
```

后端服务将在 `http://localhost:8080` 启动。

#### 前端开发

```bash
cd frontend
npm install
npm run dev
```

前端应用将在 `http://localhost:3000` 启动。

### 调试技巧

#### 使用 Makefile（推荐）

查看所有可用命令：
```bash
make help
```

常用命令：
```bash
make up              # 启动所有服务
make down            # 停止所有服务
make logs            # 查看所有日志
make logs-backend    # 查看后端日志
make logs-frontend   # 查看前端日志
make db-only         # 只启动数据库
make shell-backend   # 进入后端容器
make shell-frontend  # 进入前端容器
make shell-db        # 进入数据库
make restart         # 重启所有服务
make clean           # 清理所有容器和卷（危险）
```

#### 直接使用 Docker Compose

1. **只运行数据库**：
   ```bash
   docker-compose up postgres -d
   ```

2. **查看服务日志**：
   ```bash
   docker-compose logs -f backend
   docker-compose logs -f frontend
   ```

3. **进入容器调试**：
   ```bash
   docker-compose exec backend sh
   docker-compose exec frontend sh
   ```

4. **重建服务**：
   ```bash
   docker-compose build backend
   docker-compose up backend
   ```

### 数据库迁移

数据库迁移会在应用启动时自动执行。如果需要手动运行：

```bash
# 进入后端容器
docker-compose exec backend sh

# 手动运行迁移（如果需要）
# 迁移脚本位于 backend/migrations/ 目录
```

## 环境变量

### 开发环境 (.env)

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `POSTGRES_USER` | PostgreSQL 用户名 | `videomanager` |
| `POSTGRES_PASSWORD` | PostgreSQL 密码 | `videomanager` |
| `POSTGRES_DB` | 数据库名 | `videomanager` |
| `API_PORT` | 后端 API 端口 | `8080` |
| `FRONTEND_PORT` | 前端端口 | `3000` |
| `GIN_MODE` | Gin 模式 | `debug` |
| `JWT_SECRET` | JWT 密钥 | `your-secret-key-change-in-production` |
| `ADMIN_USERNAME` | 管理员用户名 | `admin` |
| `ADMIN_PASSWORD` | 管理员密码 | `admin123` |
| `LOG_LEVEL` | 日志级别 | `INFO` |
| `LOG_FORMAT` | 日志格式 | `text` |

### 生产环境 (.env.prod)

生产环境变量说明请参考 `.env.prod.example` 和 [PRODUCTION.md](./PRODUCTION.md)

## 项目特性

### 自动端点生成
视频流端点会根据以下配置自动生成：
- CDN 提供商、线路、域名
- 视频流区域、流路径

当这些配置发生变更（新增、更新、删除）时，端点会自动重新生成。

### URL 生成规则
视频流端点 URL 格式：
```
https://{line_display_name}.{domain}/{stream_path_full_path}.flv
```

### 认证机制
- 使用 JWT Token 进行认证
- Token 可以设置过期时间或永不过期
- 支持 Token 管理（创建、查看、删除）
- 所有 API 端点（除登录外）都需要认证

## 故障排除

### 常见问题

1. **端口被占用**
   ```bash
   # 检查端口占用
   lsof -i :8080
   lsof -i :3000

   # 修改 docker-compose.yml 中的端口映射
   ```

2. **数据库连接失败**
   ```bash
   # 检查数据库容器状态
   docker-compose ps postgres

   # 查看数据库日志
   docker-compose logs postgres
   ```

3. **前端无法连接后端**
   - 检查 `VITE_API_BASE_URL` 配置
   - 检查后端服务是否正常运行
   - 检查网络连接

4. **迁移失败**
   - 检查数据库连接配置
   - 查看后端日志获取详细错误信息
   - 确保数据库用户有足够权限

## 贡献

欢迎提交 Issue 和 Pull Request！

## 许可证

MIT License

Copyright (c) 2025 kk

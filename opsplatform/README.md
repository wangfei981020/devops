# 运维管理平台 (OpsPlat)

一个现代化的运维管理平台，基于 Go + Vue.js 构建，支持 Kubernetes 部署。

## 🚀 功能特性

- ✅ **用户管理** - 登录认证、角色权限、用户管理
- ✅ **数据源ID管理** - CRUD、批量操作、导入导出
- ✅ **审计日志** - 操作追踪、变更记录、日志导出
- ✅ **数据源配置** - 多数据源管理
- ✅ **一键巡检** - 基础巡检框架
- ✅ **主题切换** - 白天/夜间模式

## 📁 项目结构

```
opsplatform/
├── backend/                # Go 后端
│   ├── database/           # 数据库连接和初始化
│   ├── handlers/           # API 处理函数
│   ├── models/             # 数据模型
│   ├── main.go             # 程序入口
│   ├── Dockerfile          # Docker 构建文件
│   └── go.mod              # Go 依赖管理
├── frontend/               # 前端静态文件
│   ├── css/                # 样式文件
│   ├── js/                 # JavaScript 文件
│   ├── index.html          # 主页面
│   ├── nginx.conf          # Nginx 配置
│   └── Dockerfile          # Docker 构建文件
├── k8s/                    # Kubernetes 部署配置
│   ├── namespace.yaml      # 命名空间
│   ├── mysql.yaml          # MySQL 数据库
│   ├── backend.yaml        # 后端服务
│   ├── frontend.yaml       # 前端服务
│   ├── deploy.ps1          # 部署脚本
│   └── push-images.ps1     # 镜像推送脚本
└── README.md               # 本文件
```

## 🛠️ 技术栈

| 组件 | 技术 |
|------|------|
| 后端 | Go 1.21+, Gorilla Mux |
| 前端 | Vue.js 3, 原生 CSS |
| 数据库 | MySQL 8.0 |
| 容器化 | Docker, Kubernetes |
| Web服务器 | Nginx |

## ⚡ 快速开始

### 本地开发

```bash
# 1. 克隆项目
git clone https://github.com/wangfei981020/devops.git
cd devops/opsplatform

# 2. 配置数据库 (确保 MySQL 运行中)
# 默认配置: localhost:3306, root/123456, 数据库名: opsplatform

# 3. 启动后端
cd backend
go run .

# 4. 访问
# http://localhost:8088
# 默认账号: admin / admin123
```

### Docker 部署

```bash
# 构建镜像
cd backend && docker build -t opsplatform-backend .
cd ../frontend && docker build -t opsplatform-frontend .

# 运行
docker run -d -p 8088:8088 opsplatform-backend
docker run -d -p 80:80 opsplatform-frontend
```

### Kubernetes 部署

```powershell
cd k8s
.\deploy.ps1

# 访问: http://localhost:30080
```

## 🔧 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| MYSQL_HOST | localhost | MySQL 主机 |
| MYSQL_PORT | 3306 | MySQL 端口 |
| MYSQL_USER | root | MySQL 用户名 |
| MYSQL_PASSWORD | 123456 | MySQL 密码 |
| MYSQL_DATABASE | opsplatform | 数据库名 |
| PORT | 8088 | 服务端口 |

## 📡 API 接口

### 用户认证
- `POST /api/login` - 用户登录

### 记录管理
- `GET /api/records` - 获取所有记录
- `POST /api/records` - 添加记录
- `PUT /api/records/:id` - 更新记录
- `DELETE /api/records/:id` - 删除记录
- `POST /api/records/batch` - 批量添加
- `POST /api/records/batch-delete` - 批量删除
- `POST /api/records/batch-status` - 批量修改状态
- `GET /api/records/export` - 导出 CSV

### 用户管理
- `GET /api/users` - 获取用户列表
- `POST /api/users` - 创建用户
- `PUT /api/users/:id` - 更新用户
- `DELETE /api/users/:id` - 删除用户

### 审计日志
- `GET /api/audit-logs` - 获取审计日志
- `GET /api/audit-logs/export` - 导出审计日志

### 数据源管理
- `GET /api/datasources` - 获取数据源列表
- `POST /api/datasources` - 添加数据源
- `PUT /api/datasources/:id` - 更新数据源
- `DELETE /api/datasources/:id` - 删除数据源
- `POST /api/datasources/test` - 测试连接

## 🔒 安全特性

- ✅ 密码 bcrypt 加密存储
- ✅ 角色权限控制
- ✅ 操作审计日志
- ✅ SQL 注入防护（参数化查询）

## 📊 数据库表结构

### records (数据源ID表)
| 字段 | 类型 | 说明 |
|------|------|------|
| id | VARCHAR(64) | 主键 |
| connection_id | VARCHAR(128) | 连接ID（唯一） |
| project | VARCHAR(255) | 项目名 |
| env | VARCHAR(32) | 环境 |
| vid | VARCHAR(255) | VID |
| src_ip | VARCHAR(64) | 源IP |
| dest_ip | VARCHAR(64) | 目标IP |
| port | VARCHAR(32) | 端口 |
| status | VARCHAR(32) | 状态 |
| created_at | DATETIME | 创建时间 |
| updated_at | DATETIME | 更新时间 |

### users (用户表)
| 字段 | 类型 | 说明 |
|------|------|------|
| id | VARCHAR(64) | 主键 |
| username | VARCHAR(128) | 用户名（唯一） |
| password | VARCHAR(255) | 密码（bcrypt加密） |
| display_name | VARCHAR(128) | 显示名称 |
| role | VARCHAR(32) | 角色 |
| status | VARCHAR(32) | 状态 |
| permissions | TEXT | 权限 |

## 🐳 镜像仓库

```
ghcr.io/wangfei981020/opsplatform-backend:latest
ghcr.io/wangfei981020/opsplatform-frontend:latest
```

## 📝 开发计划

- [ ] JWT Token 认证
- [ ] 主机管理模块
- [ ] 监控告警集成
- [ ] CI/CD 集成
- [ ] 更多自动化功能

## 📄 License

MIT License

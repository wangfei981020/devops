# Prometheus 巡检系统

这是一个用于监控 Prometheus 指标的系统，可以检查 CPU、内存和磁盘使用率，并按多个阈值（30%、40%、50%、60%、70%、80%、90%）进行分类展示。

## 功能特性

- ✅ 自动查询 Prometheus 的 CPU、内存、磁盘使用率
- ✅ 按多个阈值（30%-90%）分类展示
- ✅ 美观的 Web 界面（基于 Vue 3）
- ✅ 实时刷新功能
- ✅ 数据源管理（支持多个 Prometheus 实例）
- ✅ 数据源切换和测试连接
- ✅ RESTful API 接口

**注意：** 这是巡检系统，用于列出超过各阈值的实例，不是告警系统。

## 系统要求

- Go 1.21 或更高版本
- 可访问的 Prometheus 实例
- 现代浏览器（支持 ES6+，用于运行 Vue 3 前端）

## 安装步骤

### 1. 安装依赖

```bash
cd go
go mod download
```

### 2. 配置环境变量（可选）

```bash
# Windows PowerShell
$env:PROMETHEUS_URL="http://localhost:9090"
$env:PORT="8080"

# Linux/Mac
export PROMETHEUS_URL="http://localhost:9090"
export PORT="8080"
```

如果不设置环境变量，默认使用：
- Prometheus URL: `http://localhost:9090`
- 服务端口: `8080`

### 3. 运行程序

```bash
go run main.go
```

或者编译后运行：

```bash
go build -o prometheus-inspector
./prometheus-inspector
```

**注意：** 首次运行会自动创建 `datasources.json` 配置文件，并创建一个默认的 Prometheus 数据源（从环境变量 `PROMETHEUS_URL` 读取，默认为 `http://localhost:9090`）。

### 4. 访问 Web 界面

打开浏览器访问：`http://localhost:8080`

### 5. 配置数据源

1. 点击页面上的"管理数据源"按钮
2. 点击"+ 添加数据源"添加新的 Prometheus 实例
3. 填写数据源信息（名称、URL、描述）
4. 点击"测试连接"验证连接是否正常
5. 保存后，点击"切换"按钮激活该数据源
6. 数据源配置会自动保存到 `datasources.json` 文件中

## Prometheus 指标要求

系统会尝试查询以下 Prometheus 指标：

### CPU 使用率
- 主要查询: `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`
- 备用查询: `100 - (avg(irate(node_cpu_seconds_total{mode="idle"}[5m])) by (instance) * 100)`

### 内存使用率
- 主要查询: `100 * (1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes))`
- 备用查询: `(node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes) / node_memory_MemTotal_bytes * 100`

### 磁盘使用率
- 主要查询: `100 * (1 - (node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"}))`
- 备用查询: `(node_filesystem_size_bytes{mountpoint="/"} - node_filesystem_avail_bytes{mountpoint="/"}) / node_filesystem_size_bytes{mountpoint="/"} * 100`

**注意**: 如果你的 Prometheus 使用不同的指标名称，需要修改 `main.go` 中的查询语句。

## API 接口

### 巡检接口

```
GET /api/inspect
```

返回 JSON 格式的巡检结果：

```json
{
  "timestamp": "2024-01-01 12:00:00",
  "cpu": {
    "90%": [
      {
        "instance": "192.168.1.100:9100",
        "job": "node-exporter",
        "value": 95.5,
        "threshold": 90,
        "metric_type": "CPU"
      }
    ]
  },
  "memory": {},
  "disk": {},
  "summary": {
    "total_instances": 10,
    "cpu_alerts": 2,
    "memory_alerts": 1,
    "disk_alerts": 0
  }
}
```

### 健康检查接口

```
GET /api/health
```

返回：

```json
{
  "status": "ok"
}
```

## 自定义配置

### 修改阈值

在 `main.go` 中修改 `thresholds` 变量：

```go
var thresholds = []float64{90, 80, 70, 60, 50, 40, 30}
```

### 修改查询语句

在 `main.go` 的 `Inspect()` 方法中修改 Prometheus 查询语句。

### 修改端口

通过环境变量 `PORT` 或直接修改代码中的默认值。

## 故障排查

### 1. 无法连接到 Prometheus

- 检查 Prometheus URL 是否正确
- 确认 Prometheus 服务正在运行
- 检查网络连接和防火墙设置

### 2. 查询返回空结果

- 确认 Prometheus 中有对应的指标数据
- 检查指标名称是否正确
- 查看 Prometheus 的 `/api/v1/query` 接口测试查询语句

### 3. 前端页面无法加载

- 确认 `static` 目录存在且包含 `index.html`
- 检查浏览器控制台的错误信息
- 确认服务端口未被占用

## 开发说明

### 项目结构

```
go/
├── main.go          # 主程序文件
├── go.mod           # Go 模块定义
├── go.sum           # 依赖锁定文件（自动生成）
├── datasources.json # 数据源配置文件（自动生成）
├── static/          # 静态文件目录
│   └── index.html   # Vue 3 前端页面
└── README.md        # 本文档
```

### 依赖包

**后端：**
- `github.com/prometheus/client_golang` - Prometheus Go 客户端
- `github.com/gorilla/mux` - HTTP 路由

**前端：**
- Vue 3 (通过 CDN 引入，无需构建工具)

## 许可证

MIT License

## 贡献

欢迎提交 Issue 和 Pull Request！


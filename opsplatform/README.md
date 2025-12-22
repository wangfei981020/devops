# 网络文档管理系统

网络配置记录管理系统，支持记录网络策略、审计日志、用户管理等功能。

## 快速开始

### 环境要求
- Go 1.21+
- MySQL 8.0+（需要 utf8mb4 字符集）

### 启动服务

```bash
# 设置环境变量（可选，有默认值）
export DB_HOST=127.0.0.1
export DB_PORT=3306
export DB_USER=root
export DB_PASS=123456
export DB_NAME=netdoc
export PORT=8088

# 启动
cd netdoc
go run main.go
```

### 访问
- Web 界面: http://localhost:8088
- 默认管理员: `admin` / `admin123`

---

## API 接口文档

### 基础信息
- 基础URL: `http://localhost:8088/api`
- Content-Type: `application/json`
- 字符集: `UTF-8`

---

### 1. 获取所有记录

```http
GET /api/records
```

**响应示例：**
```json
[
  {
    "id": "rec_1766218007734108500",
    "project": "订单系统",
    "env": "uat",
    "vid": "VLAN100",
    "src_ip": "192.168.1.10",
    "dest_ip": "10.0.0.5",
    "port": "8080",
    "status": "active",
    "created_at": "2025-12-20T16:06:47+08:00",
    "updated_at": "2025-12-20T16:06:47+08:00",
    "created_by": "admin",
    "updated_by": "admin"
  }
]
```

**调用示例：**
```bash
curl -s "http://localhost:8088/api/records"
```

```python
import requests
response = requests.get("http://localhost:8088/api/records")
records = response.json()
```

---

### 2. 查询记录（支持过滤）⭐ 推荐 AI 调用

```http
GET /api/records/query
```

**查询参数（全部可选）：**

| 参数 | 类型 | 说明 | 匹配方式 | 示例 |
|------|------|------|----------|------|
| `project` | string | 项目名称 | 模糊匹配 | `?project=订单` |
| `env` | string | 环境 | 精确匹配 | `?env=prod` 或 `?env=uat` |
| `vid` | string | VID | 模糊匹配 | `?vid=VLAN100` |
| `src_ip` | string | 源IP | 模糊匹配 | `?src_ip=192.168` |
| `dest_ip` | string | 目标IP | 模糊匹配 | `?dest_ip=10.0.0` |
| `port` | string | 端口 | 模糊匹配 | `?port=8080` |
| `status` | string | 状态 | 精确匹配 | `?status=active` |

**状态值：**
- `active` - 启用
- `inactive` - 停用
- `pending` - 待定

**环境值：**
- `uat` - UAT 测试环境
- `prod` - 生产环境

**响应示例：**
```json
{
  "count": 1,
  "records": [
    {
      "id": "rec_1766218007734108500",
      "project": "订单系统",
      "env": "uat",
      "vid": "VLAN100",
      "src_ip": "192.168.1.10",
      "dest_ip": "10.0.0.5",
      "port": "8080",
      "status": "active",
      "created_at": "2025-12-20T16:06:47+08:00",
      "updated_at": "2025-12-20T16:06:47+08:00",
      "created_by": "admin",
      "updated_by": "admin"
    }
  ]
}
```

**调用示例：**

```bash
# 查询订单系统的配置
curl -s "http://localhost:8088/api/records/query?project=订单"

# 查询生产环境所有配置
curl -s "http://localhost:8088/api/records/query?env=prod"

# 查询 UAT 环境的配置
curl -s "http://localhost:8088/api/records/query?env=uat"

# 查询某个 VID 的配置
curl -s "http://localhost:8088/api/records/query?vid=VLAN100"

# 查询某个源 IP 网段的配置
curl -s "http://localhost:8088/api/records/query?src_ip=192.168.1"

# 查询某个目标 IP 的配置
curl -s "http://localhost:8088/api/records/query?dest_ip=10.0.0.5"

# 查询某个端口的配置
curl -s "http://localhost:8088/api/records/query?port=8080"

# 查询启用状态的配置
curl -s "http://localhost:8088/api/records/query?status=active"

# 组合查询：查询订单系统在 UAT 环境的配置
curl -s "http://localhost:8088/api/records/query?project=订单&env=uat"

# 组合查询：查询生产环境中使用 8080 端口的配置
curl -s "http://localhost:8088/api/records/query?env=prod&port=8080"
```

```python
import requests

# 查询订单系统的配置
response = requests.get("http://localhost:8088/api/records/query", params={
    "project": "订单"
})
data = response.json()
print(f"找到 {data['count']} 条记录")
for record in data['records']:
    print(f"  {record['project']} - {record['src_ip']} -> {record['dest_ip']}:{record['port']}")

# 查询生产环境配置
response = requests.get("http://localhost:8088/api/records/query", params={
    "env": "prod"
})
prod_records = response.json()['records']

# 组合查询
response = requests.get("http://localhost:8088/api/records/query", params={
    "project": "订单",
    "env": "uat",
    "status": "active"
})
```

```javascript
// Node.js / JavaScript
const fetch = require('node-fetch');

// 查询订单系统配置
async function queryRecords() {
    const params = new URLSearchParams({
        project: '订单',
        env: 'uat'
    });
    
    const response = await fetch(`http://localhost:8088/api/records/query?${params}`);
    const data = await response.json();
    
    console.log(`找到 ${data.count} 条记录`);
    data.records.forEach(r => {
        console.log(`${r.project}: ${r.src_ip} -> ${r.dest_ip}:${r.port}`);
    });
}
```

---

### 3. 获取单条记录

```http
GET /api/records/{id}
```

**调用示例：**
```bash
curl -s "http://localhost:8088/api/records/rec_1766218007734108500"
```

---

### 4. 添加记录

```http
POST /api/records
Content-Type: application/json
```

**请求体：**
```json
{
  "record": {
    "project": "订单系统",
    "env": "uat",
    "vid": "VLAN100",
    "src_ip": "192.168.1.10",
    "dest_ip": "10.0.0.5",
    "port": "8080",
    "status": "active"
  },
  "operator": "admin"
}
```

**调用示例：**
```bash
curl -X POST "http://localhost:8088/api/records" \
  -H "Content-Type: application/json" \
  -d '{
    "record": {
      "project": "订单系统",
      "env": "uat",
      "vid": "VLAN100",
      "src_ip": "192.168.1.10",
      "dest_ip": "10.0.0.5",
      "port": "8080",
      "status": "active"
    },
    "operator": "admin"
  }'
```

```python
import requests

response = requests.post("http://localhost:8088/api/records", json={
    "record": {
        "project": "订单系统",
        "env": "uat",
        "vid": "VLAN100",
        "src_ip": "192.168.1.10",
        "dest_ip": "10.0.0.5",
        "port": "8080",
        "status": "active"
    },
    "operator": "admin"
})
```

---

### 5. 批量添加记录

```http
POST /api/records/batch
Content-Type: application/json
```

**请求体：**
```json
{
  "records": [
    {
      "project": "订单系统",
      "env": "uat",
      "vid": "VLAN100",
      "src_ip": "192.168.1.10",
      "dest_ip": "10.0.0.5",
      "port": "8080",
      "status": "active"
    },
    {
      "project": "支付系统",
      "env": "prod",
      "vid": "VLAN200",
      "src_ip": "192.168.1.20",
      "dest_ip": "10.0.0.6",
      "port": "443",
      "status": "active"
    }
  ],
  "operator": "admin"
}
```

---

### 6. 更新记录

```http
PUT /api/records/{id}
Content-Type: application/json
```

**请求体：**
```json
{
  "record": {
    "project": "订单系统",
    "env": "prod",
    "vid": "VLAN100",
    "src_ip": "192.168.1.10",
    "dest_ip": "10.0.0.5",
    "port": "8080",
    "status": "active"
  },
  "operator": "admin"
}
```

---

### 7. 删除记录

```http
DELETE /api/records/{id}
Content-Type: application/json
```

**请求体：**
```json
{
  "operator": "admin"
}
```

---

### 8. 导出记录（CSV）

```http
GET /api/records/export
```

**查询参数（可选）：**
- `env` - 环境过滤
- `status` - 状态过滤
- `search` - 搜索关键词

**调用示例：**
```bash
# 导出所有记录
curl -o records.csv "http://localhost:8088/api/records/export"

# 导出生产环境记录
curl -o prod_records.csv "http://localhost:8088/api/records/export?env=prod"
```

---

### 9. 获取审计日志

```http
GET /api/audit-logs
```

---

### 10. 导出审计日志（CSV）

```http
GET /api/audit-logs/export
```

---

## 字段说明

### Record（记录）

| 字段 | 类型 | 说明 |
|------|------|------|
| `id` | string | 记录ID |
| `project` | string | 项目名称 |
| `env` | string | 环境（uat/prod） |
| `vid` | string | VLAN ID |
| `src_ip` | string | 源 IP |
| `dest_ip` | string | 目标 IP |
| `port` | string | 端口 |
| `status` | string | 状态（active/inactive/pending） |
| `created_at` | string | 创建时间 |
| `updated_at` | string | 更新时间 |
| `created_by` | string | 创建人 |
| `updated_by` | string | 更新人 |

---

## AI 调用建议

如果你要通过 AI（如 GPT、Claude）调用此接口获取网络配置信息，推荐使用 `/api/records/query` 接口：

```
用户问：订单系统在生产环境的网络配置是什么？

AI 调用：
GET http://localhost:8088/api/records/query?project=订单&env=prod

返回结果包含：
- 源 IP (src_ip)
- 目标 IP (dest_ip)  
- 端口 (port)
- VID
- 状态
```

这样 AI 就能获取到结构化的网络配置信息并回答用户问题。






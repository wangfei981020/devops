# Database Initialization

## 概述

本目录包含数据库初始化脚本。

## 初始化脚本

### `init_schema.sql`

这是一个**完整的初始化脚本**，适用于**全新的数据库部署**。它包含：

- 所有表的创建语句
- 所有索引和约束
- 测试数据插入

## 使用方法

### 方法 1: 手动执行（推荐）

```bash
# 使用 psql 直接执行
psql -h localhost -U your_user -d your_database -f init_schema.sql

# 在 Docker 容器中执行
docker exec -i video-manager-db psql -U videomanager -d videomanager < init_schema.sql
```

### 方法 2: 自动执行（应用启动时）

应用启动时会自动检测：
- 如果数据库为空（没有迁移记录），会自动执行 `init_schema.sql`
- 如果数据库已有数据，会跳过初始化

## 脚本特性

- **幂等性**：脚本是幂等的，可以安全地多次运行
- **安全性**：使用 `IF NOT EXISTS` 和 `ON CONFLICT DO NOTHING` 确保不会重复创建或插入
- **完整性**：包含所有表结构、索引、约束和测试数据

## 清空和重新生成测试数据

如果需要清空所有测试数据并重新生成：

```bash
# 手动执行 SQL
psql -h localhost -U your_user -d your_database << EOF
DELETE FROM video_stream_endpoints;
DELETE FROM stream_paths;
DELETE FROM streams;
DELETE FROM domains;
DELETE FROM cdn_lines;
DELETE FROM cdn_providers;
DELETE FROM tokens;
DELETE FROM schema_migrations;
EOF

# 然后重启应用，会自动重新执行 init_schema.sql
```

## 注意事项

1. **备份数据**：在生产环境执行前，请务必备份数据库
2. **测试环境验证**：先在测试环境验证脚本
3. **管理员账号**：管理员账号通过应用代码创建（使用 `ADMIN_USERNAME` 和 `ADMIN_PASSWORD` 环境变量），不在 SQL 脚本中
4. **新部署专用**：此脚本仅适用于全新部署，不适用于已有系统的升级


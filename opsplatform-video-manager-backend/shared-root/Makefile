# Copyright (c) 2025 kk
#
# This software is released under the MIT License.
# https://opensource.org/licenses/MIT

.PHONY: help up down logs build restart clean migrate

help: ## 显示帮助信息
	@echo "可用命令:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

up: ## 启动所有服务
	docker-compose up -d

up-logs: ## 启动所有服务并显示日志
	docker-compose up

down: ## 停止所有服务
	docker-compose down

logs: ## 查看所有服务日志
	docker-compose logs -f

logs-backend: ## 查看后端日志
	docker-compose logs -f backend

logs-frontend: ## 查看前端日志
	docker-compose logs -f frontend

logs-db: ## 查看数据库日志
	docker-compose logs -f postgres

build: ## 构建所有服务
	docker-compose build

restart: ## 重启所有服务
	docker-compose restart

restart-backend: ## 重启后端服务
	docker-compose restart backend

restart-frontend: ## 重启前端服务
	docker-compose restart frontend

clean: ## 清理所有容器和卷（危险操作）
	docker-compose down -v

db-only: ## 只启动数据库
	docker-compose up -d postgres

migrate: ## 运行数据库迁移（需要在容器内或本地有 migrate 工具）
	docker-compose exec backend sh -c "cd /app && migrate -path migrations -database 'postgres://videomanager:videomanager@postgres:5432/videomanager?sslmode=disable' up" || echo "请确保已安装 golang-migrate 工具"

shell-backend: ## 进入后端容器
	docker-compose exec backend sh

shell-frontend: ## 进入前端容器
	docker-compose exec frontend sh

shell-db: ## 进入数据库容器
	docker-compose exec postgres psql -U videomanager -d videomanager

# OpenSpec commands
openspec-list: ## 列出所有变更
	openspec list

openspec-list-specs: ## 列出所有规范
	openspec list --specs

openspec-show: ## 显示变更或规范详情（需要指定名称，如: make openspec-show CHANGE=add-cdn-line-management）
	@if [ -z "$(CHANGE)" ]; then \
		echo "用法: make openspec-show CHANGE=<change-id>"; \
		echo "示例: make openspec-show CHANGE=add-cdn-line-management"; \
	else \
		openspec show $(CHANGE); \
	fi

openspec-validate: ## 验证变更或规范（需要指定名称，如: make openspec-validate CHANGE=add-cdn-line-management）
	@if [ -z "$(CHANGE)" ]; then \
		echo "用法: make openspec-validate CHANGE=<change-id>"; \
		echo "示例: make openspec-validate CHANGE=add-cdn-line-management"; \
		echo "或使用: make openspec-validate-all 验证所有"; \
	else \
		openspec validate $(CHANGE) --strict; \
	fi

openspec-validate-all: ## 验证所有变更和规范
	openspec validate --all --strict

openspec-update: ## 更新 OpenSpec 指令文件
	openspec update


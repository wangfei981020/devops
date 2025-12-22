# K8s 部署脚本

Write-Host "=== 构建 Docker 镜像 ===" -ForegroundColor Green

# 构建后端镜像
Write-Host "构建后端镜像..." -ForegroundColor Yellow
docker build -t opsplatform-backend:latest ../backend

# 构建前端镜像
Write-Host "构建前端镜像..." -ForegroundColor Yellow
docker build -t opsplatform-frontend:latest ../frontend

Write-Host "`n=== 部署到 Kubernetes ===" -ForegroundColor Green

# 创建命名空间
Write-Host "创建命名空间..." -ForegroundColor Yellow
kubectl apply -f namespace.yaml

# 部署 MySQL
Write-Host "部署 MySQL..." -ForegroundColor Yellow
kubectl apply -f mysql.yaml

# 等待 MySQL 就绪
Write-Host "等待 MySQL 就绪..." -ForegroundColor Yellow
kubectl wait --for=condition=ready pod -l app=mysql -n opsplatform --timeout=120s

# 部署后端
Write-Host "部署后端..." -ForegroundColor Yellow
kubectl apply -f backend.yaml

# 部署前端
Write-Host "部署前端..." -ForegroundColor Yellow
kubectl apply -f frontend.yaml

Write-Host "`n=== 部署完成 ===" -ForegroundColor Green
Write-Host "访问地址: http://localhost:30080" -ForegroundColor Cyan
Write-Host "`n查看 Pod 状态:" -ForegroundColor Yellow
kubectl get pods -n opsplatform


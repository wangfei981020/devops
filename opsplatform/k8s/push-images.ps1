# 推送镜像到 GitHub Container Registry
# 使用前请先登录: echo YOUR_TOKEN | docker login ghcr.io -u YOUR_USERNAME --password-stdin

param(
    [string]$Username = "wangfei981020",
    [string]$Version = "latest"
)

$Registry = "ghcr.io"
$Repo = "$Registry/$Username"

Write-Host "=== 推送镜像到 GitHub Container Registry ===" -ForegroundColor Green

# 标记后端镜像
Write-Host "标记后端镜像..." -ForegroundColor Yellow
docker tag opsplatform-backend:latest "$Repo/opsplatform-backend:$Version"

# 标记前端镜像
Write-Host "标记前端镜像..." -ForegroundColor Yellow
docker tag opsplatform-frontend:latest "$Repo/opsplatform-frontend:$Version"

# 推送后端镜像
Write-Host "推送后端镜像..." -ForegroundColor Yellow
docker push "$Repo/opsplatform-backend:$Version"

# 推送前端镜像
Write-Host "推送前端镜像..." -ForegroundColor Yellow
docker push "$Repo/opsplatform-frontend:$Version"

Write-Host "`n=== 推送完成 ===" -ForegroundColor Green
Write-Host "后端镜像: $Repo/opsplatform-backend:$Version" -ForegroundColor Cyan
Write-Host "前端镜像: $Repo/opsplatform-frontend:$Version" -ForegroundColor Cyan



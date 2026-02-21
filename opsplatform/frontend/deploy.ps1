# 前端部署脚本
# 自动更新版本号并部署到 K8s

param(
    [string]$PodName = "",
    [string]$Namespace = "opsplatform"
)

# 获取 Pod 名称
if (-not $PodName) {
    $PodName = kubectl get pods -n $Namespace -l app=opsplatform-frontend -o jsonpath="{.items[0].metadata.name}"
    if (-not $PodName) {
        Write-Error "无法获取前端 Pod 名称"
        exit 1
    }
}

Write-Host "目标 Pod: $PodName" -ForegroundColor Cyan

# 生成版本号（时间戳）
$Version = Get-Date -Format "yyyyMMddHHmmss"
Write-Host "生成版本号: v$Version" -ForegroundColor Green

# 更新 index.html 中的版本号
$IndexPath = Join-Path $PSScriptRoot "index.html"
$Content = Get-Content $IndexPath -Raw

# 替换所有 ?v=xxx 为新版本号
$NewContent = $Content -replace '\?v=\d+', "?v=$Version"
Set-Content $IndexPath -Value $NewContent -NoNewline

Write-Host "已更新 index.html 版本号" -ForegroundColor Green

# 复制文件到 Pod
Write-Host "正在复制文件到 Pod..." -ForegroundColor Cyan

$FilesToCopy = @(
    @{Local = "index.html"; Remote = "/usr/share/nginx/html/index.html"},
    @{Local = "js/api.js"; Remote = "/usr/share/nginx/html/js/api.js"},
    @{Local = "js/app.js"; Remote = "/usr/share/nginx/html/js/app.js"},
    @{Local = "css/style.css"; Remote = "/usr/share/nginx/html/css/style.css"},
    @{Local = "css/style-blueking.css"; Remote = "/usr/share/nginx/html/css/style-blueking.css"}
)

foreach ($file in $FilesToCopy) {
    $LocalPath = Join-Path $PSScriptRoot $file.Local
    if (Test-Path $LocalPath) {
        kubectl cp $LocalPath "${Namespace}/${PodName}:$($file.Remote)"
        Write-Host "  已复制: $($file.Local)" -ForegroundColor Gray
    }
}

Write-Host "`n部署完成！版本: v$Version" -ForegroundColor Green
Write-Host "用户刷新页面即可获取最新版本（无需清缓存）" -ForegroundColor Yellow

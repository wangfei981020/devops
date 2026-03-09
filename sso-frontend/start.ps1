# SSO Frontend 启动脚本
Write-Host "=== SSO Frontend ===" -ForegroundColor Cyan
Write-Host "正在启动前端服务..." -ForegroundColor Green
Write-Host "端口: 9001" -ForegroundColor White
Write-Host "访问: http://localhost:9001" -ForegroundColor White
Write-Host ""

# 使用 Python 启动简单 HTTP 服务
python -m http.server 9001

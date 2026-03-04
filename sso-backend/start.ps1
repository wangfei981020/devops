# SSO Backend 启动脚本
Write-Host "=== SSO Backend ===" -ForegroundColor Cyan
Write-Host "正在下载依赖..." -ForegroundColor Yellow

# 下载依赖
go mod tidy

Write-Host ""
Write-Host "正在启动 SSO Server..." -ForegroundColor Green
Write-Host "端口: 9000" -ForegroundColor White
Write-Host "Issuer: http://localhost:9000" -ForegroundColor White
Write-Host ""
Write-Host "测试用户:" -ForegroundColor Yellow
Write-Host "  admin / admin123" -ForegroundColor White
Write-Host "  user1 / user123" -ForegroundColor White
Write-Host "  user2 / user123" -ForegroundColor White
Write-Host ""

# 启动服务
go run main.go

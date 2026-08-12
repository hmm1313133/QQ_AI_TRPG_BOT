# QQ AI TRPG Bot 构建脚本 (PowerShell)
# 用法: .\build.ps1
# 流程: 先构建前端（Vue3 + Vite -> internal/web/static/dist），再编译 Go 程序。
# 说明: go:embed 依赖 dist 产物，因此必须先生成前端再编译 Go。

$ErrorActionPreference = "Stop"

Set-Location $PSScriptRoot

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  QQ AI TRPG Bot 构建脚本" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

# 1. 准备 Node 环境（优先系统 Node，其次项目便携版 tools/node）
$portableNode = Join-Path $PSScriptRoot "tools\node"
if (Get-Command node -ErrorAction SilentlyContinue) {
    Write-Host "[INFO] 使用系统 Node: $(node -v)" -ForegroundColor Yellow
} elseif (Test-Path (Join-Path $portableNode "node.exe")) {
    $env:PATH = "$portableNode;$env:PATH"
    Write-Host "[INFO] 使用便携 Node: $(node -v)" -ForegroundColor Yellow
} else {
    Write-Host "[ERROR] 未找到 Node.js。请安装 Node 18+，或将便携版解压到 tools/node/" -ForegroundColor Red
    exit 1
}

# 2. 构建前端
Write-Host ""
Write-Host "[INFO] 构建前端..." -ForegroundColor Yellow
Push-Location (Join-Path $PSScriptRoot "frontend")
if (-not (Test-Path "node_modules")) {
    Write-Host "[INFO] 首次构建，安装依赖..." -ForegroundColor Yellow
    npm install
}
npm run build
if ($LASTEXITCODE -ne 0) {
    Pop-Location
    Write-Host "[ERROR] 前端构建失败" -ForegroundColor Red
    exit 1
}
Pop-Location
Write-Host "[OK] 前端构建完成 -> internal/web/static/dist" -ForegroundColor Green

# 3. 编译 Go 程序
Write-Host ""
Write-Host "[INFO] 编译 Go 程序..." -ForegroundColor Yellow
go build -o bot.exe ./cmd/bot
if ($LASTEXITCODE -ne 0) {
    Write-Host "[ERROR] Go 编译失败" -ForegroundColor Red
    exit 1
}
Write-Host "[OK] Go 编译完成 -> bot.exe" -ForegroundColor Green

Write-Host ""
Write-Host "构建成功。运行 .\start.ps1 启动（或 go run ./cmd/bot）。" -ForegroundColor Cyan

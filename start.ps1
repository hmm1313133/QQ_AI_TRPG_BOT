# QQ AI TRPG Bot 启动脚本 (PowerShell)
# 用法: .\start.ps1

$ErrorActionPreference = "Stop"

# 切换到脚本所在目录（项目根目录）
Set-Location $PSScriptRoot

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  QQ AI TRPG Bot 启动脚本" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

# 读取 .env 文件并设置环境变量
$envFile = Join-Path $PSScriptRoot ".env"
if (Test-Path $envFile) {
    Write-Host "[INFO] 正在加载 .env 文件..." -ForegroundColor Yellow
    Get-Content $envFile | ForEach-Object {
        $line = $_.Trim()
        # 跳过空行和注释行
        if ($line -and -not $line.StartsWith("#")) {
            $parts = $line -split '=', 2
            if ($parts.Length -eq 2) {
                $key = $parts[0].Trim()
                $value = $parts[1].Trim()
                [System.Environment]::SetEnvironmentVariable($key, $value, "Process")
                Write-Host "  > $key = (已设置)" -ForegroundColor Green
            }
        }
    }
} else {
    Write-Host "[WARN] 未找到 .env 文件，请确认环境变量已手动设置" -ForegroundColor Red
}

Write-Host ""
Write-Host "[INFO] 正在启动 Bot..." -ForegroundColor Yellow
Write-Host ""

# 启动项目
go run cmd/bot/main.go

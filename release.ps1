# QQ AI TRPG Bot 一键发布打包脚本
# 用法: .\release.ps1
# 产物: dist/qq-ai-trpg-bot-<version>-windows-amd64.zip

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

$version = git describe --tags --always --dirty 2>$null
if (-not $version) { $version = "dev" }
$releaseName = "qq-ai-trpg-bot-$version-windows-amd64"
$workDir = Join-Path $PSScriptRoot "dist\$releaseName"
$zipPath = Join-Path $PSScriptRoot "dist\$releaseName.zip"

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  QQ AI TRPG Bot 发布打包" -ForegroundColor Cyan
Write-Host "  版本: $version" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

# 1. 准备 Node 环境
$portableNode = Join-Path $PSScriptRoot "tools\node"
if (Get-Command node -ErrorAction SilentlyContinue) {
    Write-Host "[INFO] 使用系统 Node: $(node -v)" -ForegroundColor Yellow
} elseif (Test-Path (Join-Path $portableNode "node.exe")) {
    $env:PATH = "$portableNode;$env:PATH"
    Write-Host "[INFO] 使用便携 Node: $(node -v)" -ForegroundColor Yellow
} else {
    Write-Host "[ERROR] 未找到 Node.js" -ForegroundColor Red
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
Write-Host "[OK] 前端构建完成" -ForegroundColor Green

# 3. 编译 Go 程序
Write-Host ""
Write-Host "[INFO] 编译 Go 程序..." -ForegroundColor Yellow
go build -ldflags "-s -w" -o bot.exe ./cmd/bot
if ($LASTEXITCODE -ne 0) {
    Write-Host "[ERROR] Go 编译失败" -ForegroundColor Red
    exit 1
}
Write-Host "[OK] Go 编译完成 -> bot.exe" -ForegroundColor Green

# 4. 准备发布目录
Write-Host ""
Write-Host "[INFO] 组装发布包..." -ForegroundColor Yellow
if (Test-Path $workDir) { Remove-Item -Recurse -Force $workDir }
New-Item -ItemType Directory -Force -Path $workDir | Out-Null

#   bot.exe
Copy-Item "bot.exe" $workDir

#   conf/
New-Item -ItemType Directory -Force -Path "$workDir\conf" | Out-Null
Copy-Item "conf\trpc_go.yaml" "$workDir\conf\"

#   .env.example
Copy-Item ".env.example" "$workDir\.env.example"

#   data/ 空目录（运行时自动创建数据库）
New-Item -ItemType Directory -Force -Path "$workDir\data" | Out-Null
New-Item -ItemType Directory -Force -Path "$workDir\data\worlds" | Out-Null
New-Item -ItemType Directory -Force -Path "$workDir\logs" | Out-Null

#   一键启动脚本
$startScript = @'
@echo off
chcp 65001 >nul 2>&1
title QQ AI TRPG Bot

echo ========================================
echo   QQ AI TRPG Bot
echo ========================================
echo.

REM 首次运行：从 .env.example 复制 .env
if not exist ".env" (
    echo [INFO] 首次运行，从 .env.example 创建 .env ...
    copy .env.example .env >nul
    echo [WARN] 请编辑 .env 填入 LLM_API_KEY 后重新运行
    echo.
    notepad .env
    exit /b 0
)

REM 加载 .env 环境变量
for /f "usebackq tokens=1,* delims==" %%a in (".env") do (
    set "line=%%a"
    if not "%%a"=="" if not "%%a:~0,1%"=="#" (
        set "%%a=%%b"
    )
)

echo [INFO] 正在启动...
echo [INFO] Web 界面: http://localhost:8080
echo [INFO] 按 Ctrl+C 停止
echo.

bot.exe
pause
'@
$utf8NoBom = New-Object System.Text.UTF8Encoding $false
[System.IO.File]::WriteAllText("$workDir\start.bat", $startScript, $utf8NoBom)

#   PowerShell 启动脚本
$startPsScript = @'
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot
chcp 65001 | Out-Null

Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  QQ AI TRPG Bot" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

# 首次运行：从 .env.example 复制 .env
$envFile = Join-Path $PSScriptRoot ".env"
if (-not (Test-Path $envFile)) {
    Write-Host "[INFO] 首次运行，从 .env.example 创建 .env ..." -ForegroundColor Yellow
    Copy-Item ".env.example" ".env"
    Write-Host "[WARN] 请编辑 .env 填入 LLM_API_KEY 后重新运行" -ForegroundColor Red
    Write-Host "[INFO] 正在用记事本打开 .env ..." -ForegroundColor Yellow
    Start-Process notepad ".env" -Wait
    exit 0
}

# 加载 .env
Get-Content $envFile | ForEach-Object {
    $line = $_.Trim()
    if ($line -and -not $line.StartsWith("#")) {
        $parts = $line -split '=', 2
        if ($parts.Length -eq 2) {
            [System.Environment]::SetEnvironmentVariable($parts[0].Trim(), $parts[1].Trim(), "Process")
        }
    }
}

Write-Host ""
Write-Host "[INFO] 正在启动..." -ForegroundColor Yellow
Write-Host "[INFO] Web 界面: http://localhost:8080" -ForegroundColor Green
Write-Host "[INFO] 按 Ctrl+C 停止" -ForegroundColor Yellow
Write-Host ""

& "$PSScriptRoot\bot.exe"
'@
[System.IO.File]::WriteAllText("$workDir\start.ps1", $startPsScript, $utf8NoBom)

#   快速开始说明
$readme = @'
# QQ AI TRPG Bot - 快速开始

## 系统要求
- Windows 10/11 (64位)
- 无需安装任何依赖，解压即用

## 快速开始

1. 解压压缩包到任意目录
2. 双击 `start.bat`（或右键用 PowerShell 运行 `start.ps1`）
3. 首次运行会自动创建 `.env` 配置文件并打开记事本
4. 在 `.env` 中填入你的 DeepSeek API Key：
   ```
   LLM_API_KEY=sk-your-key-here
   ```
5. 保存关闭记事本，再次双击 `start.bat` 启动
6. 浏览器打开 http://localhost:8080 即可使用

## 配置说明

### .env 文件（主要配置）
| 配置项 | 说明 | 默认值 |
|--------|------|--------|
| LLM_API_KEY | DeepSeek API 密钥（必填） | - |
| LLM_MODEL | 模型名称 | deepseek-flash |
| LLM_BASE_URL | API 地址 | https://api.deepseek.com |
| WEB_CHAT_TOKEN | 聊天页访问令牌（留空不校验） | - |
| ADMIN_TOKEN | 管理后台令牌（留空仅允许本机访问） | - |

### conf/trpc_go.yaml（服务端口等）
- Web 服务端口默认 8080，如需修改改 `server.service.port`
- 日志级别默认 debug

## 目录结构
```
qq-ai-trpg-bot/
  start.bat          # 一键启动（推荐）
  start.ps1          # PowerShell 启动脚本
  bot.exe            # 主程序
  .env.example       # 配置模板
  .env               # 你的配置（首次运行自动生成）
  conf/
    trpc_go.yaml     # 服务配置
  data/              # 运行时数据（自动创建）
  logs/              # 日志文件
```

## 获取 DeepSeek API Key
1. 访问 https://platform.deepseek.com/
2. 注册并创建 API Key
3. 粘贴到 .env 的 LLM_API_KEY=

## 常见问题

Q: 启动后无法访问 http://localhost:8080
A: 检查端口是否被占用，修改 conf/trpc_go.yaml 中的 port

Q: AI 不回复
A: 检查 .env 中 LLM_API_KEY 是否正确

Q: 如何重置数据
A: 删除 data/ 目录下的 .db 文件后重启
'@
[System.IO.File]::WriteAllText("$workDir\README.txt", $readme, $utf8NoBom)

Write-Host "[OK] 发布目录组装完成" -ForegroundColor Green

# 5. 打包 ZIP（bot.exe 可能被杀毒软件短暂锁定，自动重试）
Write-Host ""
Write-Host "[INFO] 正在打包 ZIP..." -ForegroundColor Yellow
if (Test-Path $zipPath) { Remove-Item -Force $zipPath }
$maxRetries = 5
for ($i = 1; $i -le $maxRetries; $i++) {
    try {
        Compress-Archive -Path "$workDir\*" -DestinationPath $zipPath -CompressionLevel Optimal -ErrorAction Stop
        break
    } catch {
        if ($i -eq $maxRetries) { throw }
        Write-Host "[WARN] 打包失败($i/$maxRetries)，等待 3 秒重试..." -ForegroundColor Yellow
        Start-Sleep -Seconds 3
    }
}
Write-Host "[OK] 打包完成 -> $zipPath" -ForegroundColor Green

# 6. 清理工作目录
Remove-Item -Recurse -Force $workDir

# 输出信息
Write-Host ""
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  发布打包完成" -ForegroundColor Green
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  产物: $zipPath" -ForegroundColor White
Write-Host "  大小: $([math]::Round((Get-Item $zipPath).Length / 1MB, 2)) MB" -ForegroundColor White
Write-Host ""

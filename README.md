# QQ AI TRPG BOT

基于 **trpc-go** 框架构建，集成 **trpc-agent-go** 的 AI 化 QQ 机器人框架，专注于 TRPG（桌上角色扮演游戏）场景。

## 项目简介

本项目是一个将腾讯高性能微服务框架 [trpc-go](https://github.com/trpc-group/trpc-go) 与 AI Agent 框架 [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) 相结合的 QQ 机器人框架。它能够在 QQ 群聊/私聊中提供沉浸式的 TRPG 游戏体验，包括：

- 🎲 **骰子与规则引擎** — 支持常见 TRPG 规则集（如 CoC、DnD）的骰点与判定
- 🧠 **AI 主持游戏（KP/DM）** — 由 trpc-agent-go 驱动的智能 AI 充当游戏主持人
- 📖 **角色卡管理** — 创建、保存和管理玩家角色卡
- 🗺️ **剧本/模组管理** — 加载和运行自定义 TRPG 剧本
- 💬 **多群组会话隔离** — 每个 QQ 群拥有独立的游戏状态和上下文

## 技术栈

| 组件 | 技术 | 说明 |
|------|------|------|
| 微服务框架 | [trpc-go](https://github.com/trpc-group/trpc-go) | 腾讯开源高性能 Go RPC 框架 |
| AI Agent | [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) | 基于 trpc-go 的 AI Agent 框架 |
| QQ 协议 | OneBot / go-cqhttp | 通过 WebSocket / HTTP 对接 QQ |
| 语言 | Go 1.21+ | — |

## 项目结构

```
QQ_AI_TRPG_BOT-1/
├── cmd/                      # 程序入口
│   └── bot/
│       └── main.go           # 主程序入口
├── internal/                 # 内部业务逻辑
│   ├── bot/                  # QQ 机器人连接与消息分发
│   ├── agent/                # trpc-agent-go AI Agent 封装
│   ├── trpg/                 # TRPG 游戏核心引擎
│   │   ├── dice/             # 骰子与规则引擎
│   │   ├── character/        # 角色卡管理
│   │   └── module/           # 剧本/模组管理
│   └── store/                # 数据持久化
├── pkg/                      # 可复用的公共包
├── conf/                     # 配置文件目录
│   └── trpc_go.yaml          # trpc-go 服务配置
├── deployments/              # 部署相关文件
├── go.mod
├── go.sum
└── README.md
```

## 快速开始

### 环境要求

- Go >= 1.21
- 一个已部署的 QQ 协议端（如 [Lagrange.OneBot](https://github.com/LagrangeDev/Lagrange.Core) 或 [go-cqhttp](https://github.com/Mrs4s/go-cqhttp)）
- LLM API 密钥（支持 OpenAI、腾讯混元等）

### 安装

```bash
# 克隆项目
git clone <repository-url>
cd QQ_AI_TRPG_BOT-1

# 安装依赖
go mod tidy
```

### 配置

1. 编辑 `conf/trpc_go.yaml`，配置 QQ 协议端连接信息和 LLM 服务。
2. 根据需要修改 `conf/` 下的其他配置文件。

### 运行

```bash
# 编译并运行
go run ./cmd/bot/

# 或构建二进制
go build -o bin/bot ./cmd/bot/
./bin/bot
```

## 核心功能设计

### AI Agent 架构

本项目利用 trpc-agent-go 提供的能力，构建多层 Agent：

```
┌─────────────────────────────────────┐
│          QQ 消息入口层              │
│     (WebSocket / HTTP 接收消息)      │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│        消息分发与路由层             │
│  (指令解析 / 意图识别 / 上下文管理)  │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│         AI Agent 层                 │
│  ┌───────────┐  ┌────────────────┐  │
│  │  KP/DM    │  │  规则裁判 Agent │  │
│  │  主持Agent │  │  (骰点/判定)   │  │
│  └───────────┘  └────────────────┘  │
│  ┌───────────────────────────────┐  │
│  │       剧情生成 Agent          │  │
│  └───────────────────────────────┘  │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│          TRPG 引擎层                │
│  (角色卡 / 规则集 / 剧本状态机)      │
└─────────────────────────────────────┘
```

### 指令示例

| 指令 | 说明 |
|------|------|
| `.r d100` | 投掷一个 100 面骰 |
| `.r 3d6` | 投掷 3 个 6 面骰 |
| `.coc 创建角色` | 创建一张 CoC 7th 角色卡 |
| `.kp 开始模组 [名称]` | AI 主持开始一个 TRPG 模组 |
| `.kp [行动描述]` | 描述玩家行动，AI 主持推进剧情 |

## 开发指南

### 添加新的 TRPG 规则集

在 `internal/trpg/` 下创建新的规则包，实现 `RuleSet` 接口：

```go
type RuleSet interface {
    Name() string
    Roll(diceExpr string) (*RollResult, error)
    Check(action string, char *Character) (*CheckResult, error)
}
```

### 扩展 Agent 能力

利用 trpc-agent-go 的工具（Tool）机制注册自定义能力：

```go
agent.WithTools(
    trpgtool.NewDiceTool(),
    trpgtool.NewCharacterTool(),
)
```

## 部署

支持 Docker 部署，详见 `deployments/` 目录。

## 开源协议

本项目仅供学习和研究使用。

## 致谢

- [trpc-go](https://github.com/trpc-group/trpc-go) — 腾讯高性能 RPC 框架
- [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) — AI Agent 框架
- [go-cqhttp](https://github.com/Mrs4s/go-cqhttp) — QQ 协议端实现

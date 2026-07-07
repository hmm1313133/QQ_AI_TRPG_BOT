# QQ AI TRPG BOT

> 基于 **trpc-go** + **trpc-agent-go** 构建的 QQ 官方机器人 TRPG 跑团框架，集成 AI 主持人（KP/DM）能力。

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev)
[![Version](https://img.shields.io/badge/version-0.1.0-blue)](pkg/version.go)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

## 项目简介

本项目将腾讯微服务框架 [trpc-go](https://github.com/trpc-group/trpc-go) 与 AI Agent 框架 [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) 相结合，通过 **QQ 官方机器人 API v2** 对接 QQ 群聊/私聊/频道，提供沉浸式 TRPG（桌上角色扮演游戏）体验。

核心能力：

- 🎲 **骰子引擎** — 自研递归下降解析器，支持复杂表达式（`3d6`、`1d100+5`、`4d6kh3`、`2d6!`爆炸骰、`(1d6+3)*2`）
- 🧠 **AI 主持人** — 由 trpc-agent-go + DeepSeek 驱动的 KP/DM Agent，可描述场景、扮演 NPC、推进剧情、自动判定
- 📖 **双规则集** — 内置 **CoC 7版**（克苏鲁的呼唤）与 **DnD 5e**（龙与地下城）完整规则实现
- 📇 **角色卡管理** — JSON 文件持久化，支持属性/技能/状态录入、多角色切换、群绑定
- 💬 **多场景隔离** — 每个群/私聊/频道拥有独立会话状态、规则集和角色绑定
- 📝 **跑团日志** — TRPG 模式下自动记录全部对话，支持查看与导出

## 技术栈

| 组件 | 技术 | 说明 |
|------|------|------|
| 微服务框架 | [trpc-go](https://github.com/trpc-group/trpc-go) `v1.0.3` | 腾讯开源高性能 Go RPC 框架 |
| AI Agent | [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) `v1.10.0` | 基于 trpc-go 的 LLM Agent 框架 |
| LLM 模型 | [DeepSeek](https://platform.deepseek.com/) | 默认 `deepseek-chat`，兼容 OpenAI 接口 |
| QQ 协议 | QQ 官方 Bot API v2 | WebSocket 事件订阅 + OpenAPI HTTP 调用 |
| WebSocket | [nhooyr.io/websocket](https://github.com/coder/websocket) `v1.8.17` | 纯 Go WebSocket 客户端 |
| 语言 | Go 1.21+ | — |

## 架构设计

项目采用 **四层架构**，通过 Service 统一数据源，实现 Handler（确定性指令）与 Agent（AI 对话）的协作联动：

```
┌──────────────────────────────────────────────┐
│            QQ 消息入口层 (bot)                │
│  群聊@消息 / 群全量消息 / 单聊 / 频道消息       │
│  消息去重 → MessageContext → 路由分发           │
└───────────────────┬──────────────────────────┘
                    │
          ┌─────────┴─────────┐
          │                   │
          ▼                   ▼
┌─────────────────┐  ┌─────────────────────┐
│  功能层 Handler  │  │   Agent 层 (AI)     │
│  确定性指令处理   │  │  KP/DM 主持人 Agent │
│  骰子/角色卡/规则 │  │  LLM 对话 + 工具调用 │
└────────┬────────┘  └──────────┬──────────┘
         │                      │
         └──────────┬───────────┘
                    ▼
┌──────────────────────────────────────────────┐
│           Service 统一服务层 (trpg)           │
│  规则集管理 / 角色卡操作 / 骰子投掷 / 检定     │
│  单一数据源: Handler 和 Agent 共享同一实例     │
└───────────────────┬──────────────────────────┘
                    ▼
┌──────────────────────────────────────────────┐
│          Session 联动层 (core)               │
│  会话状态 / 模式切换 / 跨层数据共享            │
│  normal → 仅指令 | trpg → AI+指令+日志        │
│  freechat → 全部交给 AI                       │
└──────────────────────────────────────────────┘
```

### 三种会话模式

| 模式 | 指令处理 | AI 对话 | 自动日志 | 说明 |
|------|---------|---------|---------|------|
| `normal` | ✅ | ❌ | ❌ | 默认模式，仅响应 `.` 开头的指令 |
| `trpg` | ✅ | ✅ | ✅ | 跑团模式，非指令消息交给 AI KP，自动记录日志 |
| `freechat` | ✅ | ✅ | ❌ | 自由对话模式，所有非指令消息交给 AI |

## 项目结构

```
QQ_AI_TRPG_BOT-1/
├── cmd/bot/main.go                  # 程序入口，组件初始化与注册
├── internal/
│   ├── bot/bot.go                   # QQ 消息入口层、路由分发、回复
│   ├── agent/
│   │   ├── agent.go                 # Agent 配置、Manager、默认系统提示词
│   │   ├── kp_agent.go              # KP/DM Agent 实现 (trpc-agent-go + DeepSeek)
│   │   └── tools.go                 # AI FunctionTools (5个工具)
│   ├── core/
│   │   ├── context.go               # MessageContext、Session、SessionManager
│   │   ├── handler.go               # Handler / AgentHandler 接口定义
│   │   ├── hook.go                  # Hook 接口 (跨切面关注点)
│   │   └── plugin.go                # PluginManager (注册中心)
│   ├── handler/
│   │   ├── dice.go                  # .r/.rh 骰子 + .help 帮助
│   │   ├── coc.go                   # .ra/.rah/.sc/.en/.coc/.ti/.li/.setcoc/.rav
│   │   ├── dnd.go                   # .rc/.dnd/.ri/.init/.ds/.longrest
│   │   ├── character.go             # .pc/.nn/.st 角色卡管理
│   │   ├── ruleset.go               # .set 规则集/默认骰子切换
│   │   ├── mode.go                  # .mode 会话模式切换
│   │   └── log.go                   # .log 跑团日志
│   ├── store/store.go               # 数据存储接口 (Memory/SQLite)
│   └── trpg/
│       ├── engine.go                # TRPG 引擎 (会话/规则集/角色绑定)
│       ├── service.go               # 统一游戏服务层 (Handler+Agent共用)
│       ├── character/character.go   # 角色卡管理 (JSON持久化)
│       ├── dice/                    # 骰子引擎 (parser + evaluator AST)
│       │   ├── dice.go              #   Roll() 入口
│       │   ├── parser.go            #   递归下降解析器
│       │   └── evaluator.go         #   AST 求值器
│       ├── gamelog/gamelog.go       # 跑团日志记录器 (实现 core.Hook)
│       ├── module/module.go         # 模组/剧本管理 (框架)
│       └── ruleset/
│           ├── ruleset.go           # RuleSet 接口定义
│           ├── coc7/                # CoC 7版规则实现
│           │   ├── coc7.go          #   技能检定/SAN/成长/对抗/疯狂
│           │   └── tables.go        #   疯狂症状表/恐惧症/躁狂症
│           └── dnd5e/dnd5e.go       # DnD 5e规则实现
├── pkg/
│   ├── version.go                   # 版本号常量
│   └── qqbot/                       # QQ 官方 Bot 协议封装
│       ├── client.go                #   高层客户端 (整合 OpenAPI + WS)
│       ├── auth.go                  #   AccessToken 管理 (获取+自动刷新)
│       ├── openapi.go               #   HTTP API 客户端
│       ├── websocket.go             #   WS 客户端 (连接/鉴权/心跳/断线Resume)
│       ├── events.go                #   事件分发器
│       ├── message.go               #   消息发送 API
│       └── types.go                 #   常量与结构体定义
├── conf/trpc_go.yaml                # trpc-go 服务配置
├── .env.example                     # 环境变量模板
├── go.mod / go.sum
├── start.ps1                        # Windows 启动脚本
└── README.md
```

## 快速开始

### 环境要求

- Go >= 1.21
- [QQ 开放平台](https://q.qq.com) 注册的机器人（获取 AppID 和 ClientSecret）
- [DeepSeek](https://platform.deepseek.com/) API Key（或兼容 OpenAI 接口的其他模型）

### 安装

```bash
git clone https://github.com/hmm1313133/QQ_AI_TRPG_BOT.git
cd QQ_AI_TRPG_BOT
go mod tidy
```

### 配置

1. 复制环境变量模板并填入凭证：

```bash
cp .env.example .env
```

```env
# QQ 机器人凭证
QQ_BOT_APPID=your_app_id
QQ_BOT_SECRET=your_client_secret

# LLM 配置 (默认 DeepSeek)
LLM_PROVIDER=deepseek
LLM_MODEL=deepseek-chat
LLM_API_KEY=sk-your-api-key
LLM_BASE_URL=https://api.deepseek.com

# 角色卡存储目录 (可选)
CHARACTER_DIR=./data/characters
```

2. `conf/trpc_go.yaml` 中可调整日志级别、存储配置、Agent 行为参数等。

### 运行

```bash
# 直接运行
go run ./cmd/bot/

# 或构建二进制
go build -o bin/bot ./cmd/bot/
./bin/bot

# Windows PowerShell
.\start.ps1
```

## 指令手册

输入 `.help` 可在 QQ 中查看完整指令列表。所有指令以 `.` 开头。

### 通用指令

| 指令 | 说明 | 示例 |
|------|------|------|
| `.r <表达式>` | 投掷骰子 | `.r 3d6` `.r 1d100+5` `.r 4d6kh3` |
| `.rh <表达式>` | 暗骰（结果仅自己可见） | `.rh 1d100` |
| `.set coc\|dnd` | 切换规则集 | `.set coc` |
| `.set <面数>` | 设置默认骰子面数 | `.set 100` |
| `.mode <模式>` | 切换会话模式 | `.mode trpg` |
| `.log <操作>` | 跑团日志管理 | `.log start` `.log show` `.log export` |
| `.help` | 显示帮助 | `.help` |

### 角色卡指令

| 指令 | 说明 | 示例 |
|------|------|------|
| `.pc new <名>` | 创建角色卡 | `.pc new 张三` |
| `.pc tag <名>` | 绑定角色卡到当前群 | `.pc tag 张三` |
| `.pc list` | 列出所有角色卡 | `.pc list` |
| `.pc del <名>` | 删除角色卡 | `.pc del 张三` |
| `.pc save` | 保存当前角色卡 | `.pc save` |
| `.nn <名>` | 切换/重命名角色 | `.nn 李四` |
| `.st <属性> <值>` | 录入属性/技能 | `.st 力量 60` `.st 力量60` |
| `.st show [属性]` | 查看角色卡数据 | `.st show` `.st show 侦查` |
| `.st del <属性>` | 删除属性 | `.st del 力量` |

### CoC 7版指令（`.set coc` 后生效）

| 指令 | 说明 | 示例 |
|------|------|------|
| `.ra [b/p] <技能> [值]` | 技能检定（b=奖励骰 p=惩罚骰） | `.ra 侦查` `.ra b2 侦查` `.ra 侦查 60` |
| `.rah <技能>` | 暗骰检定 | `.rah 侦查` |
| `.sc <成功>/<失败>` | SAN 检定 | `.sc 0/1` `.sc 1/1d4` |
| `.en <技能>` | 技能成长检定 | `.en 侦查` |
| `.coc [数量]` | 生成属性（最多10组） | `.coc` `.coc 3` |
| `.ti` | 即时疯狂症状 | `.ti` |
| `.li` | 总结性疯狂症状 | `.li` |
| `.setcoc [编号]` | 房规设置 | `.setcoc` `.setcoc 0` |
| `.rav <自身> <对手>` | 对抗检定 | `.rav 60 50` |

### DnD 5e指令（`.set dnd` 后生效）

| 指令 | 说明 | 示例 |
|------|------|------|
| `.rc [优势\|劣势] <调整值\|技能>` | 属性检定 | `.rc +5` `.rc 优势 侦查` |
| `.dnd [数量]` | 生成属性（4d6kh3） | `.dnd` `.dnd 3` |
| `.ri [角色名] <值\|+调整值\|=表达式>` | 先攻 | `.ri 12` `.ri +2` `.ri =1d20+3` |
| `.init [clear]` | 查看/清空先攻列表 | `.init` `.init clear` |
| `.ds` | 死亡豁免检定 | `.ds` |
| `.longrest` | 长休（恢复HP，重置死亡豁免） | `.longrest` |

### 骰子表达式语法

| 语法 | 说明 | 示例 |
|------|------|------|
| `NdS` | 投 N 个 S 面骰 | `3d6` → 3个6面骰 |
| `+` `-` `*` `/` | 四则运算 | `1d6+3` `2d4*10` |
| `khN` | 保留最高 N 个 | `4d6kh3` → 4d6保留最高3个 |
| `klN` | 保留最低 N 个 | `4d6kl1` → 4d6保留最低1个 |
| `!` | 爆炸骰（最大值再投） | `2d6!` |
| `()` | 括号优先级 | `(1d6+3)*2` |

## AI Agent 工具

AI 主持人（KP/DM）在对话中可自主调用以下 FunctionTools，所有操作通过 Service 层执行，与指令共享同一数据源：

| 工具 | 说明 |
|------|------|
| `roll_dice` | 投掷骰子，用于 NPC 行为、随机事件等 |
| `skill_check` | 技能检定，自动识别 CoC(1d100) 或 DnD(1d20) 规则 |
| `san_check` | SAN 检定（仅 CoC），自动更新角色卡 SAN 值 |
| `get_character` | 查询玩家角色卡（属性/技能/状态） |
| `set_ruleset` | 切换规则集 |

## 开发指南

### 添加新的 TRPG 规则集

1. 在 `internal/trpg/ruleset/` 下创建新包，实现 `RuleSet` 接口：

```go
type RuleSet interface {
    Name() string
    SkillCheck(skill string, value int, opts CheckOptions) (*CheckResult, error)
    GenerateAttrs() (map[string]int, error)
    // ... 其他方法
}
```

2. 在 `internal/trpg/engine.go` 中注册规则集。
3. 在 `internal/handler/` 下添加对应的指令处理器。

### 扩展 AI Agent 工具

在 `internal/agent/tools.go` 中添加新的 FunctionTool：

```go
func NewCustomTool(svc *trpg.Service) tool.Tool {
    fn := func(ctx context.Context, req CustomReq) (CustomRsp, error) {
        sessionID, userID, _ := getSessionAndUser(ctx)
        // 通过 Service 执行游戏逻辑
        return CustomRsp{}, nil
    }
    return function.NewFunctionTool(fn,
        function.WithName("custom_tool"),
        function.WithDescription("工具描述..."),
    )
}
```

然后在 `NewKPTools()` 中注册。

### 添加指令处理器

实现 `core.Handler` 接口并在 `cmd/bot/main.go` 中注册：

```go
type Handler interface {
    Name() string
    Match(ctx *core.MessageContext) bool
    Execute(ctx *core.MessageContext, reply ReplyFunc) error
}

// 注册（注意顺序：特定指令优先于通用指令）
plugins.RegisterHandler(handler.NewMyHandler(svc))
```

## 环境变量

| 变量名 | 必填 | 默认值 | 说明 |
|--------|------|--------|------|
| `QQ_BOT_APPID` | ✅ | — | QQ 机器人 AppID |
| `QQ_BOT_SECRET` | ✅ | — | QQ 机器人 ClientSecret |
| `LLM_API_KEY` | ✅ | — | LLM API 密钥 |
| `LLM_PROVIDER` | ❌ | `deepseek` | LLM 提供商 |
| `LLM_MODEL` | ❌ | `deepseek-chat` | 模型名称 |
| `LLM_BASE_URL` | ❌ | `https://api.deepseek.com` | LLM API 地址 |
| `CHARACTER_DIR` | ❌ | `./data/characters` | 角色卡存储目录 |

## 致谢

- [trpc-go](https://github.com/trpc-group/trpc-go) — 腾讯高性能 RPC 框架
- [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) — AI Agent 框架
- [DeepSeek](https://www.deepseek.com/) — 深度求索大模型
- [QQ 开放平台](https://q.qq.com) — QQ 机器人官方 API

## 开源协议

本项目采用 [MIT](LICENSE) 协议，仅供学习和研究使用。

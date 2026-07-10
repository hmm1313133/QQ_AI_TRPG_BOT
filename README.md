# QQ AI TRPG BOT

> 基于 **trpc-go** + **trpc-agent-go** 构建的 QQ 官方机器人 TRPG 跑团框架，集成 AI 剧本生成与多层 AI 主持人（KP/DM）能力。

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev)
[![Version](https://img.shields.io/badge/version-0.2.0-blue)](pkg/version.go)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)

## 项目简介

本项目将腾讯微服务框架 [trpc-go](https://github.com/trpc-group/trpc-go) 与 AI Agent 框架 [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) 相结合，通过 **QQ 官方机器人 API v2** 对接 QQ 群聊/私聊/频道，提供沉浸式 TRPG（桌上角色扮演游戏）体验。

核心能力：

- 🎲 **骰子引擎** - 自研递归下降解析器，支持复杂表达式（`3d6`、`1d100+5`、`4d6kh3`、`2d6!`爆炸骰、`(1d6+3)*2`）
- 🧠 **多层 AI 主持人** - 决策层（Director 导演系统）与输出层（Narrator 叙事层）独立，通过结构化 GameState 保证跨轮次决策一致性
- 📖 **AI 剧本生成** - 上传 PDF/Word 剧本，AI 自动解析为结构化时间轴、NPC、场景、线索、目标
- 🎭 **双规则集** - 内置 **CoC 7版**（克苏鲁的呼唤）与 **DnD 5e**（龙与地下城）完整规则实现
- 📇 **角色卡管理** - JSON 文件持久化，支持属性/技能/状态录入、多角色切换、群绑定
- 💾 **进度持久化** - 宏观进度（时间轴节点/决策历史）+ 微观运行态（场景/NPC状态/隐藏信息/待触发事件）双轨存储
- 💬 **多场景隔离** - 每个群/私聊/频道拥有独立会话状态、规则集和角色绑定
- 📝 **跑团日志** - TRPG 模式下自动记录全部对话，支持查看与导出

## 技术栈

| 组件 | 技术 | 说明 |
|------|------|------|
| 微服务框架 | [trpc-go](https://github.com/trpc-group/trpc-go) `v1.0.3` | 腾讯开源高性能 Go RPC 框架 |
| AI Agent | [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) `v1.10.0` | 基于 trpc-go 的 LLM Agent 框架 |
| LLM 模型 | [DeepSeek](https://platform.deepseek.com/) | 默认 `deepseek-chat`，兼容 OpenAI 接口 |
| QQ 协议 | QQ 官方 Bot API v2 | WebSocket 事件订阅 + OpenAPI HTTP 调用 |
| WebSocket | [nhooyr.io/websocket](https://github.com/coder/websocket) `v1.8.17` | 纯 Go WebSocket 客户端 |
| 语言 | Go 1.21+ | - |

## 架构设计

项目采用 **多层架构**，AI 主持人拆分为决策层与输出层，通过 GameState 持久化保证跨轮次决策一致性：

```
┌──────────────────────────────────────────────────────────┐
│              QQ 消息入口层 (bot)                          │
│  群聊@消息 / 群全量消息 / 单聊 / 频道消息                   │
│  消息去重 -> MessageContext -> 路由分发                      │
└─────────────────────────┬────────────────────────────────┘
                          │
            ┌─────────────┴─────────────┐
            │                           │
            ▼                           ▼
┌───────────────────┐     ┌─────────────────────────────────┐
│  功能层 Handler    │     │        AI 多层架构 (Agent)       │
│  确定性指令处理     │     │                                 │
│  骰子/角色卡/规则   │     │  ┌─────────────────────────┐    │
│  剧本管理/日志      │     │  │  KPPipeline 协调器       │    │
└────────┬──────────┘     │  └───────────┬─────────────┘    │
         │                │              │                   │
         │                │  ┌───────────▼─────────────┐    │
         │                │  │  Director 导演系统       │    │
         │                │  │  规则化预评估 + LLM(0.2)  │    │
         │                │  │  -> DecisionDirective    │    │
         │                │  └───────────┬─────────────┘    │
         │                │              │                   │
         │                │  ┌───────────▼─────────────┐    │
         │                │  │  Narrator 叙事层         │    │
         │                │  │  LLM(0.7) + 工具调用      │    │
         │                │  │  -> 沉浸式叙事文本        │    │
         │                │  └───────────┬─────────────┘    │
         │                │              │                   │
         │                │  ┌───────────▼─────────────┐    │
         │                │  │  StateUpdate + 持久化    │    │
         │                │  └─────────────────────────┘    │
         │                └───────────────┬─────────────────┘
         │                                │
         └──────────────┬─────────────────┘
                        ▼
┌──────────────────────────────────────────────────────────┐
│            Service 统一服务层 (trpg)                      │
│  规则集管理 / 角色卡操作 / 骰子投掷 / 检定                  │
│  单一数据源: Handler 和 Agent 共享同一实例                  │
└─────────────────────────┬────────────────────────────────┘
                          ▼
┌──────────────────────────────────────────────────────────┐
│         剧本层 + 运行态层 (script + GameState)             │
│  Script Archive: 剧本与宏观进度持久化                       │
│  GameStateStore: 微观运行态持久化 (场景/NPC/隐藏信息/事件)    │
│  ProgressTracker: 时间轴节点/决策历史/剧情摘要               │
│  TimelineEngine: 定时器+事件驱动混合推进                    │
└──────────────────────────────────────────────────────────┘
```

### AI 多层架构详解

AI KP 采用 **决策层与输出层分离** 的多层架构：

#### 1. GameState 结构化运行态

独立于 LLM 上下文窗口的结构化状态，每轮自动持久化，保证跨轮次一致性：

| 字段 | 说明 |
|------|------|
| `CurrentScene` | 当前场景（名称/描述/氛围/危险等级/调查点） |
| `NPCStates` | NPC 实时状态（态度/动机/秘密/对话风格/关键对话） |
| `HiddenInfo` | 隐藏信息/线索（描述/类型/是否已发现） |
| `PendingEvents` | 待触发事件（描述/条件/优先级/是否已触发） |
| `Objectives` | 当前目标（描述/是否完成） |
| `Metrics` | 游戏指标（张力/混乱/玩家掌控权/目标进度） |
| `StoryContext` | 故事背景与摘要 |
| `TurnCount` | 轮次计数 |
| `LastDirective` | 上一轮决策指令（供下一轮参考） |

#### 2. Director 导演系统

读取 GameState，评估局势并做出下一轮决策：

- **规则化预评估**（确定性计算，非 LLM）：
  - `TensionLevel` (0-100) - 场景激烈程度（基于危险等级/战斗状态/NPC敌意）
  - `ChaosLevel` (0-100) - 局势离失控的距离（基于混乱事件/未解决事件数）
  - `PlayerAgency` (0-100) - 玩家对剧情的掌控权（基于目标完成率/线索发现率）
  - `ObjectiveProgress` (0-100) - 目标推进程度（基于已完成目标占比）
- **LLM 决策**（温度 0.2，低温度保证一致性）：
  - 输出 `DecisionDirective` JSON：叙事基调/节奏/允许的工具/推荐行动/状态更新
  - 相同 GameState + 相同指标必然得到类似决策方向

#### 3. Narrator 叙事层

接收 DecisionDirective 约束，生成沉浸式叙事文本：

- 复用全部 KP 工具集（骰子/检定/角色卡/剧本/状态更新）
- 注入 Director 的决策指令作为创作约束
- 温度 0.7，兼顾创造性与可控性

#### 4. KPPipeline 协调器

串联 Director -> Narrator -> StateUpdate，含降级兜底：

```
玩家消息 -> 加载 GameState
  -> Director: 预评估 + LLM -> DecisionDirective
  -> Narrator: 注入 Directive + 工具循环 -> 叙事文本
  -> 应用 StateUpdates + 持久化 GameState
  -> 返回叙事文本

（任意环节失败 -> 回退到单 Agent 模式）
```

### 剧本系统

| 功能 | 说明 |
|------|------|
| `.script analyze` | 上传 PDF/Word 剧本，AI 自动解析为结构化 Script |
| `.script list` | 列出已加载剧本 |
| `.script load <名称>` | 加载剧本，初始化 GameState + 进度追踪 + 时间轴 |
| `.script unload` | 卸载剧本，清理 GameState + 进度 |
| `.script progress` | 查看当前进度 |
| `.script timeline` | 查看时间轴状态 |

剧本加载时自动建立映射：

```
Script.Timeline[0]      -> GameState.CurrentScene
Script.Characters       -> GameState.NPCStates
Script.Scenes.Hidden    -> GameState.HiddenInfo
TimelineNode.Triggers   -> GameState.PendingEvents
TimelineNode.Objectives -> GameState.Objectives
Script.Background       -> GameState.StoryContext
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
│   │   ├── kp_agent.go              # KP/DM Agent（委托 KPPipeline，回退单 agent）
│   │   ├── kp_pipeline.go           # 多层流水线协调器 (Director->Narrator->StateUpdate)
│   │   ├── gamestate.go             # GameState 结构化运行态定义
│   │   ├── gamestate_store.go       # GameState 持久化 + InitFromScript + RefreshForNode
│   │   ├── director.go              # Director 导演系统 (LLM 决策 + 降级兜底)
│   │   ├── director_prompt.go       # Director 系统提示词构建
│   │   ├── director_metrics.go      # 规则化指标评估器 (确定性计算)
│   │   ├── narrator.go              # Narrator 叙事层 Agent
│   │   ├── narrator_prompt.go       # Narrator 系统提示词构建
│   │   ├── tools.go                 # KP FunctionTools (骰子/检定/角色卡/规则集)
│   │   └── script_tools.go          # 剧本 FunctionTools (上下文/时间轴/进度/NPC/状态更新)
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
│   │   ├── script.go                # .script 剧本管理 (加载/卸载/进度/时间轴)
│   │   └── log.go                   # .log 跑团日志
│   ├── script/
│   │   ├── types.go                 # Script/TimelineNode/ScriptCharacter/Progress 结构体
│   │   ├── archive.go               # 剧本与进度持久化 (JSON)
│   │   ├── analyzer.go              # AI 剧本识别 Agent (PDF/Word -> 结构化 Script)
│   │   └── analyzer_prompt.go       # 剧本识别系统提示词
│   ├── store/
│   │   ├── store.go                 # 数据存储接口 (Memory/SQLite)
│   │   └── openviking.go            # OpenViking 上下文数据库客户端 (可选)
│   └── trpg/
│       ├── engine.go                # TRPG 引擎 (会话/规则集/角色绑定)
│       ├── service.go               # 统一游戏服务层 (Handler+Agent共用)
│       ├── progress.go              # 跑团进度追踪 (时间轴节点/决策/摘要)
│       ├── timeline.go              # 时间轴引擎 (定时器+事件驱动混合)
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
| `.script <操作>` | 剧本管理 | `.script list` `.script load 模组名` |
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
| `NdS` | 投 N 个 S 面骰 | `3d6` -> 3个6面骰 |
| `+` `-` `*` `/` | 四则运算 | `1d6+3` `2d4*10` |
| `khN` | 保留最高 N 个 | `4d6kh3` -> 4d6保留最高3个 |
| `klN` | 保留最低 N 个 | `4d6kl1` -> 4d6保留最低1个 |
| `!` | 爆炸骰（最大值再投） | `2d6!` |
| `()` | 括号优先级 | `(1d6+3)*2` |

## AI Agent 工具

AI 主持人（KP/DM）在对话中可自主调用以下 FunctionTools，所有操作通过 Service 层执行，与指令共享同一数据源：

### KP 通用工具

| 工具 | 说明 |
|------|------|
| `roll_dice` | 投掷骰子，用于 NPC 行为、随机事件等 |
| `skill_check` | 技能检定，自动识别 CoC(1d100) 或 DnD(1d20) 规则 |
| `san_check` | SAN 检定（仅 CoC），自动更新角色卡 SAN 值 |
| `get_character` | 查询玩家角色卡（属性/技能/状态） |
| `set_ruleset` | 切换规则集 |

### 剧本工具

| 工具 | 说明 |
|------|------|
| `get_script_context` | 获取当前剧本上下文（背景/时间轴/当前节点/NPC） |
| `advance_timeline` | 推进时间轴节点，联动刷新 GameState 运行态 |
| `get_progress` | 查询跑团进度（当前节点/已完成节点/决策历史） |
| `save_progress` | 保存进度摘要，联动持久化 GameState |
| `get_npc` | 查询 NPC 详细信息（动机/秘密/对话风格/关键对话） |
| `update_game_state` | 更新运行态（NPC态度/线索发现/事件触发/目标完成） |

## 开发指南

### 添加新的 TRPG 规则集

1. 在 `internal/trpg/ruleset/` 下创建新包，实现 `RuleSet` 接口
2. 在 `internal/trpg/engine.go` 中注册规则集
3. 在 `internal/handler/` 下添加对应的指令处理器

### 扩展 AI Agent 工具

在 `internal/agent/tools.go` 或 `script_tools.go` 中添加新的 FunctionTool，然后在 `NewKPTools()` 或 `NewScriptTools()` 中注册。

### 添加指令处理器

实现 `core.Handler` 接口并在 `cmd/bot/main.go` 中注册（注意顺序：特定指令优先于通用指令）。

## 环境变量

| 变量名 | 必填 | 默认值 | 说明 |
|--------|------|--------|------|
| `QQ_BOT_APPID` | ✅ | - | QQ 机器人 AppID |
| `QQ_BOT_SECRET` | ✅ | - | QQ 机器人 ClientSecret |
| `LLM_API_KEY` | ✅ | - | LLM API 密钥 |
| `LLM_PROVIDER` | ❌ | `deepseek` | LLM 提供商 |
| `LLM_MODEL` | ❌ | `deepseek-chat` | 模型名称 |
| `LLM_BASE_URL` | ❌ | `https://api.deepseek.com` | LLM API 地址 |
| `CHARACTER_DIR` | ❌ | `./data/characters` | 角色卡存储目录 |
| `SCRIPT_DIR` | ❌ | `./data/scripts` | 剧本存储目录 |
| `GAMESTATE_DIR` | ❌ | `./data/scripts/gamestate` | GameState 运行态存储目录 |
| `OPENVIKING_ENABLED` | ❌ | `false` | 是否启用 OpenViking 上下文数据库 |
| `OPENVIKING_BASE_URL` | ❌ | `http://localhost:1933` | OpenViking 服务地址 |

## 致谢

- [trpc-go](https://github.com/trpc-group/trpc-go) - 腾讯高性能 RPC 框架
- [trpc-agent-go](https://github.com/trpc-group/trpc-agent-go) - AI Agent 框架
- [DeepSeek](https://www.deepseek.com/) - 深度求索大模型
- [QQ 开放平台](https://q.qq.com) - QQ 机器人官方 API
- [AI Dungeon](https://play.aidungeon.io) - Voyage 架构设计灵感

## 开源协议

本项目采用 [MIT](LICENSE) 协议，仅供学习和研究使用。

# QQ AI TRPG Bot

基于 QQ 官方机器人 API 的 AI 驱动 TRPG（桌上角色扮演游戏）主持机器人。支持 CoC 7th 和 DnD 5e 规则集，集成 AI 多层架构（Director + Narrator）、世界引擎、记忆系统和剧本管理，为玩家提供沉浸式的自动化跑团体验。

## 核心特性

### 多规则集引擎
- **CoC 7th**：1d100 技能检定、SAN 检定、奖励/惩罚骰、疯狂发作
- **DnD 5e**：1d20 检定、优势/劣势、属性判定
- 运行时动态切换规则集，角色卡自动适配

### AI 多层架构
采用 Director-Narrator 两层架构，实现**单轮单次 LLM 调用**：

| 层级 | 职责 | 调用频率 | 模型 |
|------|------|----------|------|
| **Director (Planner)** | 场景规划、节奏控制、下一步指令 | 低频（场景切换或每 N 轮） | LLM (低温度 0.2) |
| **Narrator** | 叙事生成、NPC 扮演、氛围描写 | 每轮一次 | LLM (高温度 0.7) |
| **MetricsEvaluator** | 指标评估（紧张度、节奏等） | 每轮（规则驱动，零 LLM） | 纯规则 |

**Turn Engine** 流程：`ContextBuilder 组装上下文 -> Narrator 生成叙事 -> ApplyEvent 更新世界状态 -> 异步写入记忆`

### 世界引擎 (`internal/world`)
所有游戏状态的**唯一真相源**（Single Source of Truth），取代旧版分散的 GameState/Progress：

- **单写入入口**：所有状态变更经 `Engine.ApplyEvent()` 校验后落库，防止 LLM 幻觉导致状态不一致
- **事件溯源**：EventLog 记录所有变更历史，支持回放与审计
- **后果传播**：事件触发级联效应（如 NPC 死亡 -> 关系网破裂 -> 阵营态度变化）
- **世界时钟**：跟踪游戏内时间，离线时自动演化（NPC 情绪衰减、记忆遗忘）
- **NPC 情绪模型**：Valence-Arousal 二维情感空间，随时间和事件动态变化
- **三模式支持**：TRPG（剧本驱动）、SimRPG（模拟驱动）、Roleplay（自由扮演）
- **长记忆系统**：基于 Generative Agents 论文的三因子检索（Recency × Importance × Relevance）
- **JSON 持久化**：原子写入，支持热重载
- **旧版迁移**：自动将 GameState/Progress 数据迁移到 WorldState

### 记忆系统 (`internal/agent/memory`)
双轨写入 + 三因子检索：

- **规则侧**：状态事件（NPC 态度变化、线索发现等）由规则引擎自动写入
- **框架侧**：对话内容通过 trpc-agent-go 的 extractor 提取关键信息
- **反思压缩**：周期性生成高层洞察，附带证据链
- **检索排序**：`Score = α·Recency + β·Importance + γ·Relevance`

### 上下文预算 (`internal/agent/context_builder`)
基于 Token 预算的上下文组装，防止长团上下文爆炸：

- 优先级分层：必需段落（剧本/世界状态/角色）> 可选段落（记忆/进度）
- 复杂度感知：`cost = f(complexity)`，而非 `f(history_length)`
- 动态裁剪：超出预算时按优先级降序裁剪可选段落

### 成长闭环 (`internal/agent/progression`)
技能成长全流程自动化：

1. **检定记录**：技能检定成功时自动记录（`RecordSkillUse`）
2. **幕间结算**：场景切换时触发结算（`Settle`）
3. **角色卡更新**：通过成长检定自动提升技能值

### 剧本系统
- **AI 剧本识别**：上传 PDF/Word/文本，LLM 自动解析为结构化剧本（时间轴、NPC、场景、线索）
- **多输入方式**：本地文件路径、HTTP URL、直接发送文件附件、粘贴文本
- **进度追踪**：节点完成状态、决策记录、剧情摘要
- **时间轴引擎**：自动推进、停滞提醒、定时器管理

### AI 工具集
KP Agent 可调用以下 Function Tools：

| 工具 | 说明 |
|------|------|
| `roll_dice` | 投掷骰子（支持复杂表达式：`1d100`、`3d6`、`4d6kh3`、`2d6!`） |
| `skill_check` | 技能检定（自动识别 CoC/DnD 规则，成功时触发成长记录） |
| `san_check` | SAN 检定（CoC 专用，自动更新角色卡） |
| `get_character` | 查询角色卡（属性/技能/状态） |
| `set_ruleset` | 切换规则集 |
| `get_script_context` | 获取剧本上下文（背景/当前节点/时间轴） |
| `advance_timeline` | 推进剧情时间轴（触发场景刷新 + 幕间成长结算） |
| `get_progress` | 查看跑团进度（节点/决策/摘要） |
| `save_progress` | 保存进度（剧情摘要 + 决策记录） |
| `get_npc` | 获取 NPC 信息（性格/动机/秘密/对话风格） |
| `update_game_state` | 更新游戏运行态（NPC 态度/线索发现/事件触发/目标完成） |

## 指令列表

### 基础指令

| 指令 | 说明 |
|------|------|
| `.help` | 查看帮助 |
| `.mode trpg` / `.mode chat` | 切换 TRPG / 聊天模式 |
| `.r <表达式>` | 投骰子（如 `.r 1d100`、`.r 3d6+5`） |
| `.coc` / `.dnd` | 切换规则集 |

### 角色卡指令

| 指令 | 说明 |
|------|------|
| `.char create <名称>` | 创建角色卡 |
| `.char list` | 列出角色卡 |
| `.char bind <名称>` | 绑定角色卡到当前会话 |
| `.char show [名称]` | 查看角色卡详情 |
| `.char edit <字段> <值>` | 编辑角色卡属性 |

### 剧本指令

| 指令 | 说明 |
|------|------|
| `.script upload <路径或URL>` | 上传并 AI 识别剧本（PDF/Word/文本） |
| `.script upload` + 发送文件 | 直接发送文件附件 |
| `.script text <内容>` | 粘贴剧本文本进行识别 |
| `.script list` | 列出所有剧本 |
| `.script load <名称>` | 加载剧本到当前会话 |
| `.script info [名称]` | 查看剧本详情 |
| `.script remove <名称>` | 删除剧本 |
| `.script unload` | 卸载当前剧本 |
| `.progress` | 查看跑团进度 |
| `.timeline` | 查看时间轴状态 |

## 快速开始

### 环境要求

- Go 1.21+
- QQ 机器人开发者账号（[申请地址](https://q.qq.com/)）
- LLM API Key（默认支持 DeepSeek，兼容 OpenAI 接口）

### 安装

```bash
git clone https://github.com/hmm1313133/QQ_AI_TRPG_BOT.git
cd QQ_AI_TRPG_BOT
go mod download
```

### 配置环境变量

```bash
# QQ 机器人
export QQ_BOT_APPID=your_app_id
export QQ_BOT_SECRET=your_client_secret

# LLM 配置（默认 DeepSeek）
export LLM_PROVIDER=deepseek
export LLM_MODEL=deepseek-chat
export LLM_API_KEY=your_api_key
export LLM_BASE_URL=https://api.deepseek.com

# 可选：数据目录
export CHARACTER_DIR=./data/characters
export SCRIPT_DIR=./data/scripts
export WORLD_DIR=./data/worlds
export MEMORY_DIR=./data/memories

# 可选：OpenViking 记忆服务
export OPENVIKING_ENABLED=false
export OPENVIKING_BASE_URL=http://localhost:1933
export OPENVIKING_API_KEY=your_key
export OPENVIKING_ACCOUNT=your_account
export OPENVIKING_USER=your_user

# 可选：记忆提取器
export MEMORY_EXTRACTOR_ENABLED=true
```

### 运行

```bash
# Windows
.\start.ps1

# Linux/macOS
go run cmd/bot/main.go
```

### 构建二进制

前端（Vue3 + Element Plus，源码在 `frontend/`）通过 `go:embed` 嵌入二进制，**需先构建前端再编译 Go**。一键构建：

```powershell
# Windows（自动使用系统 Node 或项目便携版 tools/node）
.\build.ps1
```

手动分步：

```bash
cd frontend && npm install && npm run build && cd ..   # 产物 -> internal/web/static/dist
go build -o bot.exe ./cmd/bot
```

构建后访问 `http://localhost:8080/chat`（聊天页）与 `http://localhost:8080/admin`（管理后台）。前端开发调试：`cd frontend && npm run dev`（已配置 /api、/ws 代理到 :8080）。

## 项目结构

```
QQ_AI_TRPG_BOT/
├── cmd/bot/main.go              # 程序入口，组件初始化与依赖注入
├── conf/                        # 配置文件
├── internal/
│   ├── agent/                   # AI Agent 层
│   │   ├── kp_agent.go          #   KP Agent 主控（对话入口）
│   │   ├── turn_engine.go       #   Turn Engine（单轮编排：Director -> Narrator -> StateUpdate）
│   │   ├── director.go         #   Director（低频 Planner）
│   │   ├── director_prompt.go   #   Director 提示词
│   │   ├── director_metrics.go  #   指标评估器（纯规则，零 LLM）
│   │   ├── narrator.go          #   Narrator（叙事生成）
│   │   ├── narrator_prompt.go   #   Narrator 提示词
│   │   ├── context_builder.go   #   上下文预算组装器
│   │   ├── guidance.go          #   规则指导（替代每轮 Director LLM）
│   │   ├── directive.go         #   Director 指令定义
│   │   ├── memory.go            #   记忆服务（三因子检索 + 反思压缩）
│   │   ├── memory_backend.go    #   记忆后端（JSON 实现 framework 接口）
│   │   ├── progression.go       #   成长引擎（技能成长闭环）
│   │   ├── tools.go             #   KP 工具集（骰子/检定/角色卡）
│   │   └── script_tools.go      #   剧本工具集（上下文/进度/状态更新）
│   ├── world/                   # 世界引擎（状态唯一真相源）
│   │   ├── engine.go            #   引擎核心（单写入入口 + 事件溯源）
│   │   ├── types.go             #   WorldState 类型定义
│   │   ├── store.go             #   JSON 持久化（原子写入）
│   │   ├── clock.go             #   世界时钟 + 离线演化
│   │   ├── mood.go              #   NPC 情绪模型（Valence-Arousal）
│   │   ├── consequence.go       #   后果传播引擎
│   │   ├── memory.go            #   长记忆存储 + 三因子检索
│   │   ├── mode.go              #   三模式管理（TRPG/SimRPG/Roleplay）
│   │   ├── migrate.go           #   旧版 GameState/Progress 迁移
│   │   └── util.go              #   工具函数
│   ├── bot/                     # QQ Bot 连接层
│   ├── core/                    # 核心基础（Session/Plugin/Handler 框架）
│   ├── handler/                 # 指令处理器（骰子/角色/模式/剧本等）
│   ├── script/                  # 剧本系统（解析/存档/AI 识别）
│   ├── store/                   # 外部存储（OpenViking 客户端）
│   └── trpg/                    # TRPG 引擎（规则集/服务层/进度追踪）
│       ├── character/           #   角色卡管理
│       ├── gamelog/             #   游戏日志
│       ├── ruleset/             #   规则集实现（CoC7/DnD5e）
│       └── progress.go          #   进度追踪器（world.Engine 门面）
├── pkg/                         # 公共工具
│   └── version.go               # 版本号
├── go.mod / go.sum              # Go 依赖管理
├── frontend/                    # Web 前端（Vue3 + Vite + Element Plus）
├── tools/node/                  # 便携 Node 工具链（不入库）
├── build.ps1                    # 一键构建脚本（先前端后 Go）
└── start.ps1                    # Windows 启动脚本
```

## 架构概览

```
┌─────────────────────────────────────────────────────┐
│                    QQ Bot (WebSocket)                │
├─────────────────────────────────────────────────────┤
│              Plugin Manager / Session               │
├──────────────────────┬──────────────────────────────┤
│    Handler 层        │         Agent 层              │
│  (Go 确定性逻辑)     │    (AI 驱动)                  │
│  ┌──────────────┐    │  ┌──────────────────────┐   │
│  │ DiceHandler  │    │  │ KPAgent (对话入口)    │   │
│  │ CharHandler  │    │  │   ↓                   │   │
│  │ ModeHandler  │    │  │ TurnEngine            │   │
│  │ ScriptHandler│    │  │   ├─ ContextBuilder    │   │
│  │ RulesetHandlr│    │  │   ├─ Director(低频)    │   │
│  │ CoC/DnDHandlr│    │  │   ├─ Narrator(每轮)    │   │
│  └──────┬───────┘    │  │   ├─ MetricsEval(规则)│   │
│         │            │  │   └─ MemoryService     │   │
│         ▼            │  └──────────┬─────────────┘   │
│  ┌──────────────┐    │             │                  │
│  │  trpg.Service│◄───┼─────────────┘                  │
│  │  (统一服务层) │    │                                │
│  └──────┬───────┘    │             │                  │
│         │            │             ▼                  │
│         ▼            │  ┌──────────────────────┐      │
│  ┌──────────────┐    │  │    World Engine      │      │
│  │   Ruleset    │    │  │  (状态唯一真相源)     │      │
│  │  CoC7/DnD5e  │    │  │  ├─ WorldState       │      │
│  └──────────────┘    │  │  ├─ EventLog         │      │
│                      │  │  ├─ NPC Moods        │      │
│                      │  │  ├─ Memory Store     │      │
│                      │  │  └─ JSON Persistence │      │
│                      │  └──────────────────────┘      │
└──────────────────────┴──────────────────────────────┘
```

### 设计原则

1. **Service 层单一真相**：`trpg.Service` 为 Handler 和 Agent 共享的唯一游戏操作入口
2. **世界引擎单一写入**：所有状态变更经 `Engine.ApplyEvent()` 校验，防止 LLM 幻觉
3. **单轮单次 LLM**：每轮仅调用一次 Narrator LLM，Director 降频为 Planner
4. **规则优先**：能用规则解决的不用 LLM（指标评估、状态校验、情绪衰减）
5. **Token 预算**：上下文按预算组装，长团不爆炸

## 技术栈

- **语言**：Go 1.21+
- **QQ Bot SDK**：QQ 官方 WebSocket API
- **AI 框架**：[trpc-agent-go](https://github.com/trpc-group/trpc-agent-go)（Agent 编排 + Function Tools）
- **LLM**：DeepSeek（兼容 OpenAI 接口，可切换其他 Provider）
- **规则集**：CoC 7th Edition、DnD 5e
- **持久化**：JSON 文件存储（零外部依赖）
- **可选记忆服务**：[OpenViking](https://github.com/Tencent/OpenViking)

## 开发

### 运行测试

```bash
go test ./...
```

### 代码结构约定

- Handler 层只做确定性逻辑，不调用 LLM
- Agent 层通过 `trpg.Service` 访问游戏数据，不直接操作存储
- 世界状态变更必须通过 `Engine.ApplyEvent()`，不直接修改 `WorldState` 字段
- 新代码使用 `world.Engine`，旧代码通过 `ProgressTracker` 门面兼容

## License

See [LICENSE](LICENSE) file for details.

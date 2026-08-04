# QQ AI TRPG Bot

基于 QQ 官方机器人 API 的 AI 驱动 TRPG（桌上角色扮演游戏）主持机器人。支持 CoC 7th 和 DnD 5e 规则集，集成 AI 多层架构（Director + Narrator）、世界引擎、记忆系统、设定库（Lorebook）、素材库和剧本管理，并通过 Web 渠道提供浏览器聊天与可视化管理后台，为玩家提供沉浸式的自动化跑团体验。

## 核心特性

### 多渠道接入

QQ 与 Web 双渠道共享同一 `core.Router`，指令与聊天同通道——`.script`、`.r` 等指令在 Web 端零成本可用。

| 渠道 | 传输 | 说明 |
|------|------|------|
| **QQ Bot** | WebSocket（官方 API v2） | 群聊 / 单聊 / 频道，被动回复 + 主动推送 |
| **Web Chat** | WebSocket + HTTP | 浏览器聊天页（`/chat`），支持文件上传、存档管理、实时状态推送 |

### 多规则集引擎
- **CoC 7th**：1d100 技能检定、SAN 检定、奖励/惩罚骰、疯狂发作
- **DnD 5e**：1d20 检定、优势/劣势、属性判定
- 运行时动态切换规则集，角色卡自动适配

### AI 多层架构
采用 Director-Narrator 两层架构，实现**单轮单次 LLM 调用**：

| 层级 | 职责 | 调用频率 | 模型 |
|------|------|----------|------|
| **Director (Planner)** | 场景规划、节奏控制、下一步指令 | 低频（场景切换或每 N 轮，默认 8 轮） | LLM (低温度 0.2) |
| **Narrator** | 叙事生成、NPC 扮演、氛围描写 | 每轮一次 | LLM (高温度 0.7) |
| **MetricsEvaluator** | 指标评估（紧张度、节奏等） | 每轮（规则驱动，零 LLM） | 纯规则 |
| **RuleGuidance** | 规则指导（替代每轮 Director LLM） | 每轮（代码计算） | 纯规则 |

**Turn Engine** 流程：`ContextBuilder 组装上下文 -> Narrator 生成叙事 -> ApplyEvent 更新世界状态 -> 异步写入记忆`

### 世界引擎 (`internal/world`)
所有游戏状态的**唯一真相源**（Single Source of Truth），SQLite 持久化：

- **单写入入口**：所有状态变更经 `Engine.ApplyEvent()` 校验后落库，防止 LLM 幻觉导致状态不一致
- **事件溯源**：EventLog 记录所有变更历史，支持回放与审计
- **后果传播**：事件触发级联效应（如 NPC 死亡 -> 盟友关系破裂 -> 阵营态度变化）
- **世界时钟**：跟踪游戏内时间，离线时自动演化（NPC 情绪衰减、记忆遗忘、定时事件结算）
- **NPC 情绪模型**：Valence-Arousal 二维情感空间，随时间和事件动态变化
- **双时序事实表**：易变事实带有效区间，旧事实失效而非覆盖（"上周门还开着"类问题可答）
- **三模式支持**：TRPG（剧本驱动）、SimRPG（模拟驱动）、Roleplay（自由扮演），模式即配置
- **状态锁定**：死亡/任务完成等关键事实不可逆，冲突变更被拒绝
- **旧版迁移**：自动将 GameState/Progress/JSON 目录数据迁移到 SQLite

### 设定库 Lorebook (`internal/world/lore.go`)
借鉴 SillyTavern World Info 的声明式上下文注入，**零 LLM 调用、完全可测试**：

- **关键词触发**：主键 + 次键（AND_ANY / AND_ALL / NOT_ANY 逻辑组合），子串匹配不区分大小写
- **恒定注入**：世界观核心设定常驻上下文，豁免预算裁剪
- **注入位置**：`front`（世界观区）/ `tail`（风格指令区），离输出越近影响力越强
- **优先级裁剪**：0-100 优先级，超出字符预算时按优先级降序裁剪
- **扫描窗口**：按近 N 轮对话文本扫描关键词（默认 4 轮，可配置），可选递归扫描
- **ST 格式导入**：支持 SillyTavern lorebook JSON 格式一键导入，兼容对象/数组形态
- **命中测试**：管理后台提供实时命中测试，查看命中条目、原因、预算占用与被裁条目

### 素材库 (`internal/assetparse` + `internal/world/asset_store.go`)
跨世界复用的创作素材，SQLite 持久化：

- **六大类型**：角色 / 地点 / 物品 / 势力 / 主线 / 世界观
- **LLM 解析**：粘贴文本，AI 自动提取结构化素材（与剧本识别同模型配置）
- **标签 + 搜索**：按类型、关键词、标签筛选
- **世界联动**：素材可注入世界设定库，GM 在世界编辑器中引用

### 记忆系统 (`internal/agent/memory`)
双轨写入 + 三因子检索：

- **规则侧**：状态事件（NPC 态度变化、线索发现等）由规则引擎自动写入
- **框架侧**：对话内容通过 trpc-agent-go 的 extractor 提取关键信息（ADD/UPDATE/DELETE/NOOP 算子集）
- **反思压缩**：周期性生成高层洞察，附带证据引用链
- **检索排序**：`Score = α·Recency(0.995^Δh) + β·Importance + γ·Relevance`
- **Pinned 记忆**：里程碑事件（背叛/结盟/死亡）永不压缩遗忘

### 上下文预算 (`internal/agent/context_builder`)
基于字符预算的上下文组装，防止长团上下文爆炸：

- **默认 45000 字符**（约 3 万 token），可在管理后台热更新
- **优先级分层**：必需段落（锁定事实/世界状态/角色/NPC 认知卡）> 可选段落（记忆/设定库/近期对话）
- **RecentTurns 窗口**：环形保留最近 10 轮逐字对话，解决指代消解与叙事连续性
- **三层记忆分工**：近期逐字 = RecentTurns；中期事实 = MemoryService；远期剧情 = CampaignSummary
- **复杂度感知**：`cost = f(complexity)`，而非 `f(history_length)`

### 成长闭环 (`internal/agent/progression`)
技能成长全流程自动化：

1. **检定记录**：技能检定成功时自动记录（`RecordSkillUse`，按 userID+skill 去重）
2. **幕间结算**：场景切换时触发结算（`Settle`，复用 coc7.SkillGrowth）
3. **角色卡更新**：通过成长检定自动提升技能值
4. **成长报告**：结算结果并入 `advance_timeline` 响应呈现玩家

### 剧本系统
- **AI 剧本识别**：上传 PDF/Word/文本，LLM 自动解析为结构化剧本（时间轴、NPC、场景、线索）
- **多输入方式**：本地文件路径、HTTP URL、直接发送文件附件、粘贴文本
- **进度追踪**：节点完成状态、决策记录、剧情摘要
- **时间轴引擎**：自动推进、停滞提醒、定时器管理（Web 渠道通过 WS 即时推送）

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
| `get_npc` | 获取 NPC 信息（性格/动机/秘密/对话风格/情绪/记忆） |
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

## 管理后台

Web 管理后台（`/admin`）提供完整可视化管理，前端 Vue3 + Element Plus：

| 页面 | 功能 |
|------|------|
| **Dashboard** | 系统概览（运行时长、世界数、剧本数、角色卡数、Bot 状态） |
| **Bot 管理** | QQ 机器人凭证配置、连接状态查看、重启机器人 |
| **世界管理** | 世界列表、世界编辑器（NPC/地点/势力/任务/关系/事实/事件日志/锁定/设定库） |
| **剧本管理** | 剧本列表、上传识别、详情查看、删除 |
| **配置中心** | 运行时配置热更新（LLM 模型/温度/上下文预算/设定库参数/记忆抽取开关等），敏感字段掩码，按生效级别分组 |
| **角色卡管理** | 角色卡列表、查看详情 |
| **素材库** | 跨世界素材管理（角色/地点/物品/势力/主线/世界观），LLM 解析，标签搜索 |
| **记忆管理** | 查看世界/NPC 记忆条目、重要性、标签 |
| **日志** | 游戏日志查看 |

### 配置热更新分级

| 生效级别 | 说明 | 示例 |
|----------|------|------|
| **Hot** | 下个回合读取即生效 | 上下文预算、设定库参数、记忆抽取开关、计划间隔 |
| **Bot Restart** | 重启 QQ 机器人后生效 | AppID、Secret |
| **Process Restart** | 重启进程后生效 | LLM 模型、API Key、温度、Web Token |

## 快速开始

### 环境要求

- Go 1.21+
- Node.js 18+（构建前端）
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
export LLM_MODEL=deepseek-v4-flash
export LLM_API_KEY=your_api_key
export LLM_BASE_URL=https://api.deepseek.com

# 可选：Web 聊天访问令牌（为空则开放）
export WEB_CHAT_TOKEN=your_chat_token

# 可选：管理后台令牌
export ADMIN_TOKEN=your_admin_token

# 可选：数据目录（默认 ./data/app.db，SQLite 共享数据库）
export CONFIG_DB=./data/app.db

# 可选：旧数据目录（首次启动自动迁移到 SQLite）
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

> **提示**：QQ 凭证、LLM 配置等也可在管理后台「配置中心」修改，修改后按生效级别自动应用。首次启动时环境变量自动播种到 SQLite。

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

构建后访问 `http://localhost:8080/chat`（聊天页）与 `http://localhost:8080/admin`（管理后台）。Web 服务由 trpc-go 泛 HTTP 托管，监听地址在 `conf/trpc_go.yaml` 的 `server.service`（`trpc.trpg.web.Admin`）中配置。前端开发调试：`cd frontend && npm run dev`（已配置 /api、/ws 代理到 :8080）。

## 架构概览

```
┌──────────────────────────────────────────────────────────┐
│              QQ Bot (WebSocket)    Web Chat (WS + HTTP)   │
│                  └────────┬─────────┘                     │
│                   core.Router (统一路由)                    │
├──────────────────────┬───────────────────────────────────┤
│    Handler 层        │         Agent 层                   │
│  (Go 确定性逻辑)     │    (AI 驱动)                        │
│  ┌──────────────┐    │  ┌──────────────────────┐          │
│  │ DiceHandler  │    │  │ KPAgent (对话入口)    │          │
│  │ CharHandler  │    │  │   ↓                   │          │
│  │ ModeHandler  │    │  │ TurnEngine            │          │
│  │ ScriptHandler│    │  │   ├─ ContextBuilder    │          │
│  │ RulesetHandlr│    │  │   │  ├─ Lorebook      │          │
│  │ CoC/DnDHandlr│    │  │   │  ├─ RecentTurns    │          │
│  └──────┬───────┘    │  │   │  └─ Memory检索    │          │
│         │            │  │   ├─ Director(低频)    │          │
│         ▼            │  │   ├─ Narrator(每轮)    │          │
│  ┌──────────────┐    │  │   ├─ RuleGuidance(规则)│          │
│  │  trpg.Service│◄───┼──┤   └─ MemoryService     │          │
│  │  (统一服务层) │    │  └──────────┬─────────────┘          │
│  └──────┬───────┘    │             │                        │
│         │            │             ▼                        │
│         ▼            │  ┌──────────────────────┐            │
│  ┌──────────────┐    │  │    World Engine      │            │
│  │   Ruleset    │    │  │  (SQLite 状态真相源)  │            │
│  │  CoC7/DnD5e  │    │  │  ├─ WorldState       │            │
│  └──────────────┘    │  │  ├─ EventLog         │            │
│                      │  │  ├─ Lorebook         │            │
│  ┌──────────────┐    │  │  ├─ NPC Moods        │            │
│  │  Admin API   │    │  │  ├─ Memory Store     │            │
│  │ (管理后台)    │    │  │  └─ AssetStore       │            │
│  └──────────────┘    │  └──────────────────────┘            │
└──────────────────────┴───────────────────────────────────┘
```

### 设计原则

1. **双渠道共享路由**：QQ 与 Web 同走 `core.Router`，指令逻辑零重复
2. **Service 层单一真相**：`trpg.Service` 为 Handler 和 Agent 共享的唯一游戏操作入口
3. **世界引擎单一写入**：所有状态变更经 `Engine.ApplyEvent()` 校验，防止 LLM 幻觉
4. **单轮单次 LLM**：每轮仅调用一次 Narrator LLM，Director 降频为 Planner
5. **规则优先**：能用规则解决的不用 LLM（指标评估、状态校验、情绪衰减、后果传播）
6. **上下文按预算**：设定库 + 记忆 + 近期对话按字符预算组装，长团不爆炸
7. **声明式注入**：Lorebook 关键词触发，零 LLM 调用，完全可测试

## 技术栈

- **语言**：Go 1.21+
- **QQ Bot SDK**：QQ 官方 WebSocket API v2
- **AI 框架**：[trpc-agent-go](https://github.com/trpc-group/trpc-agent-go)（Agent 编排 + Function Tools + Memory Extractor）
- **LLM**：DeepSeek（兼容 OpenAI 接口，可切换其他 Provider）
- **Web 框架**：[trpc-go](https://github.com/trpc-group/trpc-go) 泛 HTTP + WebSocket
- **前端**：Vue 3 + Vite + Element Plus
- **规则集**：CoC 7th Edition、DnD 5e
- **持久化**：SQLite（共享数据库 `data/app.db`：世界状态/角色卡/素材/聊天记录/运行时配置）
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
- 配置项通过 `config.Store` 管理，管理后台可热更新（Hot 级别）

## License

See [LICENSE](LICENSE) file for details.

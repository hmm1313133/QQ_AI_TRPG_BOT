# Voyage World Engine

纯 Web 的 AI 世界引擎 / 角色扮演平台。基于 AI 多层架构（Director + Narrator）、确定性世界引擎、设定库（Lorebook）与素材库，通过浏览器聊天页进行沉浸式文字扮演，并配备可视化管理后台（世界编辑器 / 素材库 / 配置中心）。

> 本项目由 QQ AI TRPG Bot 重构而来：已移除 QQ 机器人渠道与 TRPG 规则集/剧本系统，仅保留 Web 渠道与世界引擎扮演能力。最新版本进一步移除了旧版向量记忆系统（OpenViking / extractor 双轨写入 / 三因子检索），改为 SillyTavern 式滚动剧情摘要。

## 核心特性

### Web 渠道

浏览器聊天页（`/chat`）：WebSocket 实时对话 + REST 会话管理。

- **会话模式**：`normal`（纯对话，不注入世界上下文）/ `freechat`（世界内扮演，注入世界引擎上下文）
- **会话自恢复**：normal 模式下若世界状态仍在，自动恢复为 freechat
- **进入世界**：把管理后台创建的世界实例化复制到当前会话（整体快照，源世界不受影响）
- **玩家人设**：全局默认 + 每世界覆盖两层，注入叙事上下文
- **回复长度**：short / standard / detailed 三档（prompt 注入实现）
- **游玩存档**：保存 / 恢复 / 删除，恢复前自动备份，存档可带对话历史快照

玩家侧 API：

| 端点 | 说明 |
|------|------|
| `GET /api/session` | 签发会话令牌 |
| `GET /ws/chat` | WebSocket 聊天 |
| `GET\|PUT /api/chat/state` | 会话模式 / 回复长度 |
| `GET\|PUT /api/chat/persona` | 玩家人设（全局 + 每世界） |
| `POST /api/chat/world/enter` / `leave` | 进入 / 离开世界 |
| `GET\|POST /api/saves`、`POST /api/saves/{id}/restore` | 游玩存档 |
| `GET /api/worlds` | 可进入的世界列表 |

### AI 多层架构

采用 Director-Narrator 两层架构，实现**单轮单次 LLM 调用**：

| 层级 | 职责 | 调用频率 | 模型 |
|------|------|----------|------|
| **Director (Planner)** | 场景规划、节奏控制、下一步指令 | 低频（场景切换或每 N 轮，默认 8 轮） | LLM (低温度 0.2) |
| **Narrator** | 叙事生成、NPC 扮演、氛围描写 | 每轮一次 | LLM (高温度 0.7) |
| **MetricsEvaluator** | 指标评估（张力、混乱度、掌控权、目标进度） | 每轮（规则驱动，零 LLM） | 纯规则 |
| **RuleGuidance** | 规则指导（替代每轮 Director LLM） | 每轮（代码计算） | 纯规则 |
| **SummaryService** | 滚动剧情摘要（淘汰轮压缩） | 攒够批次异步触发（默认 6 轮） | LLM (低温度补全) |

**Turn Engine** 流程：`ContextBuilder 组装结构化输入 -> Narrator 生成叙事 -> ApplyEvent 更新世界状态 -> 短期窗口淘汰轮进入待总结队列 -> 异步合并进剧情摘要`

AI 唯一可调用的 Function Tool 是 `roll_dice`（复杂骰子表达式：`1d100`、`3d6`、`4d6kh3`、`2d6!`），用于叙事中的随机判定。

### Narrator 输出层 (`internal/agent/narrator.go`)

每轮无状态调用，直接走 `model.GenerateContent`，不再持有 llmagent/runner 持久会话：

- **标准 messages 结构**：system 主提示词 + 设定资料 user 消息 + 近期对话 user/assistant 消息对 + 本轮玩家 user 消息，对齐 DeepSeek/OpenAI chat completions 与 SillyTavern 用法
- **工具调用小循环**：内层最多 4 轮工具调用防死循环（`maxToolCallRounds`）
- **系统提示词每轮动态解析**：按世界预设/自定义现场构建，纯内存无 I/O

### 思维链 CoT (`internal/agent/cot.go`)

世界开启 `Style.EnableCoT` 后，每轮在回复风格分区之后注入输出格式要求，让模型先输出 `<think>` 思考段再输出正文；回收回复时剥离思考段，玩家只见正文。指导文本可由世界配置 `CoTGuide` 覆盖。

### 世界引擎 (`internal/world`)

所有游戏状态的**唯一真相源**（Single Source of Truth），SQLite 持久化：

- **单写入入口**：所有状态变更经 `Engine.ApplyEvent()` 校验后落库，防止 LLM 幻觉导致状态不一致
- **事件溯源**：EventLog 记录所有变更历史，支持回放与审计
- **后果传播**：事件触发级联效应（如 NPC 死亡 -> 盟友关系破裂 -> 阵营态度变化）
- **世界时钟**：跟踪游戏内时间，离线时自动演化（NPC 情绪衰减、定时事件结算）
- **NPC 情绪模型**：Valence-Arousal 二维情感空间，随时间和事件动态变化
- **双时序事实表**：易变事实带有效区间，旧事实失效而非覆盖
- **双模式支持**：SimRPG（模拟驱动）、Roleplay（自由扮演），模式即配置
- **状态锁定**：死亡/任务完成等关键事实不可逆，冲突变更被拒绝

### 设定库 Lorebook (`internal/world/lore.go`)

借鉴 SillyTavern World Info 的声明式上下文注入，**零 LLM 调用、完全可测试**：

- **关键词触发**：主键 + 次键（AND_ANY / AND_ALL / NOT_ANY 逻辑组合），子串匹配不区分大小写
- **恒定注入**：世界观核心设定常驻上下文，豁免预算裁剪
- **注入位置**：`front`（世界观区）/ `tail`（风格指令区），离输出越近影响力越强
- **优先级裁剪**：0-100 优先级，超出字符预算时按优先级降序裁剪
- **扫描窗口**：按近 N 轮对话文本扫描关键词（默认 4 轮，可配置），可选递归扫描
- **ST 格式导入**：支持 SillyTavern lorebook JSON 格式一键导入
- **命中测试**：管理后台提供实时命中测试

### 素材库 (`internal/assetparse` + `internal/world/asset_store.go`)

跨世界复用的创作素材，SQLite 持久化：

- **六大类型**：角色 / 地点 / 物品 / 势力 / 主线 / 世界观
- **LLM 解析**：粘贴文本，AI 自动提取结构化素材；支持 SillyTavern 角色卡（PNG/JSON）程序化解码
- **标签 + 搜索**：按类型、关键词、标签筛选
- **世界联动**：素材可导入世界、从世界收藏回库、跨世界复制

### 滚动剧情摘要 (`internal/agent/summary.go`)

SillyTavern 式记忆方案：近期对话逐字 + 远期剧情压缩摘要，替代传统向量检索记忆系统。**零外部依赖**，纯本地 SQLite + 低温度补全，无向量数据库。

- **RecentTurns 固定窗口**：环形保留最近 N 轮逐字对话（默认 10 轮），解决指代消解与叙事连续性，不参与预算裁剪
- **滚动摘要**：滑出窗口的淘汰轮进入 `PendingSummaryTurns`，攒够批次（默认 6 轮，可热更新）后异步调用 LLM 合并进 `CampaignSummary`
- **远期承载**：具体数值/剧情线由世界引擎（EventLog / ScenePlan / Metrics）承载，不进摘要
- **防并发**：按 worldID 互斥，避免同一世界并发总结

### 上下文预算 (`internal/agent/context_builder.go`)

基于字符预算的上下文组装，防止长会话上下文爆炸：

- **默认 45000 字符**（约 3 万 token），可在管理后台热更新
- **结构化输入**：返回 `NarratorInput`（ContextBlock + History + Tail），对话历史以独立消息对呈现
- **优先级分层**：必需段落（锁定事实/世界状态/NPC/长度/CoT）> 可选段落（设定库 tail）
- **三层记忆分工**：近期逐字 = RecentTurns；中期淘汰轮 = PendingSummaryTurns；远期剧情 = CampaignSummary
- **前缀缓存友好**：system 主提示词 + 设定资料块构成稳定前缀，吃厂商前缀缓存

### LLM 调试日志 (`internal/agent/llmlog.go`)

开发期排查提示词用，默认开启：

- 每次 LLM 调用（Narrator / Planner / Summary）的完整输入/输出追加到 `data/llm_debug/<日期>.jsonl`
- 记录 system prompt、user message、reply、耗时、错误
- `LLM_DEBUG_LOG=0` 关闭，`LLM_DEBUG_DIR` 自定义输出目录

## 管理后台

Web 管理后台（`/admin`）提供完整可视化管理，前端 Vue3 + Element Plus：

| 页面 | 功能 |
|------|------|
| **Dashboard** | 系统概览（运行时长、会话数等） |
| **世界管理** | 世界列表、手动创建（simrpg/roleplay）、世界编辑器（NPC/地点/势力/任务/关系/事实/事件日志/锁定/设定库/世界风格/CoT 配置） |
| **配置中心** | 运行时配置热更新（LLM 模型/温度/上下文预算/设定库参数/摘要批次等），敏感字段掩码 |
| **素材库** | 跨世界素材管理（角色/地点/物品/势力/主线/世界观），LLM 解析，标签搜索 |

### 配置热更新分级

| 生效级别 | 说明 | 示例 |
|----------|------|------|
| **Hot** | 下个回合读取即生效 | 上下文预算、设定库参数、摘要批次、计划间隔 |
| **Process Restart** | 重启进程后生效 | LLM 模型、API Key、温度、Web Token |

## 快速开始

### 环境要求

- Go 1.21+
- Node.js 18+（构建前端）
- LLM API Key（默认支持 DeepSeek，兼容 OpenAI 接口）

### 安装

```bash
git clone https://github.com/hmm1313133/QQ_AI_TRPG_BOT.git
cd QQ_AI_TRPG_BOT
go mod download
```

### 配置环境变量

```bash
# LLM 配置（默认 DeepSeek）
export LLM_PROVIDER=deepseek
export LLM_MODEL=deepseek-v4-flash
export LLM_API_KEY=your_api_key
export LLM_BASE_URL=https://api.deepseek.com

# 可选：Web 聊天访问令牌（为空则开放）
export WEB_CHAT_TOKEN=your_chat_token

# 可选：管理后台令牌（留空时仅允许本机 127.0.0.1 访问）
export ADMIN_TOKEN=your_admin_token

# 可选：数据目录（默认 ./data/app.db，SQLite 共享数据库）
export CONFIG_DB=./data/app.db
export WORLD_DIR=./data/worlds

# 可选：LLM 调试日志（每次调用完整输入/输出追加到 data/llm_debug/<日期>.jsonl）
export LLM_DEBUG_LOG=1
# export LLM_DEBUG_DIR=./data/llm_debug
```

> **提示**：LLM 配置等也可在管理后台「配置中心」修改，修改后按生效级别自动应用。首次启动时环境变量自动播种到 SQLite。

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

构建后访问 `http://localhost:8080/chat`（聊天页）与 `http://localhost:8080/admin`（管理后台）。Web 服务由 trpc-go 泛 HTTP 托管，监听地址在 `conf/trpc_go.yaml` 的 `server.service`（`trpc.voyage.web.Admin`）中配置。前端开发调试：`cd frontend && npm run dev`（已配置 /api、/ws 代理到 :8080）。

## 架构概览

```
┌──────────────────────────────────────────────────────────┐
│                  Web Chat (WS + HTTP)                     │
│                   core.Router (统一路由)                   │
├──────────────────────────────────────────────────────────┤
│                     Agent 层 (AI 驱动)                     │
│  ┌──────────────────────┐                                 │
│  │ KPAgent (对话入口)    │                                 │
│  │   ↓                   │                                 │
│  │ TurnEngine            │                                 │
│  │   ├─ ContextBuilder   │  返回 NarratorInput：           │
│  │   │  ├─ Lorebook      │   ContextBlock + History + Tail │
│  │   │  ├─ RecentTurns   │   (标准 messages 结构)          │
│  │   │  └─ CampaignSummary│                                │
│  │   ├─ Director(低频)   │                                 │
│  │   ├─ Narrator(每轮)   │  model.GenerateContent 直调     │
│  │   ├─ RuleGuidance(规则)│                                │
│  │   └─ SummaryService   │  淘汰轮→Pending→CampaignSummary │
│  └──────────┬─────────────┘                                │
│             ▼                                             │
│  ┌──────────────────────┐                                 │
│  │    World Engine      │                                 │
│  │  (SQLite 状态真相源)  │                                 │
│  │  ├─ WorldState       │                                 │
│  │  ├─ EventLog         │                                 │
│  │  ├─ Lorebook         │                                 │
│  │  ├─ NPC Moods        │                                 │
│  │  ├─ AssetStore       │                                 │
│  │  └─ PendingSummaryTurns│                               │
│  └──────────────────────┘                                 │
└──────────────────────────────────────────────────────────┘
```

### 设计原则

1. **世界引擎单一写入**：所有状态变更经 `Engine.ApplyEvent()` 校验，防止 LLM 幻觉
2. **单轮单次 LLM**：每轮仅调用一次 Narrator LLM，Director 降频为 Planner，Summary 异步批次
3. **规则优先**：能用规则解决的不用 LLM（指标评估、状态校验、情绪衰减、后果传播、规则指导）
4. **上下文按预算**：设定库 + 摘要 + 近期对话按字符预算组装，长会话不爆炸
5. **声明式注入**：Lorebook 关键词触发，零 LLM 调用，完全可测试
6. **滚动摘要替代向量检索**：SillyTavern 式方案，零外部依赖，纯本地 SQLite

## 技术栈

- **语言**：Go 1.21+
- **AI 框架**：[trpc-agent-go](https://github.com/trpc-group/trpc-agent-go)（model.Model 直调 + Function Tools）
- **LLM**：DeepSeek（兼容 OpenAI 接口，可切换其他 Provider）
- **Web 框架**：[trpc-go](https://github.com/trpc-group/trpc-go) 泛 HTTP + WebSocket
- **前端**：Vue 3 + Vite + Element Plus
- **持久化**：SQLite（共享数据库 `data/app.db`：世界状态/素材/人设/聊天记录/运行时配置/摘要）

## 开发

### 运行测试

```bash
go test ./...
```

### 代码结构约定

- 世界状态变更必须通过 `world.Engine`（`ApplyEvent` 单写入入口），不直接修改 `WorldState` 字段
- 配置项通过 `config.Store` 管理，管理后台可热更新（Hot 级别）
- LLM 调用统一经 `logLLMInteraction` 记录调试日志

## License

See [LICENSE](LICENSE) file for details.

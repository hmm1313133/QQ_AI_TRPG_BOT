# Voyage World Engine

纯 Web 的 AI 世界引擎 / 角色扮演平台。基于 AI 多层架构（Director + Narrator）、确定性世界引擎、记忆系统、设定库（Lorebook）与素材库，通过浏览器聊天页进行沉浸式文字扮演，并配备可视化管理后台（世界编辑器 / 素材库 / 配置中心）。

> 本项目由 QQ AI TRPG Bot 重构而来：已移除 QQ 机器人渠道与 TRPG 规则集/剧本系统，仅保留 Web 渠道与世界引擎扮演能力。

## 核心特性

### Web 渠道

浏览器聊天页（`/chat`）：WebSocket 实时对话 + REST 会话管理。

- **会话模式**：`normal`（纯对话，不注入世界上下文）/ `freechat`（世界内扮演，注入世界引擎上下文）
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

**Turn Engine** 流程：`ContextBuilder 组装上下文 -> Narrator 生成叙事 -> ApplyEvent 更新世界状态 -> 异步写入记忆`

AI 唯一可调用的 Function Tool 是 `roll_dice`（复杂骰子表达式：`1d100`、`3d6`、`4d6kh3`、`2d6!`），用于叙事中的随机判定。

### 世界引擎 (`internal/world`)
所有游戏状态的**唯一真相源**（Single Source of Truth），SQLite 持久化：

- **单写入入口**：所有状态变更经 `Engine.ApplyEvent()` 校验后落库，防止 LLM 幻觉导致状态不一致
- **事件溯源**：EventLog 记录所有变更历史，支持回放与审计
- **后果传播**：事件触发级联效应（如 NPC 死亡 -> 盟友关系破裂 -> 阵营态度变化）
- **世界时钟**：跟踪游戏内时间，离线时自动演化（NPC 情绪衰减、记忆遗忘、定时事件结算）
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

### 记忆系统 (`internal/agent/memory`)
双轨写入 + 三因子检索：

- **规则侧**：状态事件（NPC 态度变化、线索发现等）由规则引擎自动写入
- **框架侧**：对话内容通过 trpc-agent-go 的 extractor 提取关键信息（ADD/UPDATE/DELETE/NOOP 算子集）
- **反思压缩**：周期性生成高层洞察，附带证据引用链
- **检索排序**：`Score = α·Recency(0.995^Δh) + β·Importance + γ·Relevance`
- **Pinned 记忆**：里程碑事件（背叛/结盟/死亡）永不压缩遗忘

### 上下文预算 (`internal/agent/context_builder`)
基于字符预算的上下文组装，防止长会话上下文爆炸：

- **默认 45000 字符**（约 3 万 token），可在管理后台热更新
- **优先级分层**：必需段落（锁定事实/世界状态/NPC）> 可选段落（记忆/设定库/近期对话）
- **RecentTurns 窗口**：环形保留最近 10 轮逐字对话，解决指代消解与叙事连续性
- **三层记忆分工**：近期逐字 = RecentTurns；中期事实 = MemoryService；远期剧情 = CampaignSummary

## 管理后台

Web 管理后台（`/admin`）提供完整可视化管理，前端 Vue3 + Element Plus：

| 页面 | 功能 |
|------|------|
| **Dashboard** | 系统概览（运行时长、会话数等） |
| **世界管理** | 世界列表、手动创建（simrpg/roleplay）、世界编辑器（NPC/地点/势力/任务/关系/事实/事件日志/锁定/设定库） |
| **配置中心** | 运行时配置热更新（LLM 模型/温度/上下文预算/设定库参数/记忆抽取开关等），敏感字段掩码 |
| **素材库** | 跨世界素材管理（角色/地点/物品/势力/主线/世界观），LLM 解析，标签搜索 |
| **记忆管理** | 查看世界/NPC 记忆条目、重要性、标签 |

### 配置热更新分级

| 生效级别 | 说明 | 示例 |
|----------|------|------|
| **Hot** | 下个回合读取即生效 | 上下文预算、设定库参数、记忆抽取开关、计划间隔 |
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

# 可选：管理后台令牌
export ADMIN_TOKEN=your_admin_token

# 可选：数据目录（默认 ./data/app.db，SQLite 共享数据库）
export CONFIG_DB=./data/app.db
export WORLD_DIR=./data/worlds
export MEMORY_DIR=./data/memories

# 可选：OpenViking 记忆服务
export OPENVIKING_ENABLED=false
export OPENVIKING_BASE_URL=http://localhost:1933

# 可选：记忆提取器
export MEMORY_EXTRACTOR_ENABLED=true
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
│  │   ├─ ContextBuilder    │                                │
│  │   │  ├─ Lorebook      │                                │
│  │   │  ├─ RecentTurns    │                               │
│  │   │  └─ Memory检索    │                                │
│  │   ├─ Director(低频)    │                                │
│  │   ├─ Narrator(每轮)    │                                │
│  │   ├─ RuleGuidance(规则)│                                │
│  │   └─ MemoryService     │                                │
│  └──────────┬─────────────┘                                │
│             ▼                                             │
│  ┌──────────────────────┐                                 │
│  │    World Engine      │                                 │
│  │  (SQLite 状态真相源)  │                                 │
│  │  ├─ WorldState       │                                 │
│  │  ├─ EventLog         │                                 │
│  │  ├─ Lorebook         │                                 │
│  │  ├─ NPC Moods        │                                 │
│  │  ├─ Memory Store     │                                 │
│  │  └─ AssetStore       │                                 │
│  └──────────────────────┘                                 │
└──────────────────────────────────────────────────────────┘
```

### 设计原则

1. **世界引擎单一写入**：所有状态变更经 `Engine.ApplyEvent()` 校验，防止 LLM 幻觉
2. **单轮单次 LLM**：每轮仅调用一次 Narrator LLM，Director 降频为 Planner
3. **规则优先**：能用规则解决的不用 LLM（指标评估、状态校验、情绪衰减、后果传播）
4. **上下文按预算**：设定库 + 记忆 + 近期对话按字符预算组装，长会话不爆炸
5. **声明式注入**：Lorebook 关键词触发，零 LLM 调用，完全可测试

## 技术栈

- **语言**：Go 1.21+
- **AI 框架**：[trpc-agent-go](https://github.com/trpc-group/trpc-agent-go)（Agent 编排 + Function Tools + Memory Extractor）
- **LLM**：DeepSeek（兼容 OpenAI 接口，可切换其他 Provider）
- **Web 框架**：[trpc-go](https://github.com/trpc-group/trpc-go) 泛 HTTP + WebSocket
- **前端**：Vue 3 + Vite + Element Plus
- **持久化**：SQLite（共享数据库 `data/app.db`：世界状态/素材/人设/聊天记录/运行时配置）
- **可选记忆服务**：[OpenViking](https://github.com/Tencent/OpenViking)

## 开发

### 运行测试

```bash
go test ./...
```

### 代码结构约定

- 世界状态变更必须通过 `world.Engine`（`ApplyEvent` 单写入入口），不直接修改 `WorldState` 字段
- 配置项通过 `config.Store` 管理，管理后台可热更新（Hot 级别）

## License

See [LICENSE](LICENSE) file for details.

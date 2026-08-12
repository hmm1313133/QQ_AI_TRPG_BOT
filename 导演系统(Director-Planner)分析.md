# 导演系统（Director / Planner 层）分析

> 本文基于当前代码实现逐行核实，分析 Director（Planner）层在架构中的定位、实际作用与完整执行流程。所有结论均标注代码位置（`path:line`）。

## 1. 架构定位：从"每轮决策者"到"低频场景规划器"

Director 已经从早期设计中"跟随每条玩家消息的决策者"**降级为低频 Planner**。代码注释明确记录了这次演变：

- `internal/agent/director.go:5`：`（P2 起降级为低频 Planner 使用，不再跟随每条玩家消息。）`
- `internal/agent/turn_engine.go:1-14`：TurnEngine "替代旧 KPPipeline（Director -> Narrator 每轮双 LLM）"，同步路径上只有 Narrator 一次必需 LLM 调用，Director 仅在场景切换或每 `planInterval` 轮触发一次。
- 每轮的叙事指导改由**规则化 RuleGuidance**（零 LLM、确定性）承担：`internal/agent/guidance.go:1-6` 注释明确"替代每轮一次的 Director LLM……源自旧 Director.fallbackDirective 的逻辑转正"。

当前三层结构：

| 层 | 组件 | 调用频率 | LLM 调用 |
|---|---|---|---|
| 决策层 | Director LLM → `DecisionDirective` → `WorldState.ScenePlan` | 低频（场景切换 / 每 N 轮） | 低频 1 次 |
| 指导层 | `AIGuidance`（AI 叙事指导，可回写 state_updates；超时/失败降级 `BuildGuidance` 规则版）+ `MetricsEvaluator`（指标） | 每轮 | 每轮 1 次（低成本模型，5s 超时） |
| 输出层 | Narrator 无状态调用 | 每轮 | 每轮 1 次（必需） |

> **更新说明**：指导层原为纯规则的 `BuildGuidance`（零 LLM），后改为 `AIGuidance` 每轮同步调用低成本 LLM（`internal/agent/ai_guidance.go`），规则版保留为降级兜底。改动动机：规则指导的指标只反映世界配置而非叙事状态，且 Narrator 行为无法反馈指标（开环）；AI 指导读取真实叙事上下文并可经 `state_updates` 回写世界状态，使指标被叙事驱动（闭环）。

这一设计服务于 `context_builder.go:1-9` 的设计宣言：**成本 = f(回合复杂度)，而非 f(历史长度)**。Director 低频化是控制成本的关键手段。

## 2. 输入与输出

### 2.1 输入（`Director.Decide`，`internal/agent/director.go:83-89`）

1. `state *world.WorldState` —— 完整世界运行态（场景、NPC、任务、事件队列、指标等），经 `buildDirectorUserMessage` 序列化为 JSON 注入（`internal/agent/director_prompt.go:77-84`）。
2. `playerMessage string` —— 玩家最新消息（`director_prompt.go:105-106`）。
3. `scriptContext string` —— 世界设定上下文，由调用方 `refreshPlan` 组装：lore 命中条目（预算 `lore_budget*2`）+ 兜底 Background + 全部阶段摘要拼接（`internal/agent/turn_engine.go:359-372`）。
4. `sessionID string` —— 仅用于生成一次性会话 ID 和日志。

用户消息还包含：规则化预评估指标（张力/混乱/掌控权/目标进度/轮次，`director_prompt.go:86-91`）、当前场景计划 `ScenePlan`（`director_prompt.go:99-103`）。

**注意**：Director 是**无状态会话**——每次调用用 `fmt.Sprintf("%s-plan-%d", sessionID, time.Now().UnixNano())` 生成新 session，历史不累积（`director.go:99-106`）。

### 2.2 输出：`DecisionDirective`（`internal/agent/directive.go:7-13`）

| 字段 | 类型 | 说明 | 实际去向 |
|---|---|---|---|
| `assessment` | `SceneAssessment` | 局势评估：tension/chaos/agency/progress/overall（`directive.go:16-22`） | 仅 `overall_situation` 被 `formatScenePlan` 写入 ScenePlan 文本（`turn_engine.go:415-417`）；其余四个字段**无任何消费方** |
| `narration_guide` | `NarrationGuide` | tone / pacing / focus_points / npc_behavior / constraints（`directive.go:25-31`） | `focus_points`、`npc_behavior`、`constraints` 进入 ScenePlan 文本（`turn_engine.go:403-411`）；**tone 和 pacing 被丢弃**（formatScenePlan 不读这两个字段） |
| `actions` | `[]DirectorAction` | type（advance_timeline / trigger_event / introduce_npc / add_clue / adjust_difficulty）+ description + target（`directive.go:34-38`） | **仅被序列化为"计划动作：[type] description"文本进 ScenePlan（`turn_engine.go:412-414`），没有任何代码执行这些动作**。项目自己的分析文档也确认了这点：`AI多层架构分析.md:54` 指出"没有任何代码执行它们……决策层与执行层之间没有闭环" |
| `state_updates` | `[]StateUpdate` | type（npc_disposition / hidden_discovered / event_triggered / objective_completed）+ target + value（`directive.go:42-46`） | **唯一被真正执行的输出**：转为 `WorldEvent{Actor:"planner"}` 经 `worldEngine.ApplyEvents` 落库（`turn_engine.go:385-394`），由 `world.Engine.ApplyEvent` 的 switch 实际修改状态并写入 EventLog（`internal/world/engine.go:249-317`） |
| `reasoning` | string | 决策推理 | **无任何消费方**（不落库、不注入） |

### 2.3 消费方

唯一的直接消费方是 `TurnEngine.refreshPlan`（`turn_engine.go:356-398`）。间接消费链：

- `ScenePlan` 文本 → ContextBuilder 的可选分区 `scene_plan`（优先级 5，超预算可裁）（`internal/agent/context_builder.go:179-184`）→ Narrator 的设定资料块。
- `StateUpdates` → world 引擎单写入口落库 → 影响后续指标评估与状态摘要。

**Narrator 不直接接收 `DecisionDirective` 结构体**。`narrator.go:3` 注释"接收 DecisionDirective 作为约束"是过时注释——实际 Narrator 只拿到 `NarratorInput`（含 ScenePlan 文本 + RuleGuidance 文本），见 `narrator.go:107-113` 与 `context_builder.go:100-111`。

## 3. Director 现在实际发挥的作用

以代码实际行为为准，Director 目前承担五项职责：

### 3.1 低频场景计划生成（核心职责）

产出 ScenePlan 文本（叙事重点 / NPC 行为 / 约束 / 计划动作 / 局势判断），供后续若干轮注入 Narrator。
位置：`turn_engine.go:356-398`（refreshPlan）、`turn_engine.go:400-419`（formatScenePlan）。

### 3.2 状态变更落库（受限执行）

`state_updates` 中四类变更（NPC 态度、线索发现、事件触发、目标完成）经 ApplyEvents 真正改写 WorldState（`turn_engine.go:385-394`；`internal/world/engine.go:258-265`）。这是 Director 输出中**唯一形成闭环**的部分。命中失败会记日志"ApplyEvent 未命中"（`engine.go:313`）。

### 3.3 剧情节奏控制（间接、建议性）

通过 `narration_guide` 的 focus_points / constraints 文本影响 Narrator。每轮的叙事指导由指导层承担：**当前为 `AIGuidance`**（`ai_guidance.go`：读取场景/NPC/任务/最近对话/玩家最新行动，LLM 生成基调/节奏/关注/约束，并可输出 `state_updates` 回写世界状态）；AI 不可用时的规则兜底 `BuildGuidance`（`guidance.go:24-75`：张力>80 给喘息空间、张力<20 引入威胁、混乱>70 收束线索、掌控权<30 提供选择、进度满引导推进、停滞检测 `guidance.go:70-72`）。

### 3.4 LLM 失败时的规则化兜底

`fallbackDirective`（`director.go:175-218`）：进度满建议 advance_timeline、张力>80 加喘息约束、张力<20 建议 adjust_difficulty；基调/节奏由 `recommendTone` / `recommendPacing`（`director.go:221-243`）推导。保证 Director LLM 不可用时系统仍能产出合理计划。

### 3.5 为摘要服务提供 LLM 通道

`Director.rawCompletion` 被 `SummaryService` 复用做阶段摘要生成（`summary.go:44`、`director.go:149-172`）。注意 Director 只提供通道，**摘要触发逻辑与 Director 无关**（见第 6.2 节）。

### 3.6 澄清：代码中不存在的"功能"

以下能力容易被误认为属于 Director，但代码中并不存在：

- **内容安全审查**：全仓无任何 Director 侧的内容安全/审核逻辑。
- **长度控制**：与 Director 无关，是会话级 `reply_length` 偏好经 prompt 尾部注入实现（`internal/agent/length.go:22-26`、`turn_engine.go:193-194`）。
- **摘要触发决策**：Director 不决定何时摘要（见第 6.2 节）。
- **幕/场景推进的自动执行**：`advance_timeline` 等 action 不会被任何代码消费，只是建议文本；场景切换判断（`PlanNodeID != Scene.NodeID`，`turn_engine.go:339`）只触发"重新规划"，真正改 Scene 的是 world 引擎其他路径。
- **NPC 调度的强制执行**：NPC 行为只是 ScenePlan 里的建议文本；NPC 态度变化需走 `state_updates` 且依赖 target 名称命中（`engine.go:320-336` applyNPCDisposition）。

## 4. 完整执行流程（一条玩家消息）

### 4.1 调用链

```
WS 消息
  └─ internal/web/server.go:236        go s.router.Route(mc, ...)   （每个 WS 消息一个 goroutine）
      └─ internal/core/router.go:28    Route
          └─ internal/core/plugin.go:116   agent.Chat(ctx, session)
              └─ internal/agent/kp_agent.go:117-132   KPAgent.Chat
                  └─ internal/agent/turn_engine.go:112   TurnEngine.Run
```

### 4.2 `TurnEngine.Run` 内部步骤（`turn_engine.go:112-217`）

1. **会话锁**：`worldEngine.Lock(sessionID)`，同一世界回合串行化（`turn_engine.go:122-125`）。
2. **状态解析**：`resolveState`（`turn_engine.go:309-321`）——无世界状态走**纯对话路径**，直接 Narrator，完全不经过指标/指导/Planner（`turn_engine.go:134-141`）。
3. **回合快照**：供"AI 回复重试"回滚；重试重跑带 `ExtraKeyRegenRetry` 标记则跳过（`turn_engine.go:147-151`、`turn_engine.go:36-45`；web 侧 `internal/web/server.go:308`）。
4. **热更新配置读取**：`context_budget` / `lore_budget` / `lore_scan_rounds` / `lore_recursion` 每回合读（`turn_engine.go:154-162`）。
5. **lore 检索**：`world.Resolve` 按需召回设定条目（`turn_engine.go:165-171`）。
6. **世界时钟**：离线演化 + 本回合时间推进（`turn_engine.go:174`、`423-450`）。
7. **规则化指标评估**：`metrics.Evaluate(state)`（`turn_engine.go:177-179`；算法在 `director_metrics.go:25-144`，确定性，相同状态必得相同分数）。
8. **叙事指导**：`applyGuidance`（`turn_engine.go`）——AI 指导开启时（`mode.EnablePlanner` 且配置 `ai_guidance=true`）同步调用 `AIGuidance.Guide`（5s 超时），其 `state_updates` 经 `ApplyEvents` 回写（Actor="guidance"）；超时/失败/禁用降级 `BuildGuidance(state)` 规则版。每轮记录一条运行日志（见 6.5）。
9. **低频 Planner 判断与调用**（`turn_engine.go:185-187`），触发条件 = `mode.EnablePlanner`（仅 simrpg 为 true，roleplay 为 false，`internal/world/mode.go:26,32`）且 `needsPlan`（`turn_engine.go:332-351`）为真——三者任一：
   - `ScenePlan == ""`（首次规划）；
   - `PlanNodeID != Scene.NodeID`（场景切换）；
   - `RoundCount - PlanRound >= interval`（间隔到期；interval 热读 `plan_interval`，默认 8，`turn_engine.go:343-346`、`DefaultPlanInterval` 在 `turn_engine.go:32`）。
10. **refreshPlan**（同步阻塞本回合）：组装 scriptContext → `director.Decide` → ScenePlan 落 state → StateUpdates 经 ApplyEvents 落库（`turn_engine.go:356-398`）。
11. **上下文组装**：`ContextBuilder.BuildNarratorInput` 按字符预算（默认 45000 ≈ 3 万 token，`context_builder.go:20`）贪心装配分区（`context_builder.go:100-227`）。
12. **Narrator 无状态调用**：唯一同步必需 LLM（`turn_engine.go:200`；`narrator.go:107-152`，含 roll_dice 工具循环，上限 4 轮 `narrator.go:28,186-215`）。
13. **记账持久化**：`bookkeep`（`turn_engine.go:454-500`）——重新加载最新状态叠加增量，短期窗口淘汰轮入 `PendingSummaryTurns`。
14. **异步摘要**：`go t.summary.MaybeSummarize(sessionID)`（`turn_engine.go:209-211`）——**唯一的异步 LLM**，仅当 `mode.EnableMemory && summary != nil`。

### 4.3 同步/异步与时机结论

- Director **不是每轮调用**；是同步低频调用，触发时会拉长该回合响应时间。
- **AIGuidance 是每轮同步调用**（5s 超时，超时即降级规则版），为同步路径上的第二次 LLM 调用。
- 摘要 LLM 异步（goroutine）；其余全部同步。

### 4.4 失败降级策略（四层）

1. **AIGuidance 超时（5s）/ 调用失败 / 空回复 / JSON 解析失败** → `BuildGuidance` 规则版指导，流程继续（`ai_guidance.go` Guide/fallback）。
2. **Director LLM 失败 / 空回复 / 无 JSON / JSON 解析失败** → `fallbackDirective` 规则化指令，流程继续（`director.go:107-139`）。
3. **refreshPlan 整体失败** → 记日志"沿用旧计划"，不影响本回合叙事（`turn_engine.go` refreshPlan）。
4. **TurnEngine 执行失败** → KPAgent 回退到单 agent 模式 `chatSingleAgent`（`kp_agent.go:121-131,135-196`）。

另有摘要失败 → 待总结队列保留、下轮重试（`summary.go:91-99`）。

### 4.5 指标与日志

- 指标评估本身无独立持久化指标系统，`state.Metrics` 随 WorldState 落库（`turn_engine.go:464`）。
- LLM 调试日志：`logLLMInteraction` 落盘 `data/llm_debug/<日期>.jsonl`，planner 类记录含 systemPrompt / userMessage / reply / elapsed_ms（`director.go:109-122`；`llmlog.go:48-98`），`LLM_DEBUG_LOG=0` 关闭。
- 运行日志：`[Director] 预评估/决策完成`（`director.go:94-97,142-143`）、`[TurnEngine] 场景计划已刷新`（`turn_engine.go:396-397`）、`[World] ApplyEvent 未命中/拒绝`（`engine.go:307,313`）。

## 5. 导演提示词（`internal/agent/director_prompt.go:13-70`）

### 5.1 职责定义

分析游戏状态（GameState JSON + 预评估指标 + 玩家消息 + 世界设定），输出下一轮决策指令 JSON。

### 5.2 七条硬约束（`director_prompt.go:28-36`）

1. 决策必须基于 GameState 和指标，不能凭空编造元素；
2. 保持跨轮一致性（不突变 NPC 性格 / 已确认事实）；
3. 张力 > 70 给玩家喘息或降难度；
4. 张力 < 20 引入新威胁 / 紧迫感；
5. 掌控权 < 30 提供更多选择和线索；
6. 目标进度 = 100 建议推进下一剧情节点；
7. `state_updates` 只能改已存在状态，不能创建新元素。

### 5.3 输出格式约束

**纯 JSON，不允许 markdown 代码块或任何其他文本**（`director_prompt.go:37`），并给出完整 JSON 模板（`director_prompt.go:40-70`）。

代码侧容错：`extractAgentJSON` 仍会尝试剥 ```` ```json ```` 代码块、提取首尾花括号（`director.go:265-301`）。值得注意的张力：提示词要求"纯 JSON 不带代码块"，但解析器却兼容代码块——说明实际模型输出经常不守约束。

## 6. 配置项与子系统协作

### 6.1 配置项

| 键 | 定义 | 生效级别 | 实际读取点 | 备注 |
|---|---|---|---|---|
| `director_temp` | `internal/config/store.go:26`，注册于 `store.go:65`（Label "Planner 温度"） | process-restart | **没有任何代码读取它** | main.go 只 Seed（`cmd/bot/main.go:134`）；Director 温度来自 `agent.Config.DirectorTemperature`（main.go 硬编码 0.2，`cmd/bot/main.go:97`；`director.go:39-42`）。**这是一个注册但失效的配置项** |
| `plan_interval` | `store.go:31,71` | hot | `turn_engine.go` needsPlan 每轮读 | 默认 8（`turn_engine.go` DefaultPlanInterval；Seed 于 main.go） |
| `ai_guidance` | `store.go`（KeyAIGuidance） | hot | `turn_engine.go` applyGuidance 每轮读 | 默认 true；关闭后退回纯规则指导 |
| `summary_chunk_size` | `store.go:32,70` | hot | `summary.go:50-57` | 默认 6（`summary.go:23`） |
| `context_budget` / `lore_budget` / `lore_scan_rounds` / `lore_recursion` | `store.go:27-30` | hot | `turn_engine.go:157-161`；Planner 的 lore 预算 = `lore_budget*2`（`turn_engine.go:359`） | |
| Director MaxTokens | 无运行时配置键 | — | `agent.Config.DirectorMaxTokens`，main.go 硬编码 2048（`cmd/bot/main.go:98`；`director.go:43-46`） | |
| `EnablePlanner` | `internal/world/mode.go:16` | 模式预设 | `turn_engine.go:185` | simrpg=true / roleplay=false（`mode.go:26,32`） |

热更新白名单见 `store.go:91-98`。

### 6.5 运行可视化（管理后台）

- 每轮指导与每次计划刷新记录一条 `DirectorLogEntry`（时间/会话/轮次/kind/source/耗时/指导内容/state_updates 命中数/降级原因），存全局环形缓存（100 条，`internal/agent/director_log.go`）。
- 管理端 API：`GET /api/admin/director/logs?session_id=&limit=`（`internal/web/admin.go` handleDirectorLogs，Bearer 鉴权）。
- 前端页面：管理后台"导演系统"页（`frontend/src/views/admin/DirectorView.vue`），含统计卡片（AI/兜底占比、状态更新命中率、平均耗时）与可展开明细表格。

### 6.2 与 Summary（摘要）的协作

- Director 与摘要**没有决策关系**：触发条件是"RecentTurns 窗口（容量 10，`internal/world/types.go:124`）淘汰轮攒够 `summary_chunk_size`（默认 6）"，由 `SummaryService.MaybeSummarize` 在回合结束后异步执行（`turn_engine.go:208-211`；`summary.go:62-110`）。
- 协作点仅两处：
  1. SummaryService 复用 `director.rawCompletion` 作为低温度补全通道（`summary.go:44`）；
  2. 阶段摘要会被拼进 Planner 的 scriptContext（`turn_engine.go:366-372`），即"摘要喂养规划"。

### 6.3 与 Guidance（叙事指导）的协作

- 指导层现状：**`AIGuidance` 为主（每轮同步 LLM），`BuildGuidance` 规则版为兜底**。AI 指导读取真实叙事上下文（场景/NPC/任务/最近对话/玩家最新行动），输出基调/节奏/关注/约束 + 可选 `state_updates`。
- AI 指导的 `state_updates` 与 Planner 走同一单写入口 `ApplyEvents`（Actor="guidance"），同一套类型白名单与 target 命中校验；这使 Narrator 的叙事行为能反馈到 WorldState，下一轮指标评估随之变化——**指导层从开环变为闭环**。
- 与 Planner 的分工：Guidance 管"本轮怎么讲"（每轮、低成本模型、priority 1 必需分区注入 Narrator）；Director 管"接下来几轮讲什么"（低频、可裁剪的 scene_plan 分区）。
- 兜底规则版与 Planner 的 `fallbackDirective`/`recommendTone`/`recommendPacing` 规则高度同构（`guidance.go:24-75` vs `director.go:175-243`），二者仍是两份独立维护的规则拷贝，阈值存在漂移（喘息阈值 80 vs 70），属已知技术债。

### 6.4 与 world 引擎的协作

- Planner 的 StateUpdates 走 world 引擎单写入口 `ApplyEvents`（`turn_engine.go:392`；`engine.go:302-317`），Actor 标记为 `"planner"`（`turn_engine.go:389`），命中后写入 `EventLog` 并可触发 consequence 传播（`engine.go:292-297`）。
- Planner 产出的 ScenePlan / PlanNodeID / PlanRound 存于 WorldState（`internal/world/types.go:58-61`），在 bookkeep 时合并回最新状态（`turn_engine.go:465-469`）。

## 7. 总结：声明与实现的落差

Director 层目前处于"半闭环"状态：

- **已闭环**：`state_updates` → world 引擎落库，是唯一能真正改变世界状态的输出。
- **半闭环**：ScenePlan 文本 → 注入 Narrator 提示词，影响力是软性、可裁剪的。
- **未闭环**：`actions` 无任何执行方；`assessment` 四个子字段、`reasoning`、`narration_guide.tone/pacing` 被丢弃；`director_temp` 配置项注册但无代码读取。

决策层与执行层之间没有闭环是**已知且被项目自己记录的问题**（`AI多层架构分析.md:54`）。阅读或扩展 Director 相关代码时，应以本文第 2.2 节的"实际去向"列为准，而非结构体字段名所暗示的语义。

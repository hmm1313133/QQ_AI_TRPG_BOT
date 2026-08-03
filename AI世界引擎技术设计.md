# AI 世界引擎技术设计

> **版本**: v1.0（草案）
> **日期**: 2026-08-03
> **前置文档**: `AI多层架构分析.md`（现状问题分析）、`Voyage_World_Engine_Technical_Report.md`（参考架构）
> **适用范围**: QQ_AI_TRPG_BOT 的 AI 层演进设计。当前支撑「AI 跑团（TRPG 带团）」，架构上预留「文字 RPG 模拟」「AI 角色扮演」两种后续业务模式。

---

## 一、背景与目标

### 1.1 现状的局限

当前架构（Director → Narrator → GameState）在短团、剧本驱动场景下可以工作，但面对真实需求存在四个结构性缺口：

| 需求 | 现状 | 缺口 |
|------|------|------|
| **长团**（数百~数千回合） | LLM 会话历史线性增长，GameState 只有当前节点快照 | 无分层记忆，无历史压缩，上下文必然爆炸 |
| **世界随时间变动** | TimelineEngine 只有"无进展提醒"定时器 | 无世界时钟、无事件调度、无离线演化，玩家不在场时世界是冻结的 |
| **人物技能成长** | `coc7.SkillGrowth` 已存在但需手动触发 | 无"成功技能使用记录 → 幕间自动成长结算"的闭环 |
| **NPC 性格/状态模拟** | NPCState 只有一个 `Disposition` 枚举字段 | 无情绪状态、无记忆、无关系网络，NPC 是"剧本卡片"而非"智能体" |

### 1.2 三种业务模式

架构必须用一个内核支撑三种模式（模式 = 配置，而非三套代码）：

| 模式 | 驱动方式 | 规则层 | 模拟层 | 记忆层 | 状态 |
|------|---------|--------|--------|--------|------|
| **AI 跑团**（现有） | 剧本时间轴 + 玩家行动 | 全开（CoC7/DnD5e） | 半开（NPC 仅在场时交互） | 中（会话+战役摘要） | 已上线 |
| **文字 RPG 模拟** | 开放世界 + 玩家行动 | 简化（轻量属性/HP） | 全开（NPC 目标驱动、世界 tick、离线演化） | 全开 | 预留 |
| **AI 角色扮演** | 单/少 NPC 深度对话 | 关闭 | 仅情绪/关系衰减 | 全开（单 NPC 深度记忆） | 预留 |

### 1.3 设计目标

1. **长程一致性**：数千回合后，NPC 仍记得玩家做过什么，世界事实不自相矛盾。
2. **世界是活的**：有时间、有事件调度、有离线演化，NPC 有自己的目标和生活。
3. **成长有闭环**：技能使用 → 记录 → 幕间结算 → 角色卡更新，全自动。
4. **NPC 是智能体**：有记忆、有情绪、有关系、有目标，而非一次性对话树。
5. **成本可控**：每回合 LLM 调用次数有硬预算，便宜模型做粗活，强模型只负责叙事。

### 1.4 非目标

- 不做多人在线共享世界（QQ 群 = 一个独立世界实例，不做跨群同步）。
- 不做实时战斗系统（保持回合制、叙述驱动）。
- 不追求 Voyage 的多模态（图像/音频），文本优先。

---

## 二、设计原则

以 Voyage World Engine 的核心哲学为基础，结合本项目教训（见 `AI多层架构分析.md`）：

1. **引擎负责真相，AI 负责叙事（双轨架构）**
   一切客观事实（HP、物品、位置、关系、任务状态、谁死了）存储在确定性的世界状态层，由代码读写；LLM 只在状态框架内做创意填充。LLM 永远不能直接"说"一个事实成立，只能通过工具/结构化输出**请求**状态变更，由引擎校验后落库。

2. **能写代码的不用 LLM**
   指标计算、时间推进、关系传播、成长结算、状态合并——全部代码化。LLM 只做三件事：理解玩家意图、生成叙事文本、做语义判断（如重要性评分）。这是成本控制的第一原则，也是一致性的第二保障。

3. **单一写入者**
   任何状态变更都必须经过 `WorldEngine.ApplyEvent()` 一个入口。禁止"Director 写一份、Narrator 工具写一份、pipeline 末尾再覆盖一份"的多写入路径（现状 bug 的根因）。

4. **模式即配置**
   TRPG / RPG 模拟 / 角色扮演 = 同一引擎内核 + 不同的子系统开关、规则集、提示词模板、工具集。新增模式不改引擎。

5. **记忆分层，上下文按预算组装**
   永远不把全量历史喂给 LLM。每回合按 token 预算从各层记忆中检索、组装"上下文包"，超预算的部分先压缩再丢弃。

6. **低频决策，高频叙事**
   规划（Planner）按场景/幕粒度运行，不跟随每条玩家消息；反思（Reflector）按阈值异步触发。每回合只有叙事是同步必需的 LLM 调用。

---

## 三、总体架构

### 3.1 分层视图

```
┌────────────────────────────────────────────────────────────────┐
│ 接口层    QQ Bot（现有）/ CLI / 未来: Web API                    │
├────────────────────────────────────────────────────────────────┤
│ 模式层    TRPGMode ｜ SimRPGMode ｜ RoleplayMode               │
│          （子系统开关 + 规则集 + Prompt 模板 + 工具集配置）        │
├────────────────────────────────────────────────────────────────┤
│ 叙事层    Narrator（叙事生成，同步）                             │
│          Planner（场景/幕级规划，低频）                          │
│          Reflector（记忆压缩与洞察，异步）                       │
├────────────────────────────────────────────────────────────────┤
│ 模拟层    NPCAgent（感知→记忆→决策→行动，批量 tick）            │
│          EmotionModel（情绪状态+衰减）                           │
│          WorldSimulator（世界时钟、事件调度、离线演化）           │
│          ConsequenceEngine（关系/声誉/因果传播）                 │
├────────────────────────────────────────────────────────────────┤
│ 规则层    trpg.Service（现有：骰子/检定/SAN/成长）               │
│          ProgressionEngine（成长结算自动化）                     │
├────────────────────────────────────────────────────────────────┤
│ 记忆层    四层记忆（核心身份/长期/短期/工作）                    │
│          检索（recency+importance+relevance）｜ OpenViking      │
├────────────────────────────────────────────────────────────────┤
│ 世界状态层 WorldState（实体/关系/时钟/事件日志）                 │
│          ApplyEvent 单写入入口 ｜ 状态锁定 ｜ 不变量校验          │
├────────────────────────────────────────────────────────────────┤
│ 存储层    StateRepository 接口                                  │
│          实现1: JSON 文件（现状迁移） 实现2: SQLite（预留）       │
└────────────────────────────────────────────────────────────────┘
```

### 3.2 与 Voyage World Engine 的对应关系

| Voyage 概念 | 本设计 | 说明 |
|------------|--------|------|
| Narrative Engine | 叙事层 Narrator | 职责相同，输入从"状态查询"改为"按预算组装的上下文包" |
| Mechanics Engine | 规则层 trpg.Service + ProgressionEngine | 本项目已具备（骰子/检定/SAN），只需补成长闭环 |
| World State Tracker | 世界状态层 WorldState | 核心资产，从现有 GameState 演化扩展 |
| Character Memory System | 记忆层（四层模型） | 借鉴 Generative Agents 的检索与反思机制 |
| Relationship & Consequence Engine | ConsequenceEngine | 首版规则化传播，不上 LLM |
| NPC Autonomous Agents | 模拟层 NPCAgent（批量 tick） | 成本约束下不做"每 NPC 每回合一次 LLM" |
| 世界离线演化 | WorldSimulator.fastForward() | 回归时结算，而非后台常驻模拟 |
| 多模型协作 | 模型分级（强模型叙事 / 便宜模型抽取与校验） | 单一 provider 内的模型分级即可 |

### 3.3 一个回合的生命周期（核心数据流）

```
玩家消息
  │
  ▼
① 模式层路由：确定当前模式与子系统配置
  │
  ▼
② ContextBuilder 组装上下文包（按 token 预算）：
   - 硬事实注入：当前场景、在场 NPC、锁定状态
   - 记忆检索：与当前输入相关的长期/短期记忆
   - 规则指导：代码计算的局势标志（卡住/威胁/目标完成…）
   - Planner 指令：当前场景级计划（若启用）
  │
  ▼
③ Narrator 生成叙事（同步 LLM 调用，可调用规则工具：骰子/检定）
  │
  ▼
④ 输出后处理：
   - 结构化变更请求解析（工具调用结果）
   - WorldEngine.ApplyEvent() 校验并落库（单写入入口）
   - 状态锁定校验：叙事与锁定事实冲突 → 重生成或修正
  │
  ▼
⑤ 异步侧链（不阻塞回复）：
   - 事件写入记忆层（重要性评分，便宜模型或规则）
   - 技能使用记录（供幕间成长结算）
   - 世界时钟推进，到期事件入队
   - Reflector 阈值检查（需要时压缩记忆）
  │
  ▼
⑥ 回复玩家
```

关键约束：**同步路径上只有 ③ 一次必需的 LLM 调用**；④的校验若启用则为第二次（可用便宜模型）；⑤全部异步。这直接回应了现状"每轮双 LLM、延迟翻倍"的问题，同时比"纯单 Agent"多出的智能都发生在异步侧链和低频任务中。

---

## 四、世界状态层（WorldState）

这是整个引擎的核心资产，从现有 `GameState`（internal/agent/gamestate.go）演化而来——GameState 的"微观运行态"定位是对的，但需要扩展为完整的世界模型。

### 4.1 实体模型

```go
// WorldState 是一个世界实例（= 一个 QQ 群的一场游戏）的全部硬状态。
type WorldState struct {
    WorldID     string
    Mode        string            // trpg / simrpg / roleplay
    Clock       WorldClock        // 世界时钟
    Scene       SceneState        // 当前场景（现有 GameState.CurrentScene 演化）
    Characters  map[string]*CharacterState  // PC + NPC 统一实体
    Locations   map[string]*Location        // 地点（RPG 模式全量，TRPG 模式按需）
    Factions    map[string]*Faction         // 势力/组织（可选）
    Quests      []QuestState                // 任务/目标（现有 Objectives 演化）
    EventQueue  []ScheduledEvent            // 待触发事件（现有 PendingEvents 演化）
    Relations   []RelationEdge              // 关系图谱
    Locks       []StateLock                 // 状态锁定
    EventLog    []WorldEvent                // 事件溯源日志（追加式，可截断归档）
    Metrics     GameMetrics                 // 现有规则化指标（降级为粗粒度标志）
}
```

**CharacterState**（PC 与 NPC 统一，现有 NPCState 的大幅扩展）：

```go
type CharacterState struct {
    ID          string
    Name        string
    Kind        string            // pc / npc
    CardRef     string            // 关联 character.Card ID（规则数值的唯一真相）
    Alive       bool
    Location    string
    Disposition string            // 对玩家的态度（友好/中立/怀疑/敌对）
    Mood        MoodState         // 情绪状态（见 6.2）
    Traits      []string          // 性格特质标签（勇敢/贪婪/多疑…）
    Goals       []Goal            // 当前目标（NPC 驱动行为的来源）
    Secrets     string            // 隐藏信息（不会主动透露）
    MemoryRef   string            // 记忆层中该角色的记忆命名空间
    LockedFlags map[string]bool   // 角色级状态锁定（如 dead、mad、removed）
}
```

**关键设计决策**：

- **数值真相仍在 character.Card**。WorldState 不复制 HP/SAN/技能值，只存 `CardRef` 引用，避免双份数值失步（现状 GameState 与角色卡分离的教训）。
- **EventLog 事件溯源**：所有状态变更以 `WorldEvent{Type, Actor, Target, Delta, WorldTime, Round}` 追加记录。它是审计、调试、离线演化回放、以及"世界史摘要"的统一数据源。可定期归档截断（旧段压缩成摘要事件）。
- **StateLock 状态锁定**：`{Key: "npc:老陈:dead", Reason: "被玩家击杀", Round: 42}`。锁定事实注入每轮上下文，且 ApplyEvent 拒绝与之冲突的变更（如复活已死 NPC 需显式解锁事件）。

### 4.2 世界时钟（WorldClock）

```go
type WorldClock struct {
    WorldTime   int64   // 世界内时间（分钟计数，虚构纪元）
    RealLapsed  int64   // 上次活跃时的现实时间戳（离线演化依据）
    RoundCount  int
}
```

- 每个玩家回合推进世界时间（动作粒度：对话 ≈ 分钟，探索 ≈ 十分钟，旅行 ≈ 小时，由 Narrator 结构化输出建议值、代码钳制范围）。
- 时间驱动：定时事件到期、情绪衰减、记忆时效性（recency 计算用世界时间而非现实时间——暂停一周的团，记忆不会"过期"）。

### 4.3 关系图谱与声誉

```go
type RelationEdge struct {
    From, To  string  // 角色 ID
    Trust     int     // -100..100
    Fear      int
    Debt      int     // 人情/债务
    Tags      []string
}
```

- 声誉 = 按 Faction/Location 维度的聚合值，NPC 初次见面时查表得初始态度。
- 后果传播**规则化**：事件类型 → 传播模板（例：`killed(A)` → 对 A 的 `family/ally` 边 Trust-50、Fear+30；Faction 声誉-20）。不走 LLM，确定性、可测试、可回放。

### 4.4 存储层

```go
type StateRepository interface {
    Load(worldID string) (*WorldState, error)
    Save(state *WorldState) error          // 原子写
    AppendEvents(worldID string, evts []WorldEvent) error
    Archive(worldID string, beforeRound int) error  // 旧事件压缩归档
}
```

- 实现 1（现状迁移）：每世界一个 JSON 文件（合并现有 GameState + Progress 两份文件，消除双份真相）。
- 实现 2（预留）：SQLite——当 EventLog 增长、需要按实体/时间查询时切换。接口先行，上层无感知。
- **会话级互斥锁**保持现状（pipeline 修复已加），长团下建议同一世界任何时刻只有一个回合在处理。ApplyEvent 等价于 AI Town 的事务化写入：tick 内决策、tick 边界落账。

### 4.5 双时序事实模型（借鉴 Zep/Graphiti）

动态世界的事实会**过期**（"门开了又锁上"、"老陈从友好变敌对"）。为支持时序一致性，易变事实以带有效区间的形式记录：

```go
type Fact struct {
    Subject, Predicate, Object string  // ("酒馆大门", "状态", " locked")
    ValidAt, InvalidAt         int64   // 世界时间：何时为真/何时失效（0 = 仍有效）
    RecordedAt                 int64   // 系统时间：何时写入
}
```

- 新事实与旧事实矛盾时，给旧事实设 `InvalidAt` 而非覆盖——历史永不丢失，"上周门还开着"类问题可答。
- 首版只借数据模型（WorldState 加一张事实表 + 失效写路径），不引入图数据库；Graphiti 全量管线（实体抽取/社区聚类）属于离线增强，不进玩家等待的写路径（其论文自评构建延迟高）。

---

## 五、记忆层（Memory）

长团的核心问题不是"状态存不下"，而是"什么该进入 LLM 的上下文"。借鉴 Voyage 四层记忆与 Generative Agents（Park et al., 2023）的检索/反思机制。

### 5.1 四层记忆模型（每个 NPC 一份，世界本身一份）

| 层 | 内容 | 生命周期 | 实现 |
|----|------|---------|------|
| **核心身份** | 性格、背景、动机、说话风格、秘密 | 永久，只读 | CharacterState 内嵌字段（剧本/设定写入） |
| **长期记忆** | 关键事件、重要对话结论、关系变化、反思洞察 | 永久，可压缩 | MemoryEntry 列表 + 向量索引（OpenViking） |
| **短期记忆** | 当前会话/当前场景内的事件与对话摘要 | 场景切换时压缩进长期 | 内存 ring buffer + 滚动摘要 |
| **工作记忆** | 本轮即时上下文：当前感知、交互对象、即时意图 | 单回合 | 每回合临时组装，不持久化 |

### 5.2 记忆条目与检索

```go
type MemoryEntry struct {
    ID          string
    Content     string    // 自然语言事实："玩家在老陈的店里偷了钱袋"
    Importance  int       // 1-10，写入时评分（规则启发式 + 可选便宜模型）
    WorldTime   int64     // 发生时的世界时间（recency 依据）
    LastAccess  int64     // 上次被检索到的世界时间
    Tags        []string  // 实体 ID、事件类型，结构化过滤用
    Embedding   []float32 // 语义检索用（OpenViking Find 代劳时可不本地存）
    Pinned      bool      // 里程碑事件（背叛/结盟/死亡）永不压缩遗忘
}
```

**检索打分**（Generative Agents 三因子，各自 min-max 归一化到 [0,1] 后加权求和）：

```
score = w_recency    · 0.995^(Δh)          // Δh = 距"上次被检索"的游戏小时数
      + w_importance · (Importance / 10)
      + w_relevance  · cosine(query, entry) // OpenViking Find
```

- 三个权重基线为 1/1/1（GA 论文原值），长团场景建议调为 `0.3 / 0.4 / 0.3`（重要性高于时效）。
- recency 按**上次访问时间**而非创建时间计算——被反复用到的记忆会"保鲜"（GA 论文关键细节）；衰减用**世界时间**而非现实时间（暂停一周的团记忆不过期）。
- 每回合取 top-K（K 由 token 预算反推，默认 5~10 条）。

**Importance 评分规则化优先**：涉及死亡/背叛/任务关键道具 = 9-10；检定大成功/大失败 = 7-8；普通对话 = 2-4。规则打不准的再落到便宜模型。**这一步绝不用强模型**。

**写入路径的一致性维护**（借鉴 Mem0 的最小算子集）：新记忆写入前检索 top-s 相似旧记忆，判定四选一——**ADD**（新事实）/ **UPDATE**（修正旧事实）/ **DELETE**（作废错误事实）/ **NOOP**（重复，跳过）。被 UPDATE/DELETE 的旧记忆**标记失效而非物理删除**（保留"三天前那把剑还是完好的"这类时序问答能力，见 4.5 双时序模型）。首版可用规则+embedding 相似度实现，LLM 判定作为可选增强。

### 5.3 反思与压缩（Reflector，异步）

- **触发**：某 NPC 未反思事件的重要性累计超过阈值（GA 论文原值 150，实践中每天触发 2~3 次；本项目 NPC 数量少、事件密度低，默认 100 起步）；或场景/幕切换时；或短期记忆条数超上限。
- **动作**（对齐 GA 的 reflection 流程）：
  1. 取最近若干条记忆 → 生成"最值得追问的高层问题" → 以问题为查询二次检索；
  2. 生成 1-2 条**带证据引用的洞察**（`洞察 (because of 记忆ID 1, 5, 3)`），洞察作为 Importance 8+ 的长期记忆写回，并保留指向证据记忆的指针——洞察可建立在旧洞察之上，形成"反思树"，且异常时可回溯证据链；
  3. 短期记忆 → 压缩为场景摘要并入长期；
  4. 低重要性 + 久未访问的非 Pinned 条目 → 归档（不删，移出检索池）。
- **执行时机**：异步侧链或幕间结算，绝不在同步回合路径上。仅"在场/活跃 NPC"启用完整反思循环，背景 NPC 降频（GA 论文的成本教训：25 个 NPC 全量反思开销巨大）。

### 5.4 与 OpenViking 的对接

项目已集成的 OpenViking 客户端（internal/store/openviking.go）提供了现成能力：

| OpenViking API | 用途 |
|----------------|------|
| `Find(query, targetURI)` | 语义检索（relevance 因子），免去自建向量库 |
| `UpdateMemory / ReadMemory` | 长期记忆 KV（按 `npc:{id}` 命名空间隔离） |
| `CreateSession / AddSessionMessage / CommitSession / GetSessionContext` | 会话级滚动摘要（短期记忆压缩可直接复用） |
| `WriteJSON / ReadContext` | 结构化记忆条目持久化 |

OpenViking 未启用时降级为本地 JSON + 关键词匹配（现有降级模式延续），检索质量下降但功能完整。

### 5.5 玩家侧记忆（战役摘要）

独立于 NPC 记忆，为 Narrator 提供"至今为止的故事"：

- 每幕/每 N 回合由 Reflector 生成滚动战役摘要（现有 `save_progress` 工具的正式化、自动化）。
- 决策日志（现有 `Progress.PlayerDecisions`）并入 EventLog，按"关键决策"标签检索。
- 修复现状缺陷：`StoryContext` 拆分为不可变的 `Background`（剧本背景）与滚动的 `CampaignSummary`，杜绝摘要覆盖设定。

---

## 六、模拟层（Simulation）

让"世界是活的"，但成本必须可控。核心策略：**事件驱动 + 批量 tick + 回归结算**，不做每 NPC 每回合一次 LLM。

### 6.1 NPC 智能体循环

```
感知（Perception）→ 检索记忆 → 评估目标/情绪 → 决策 → 行动
```

两种触发方式：

1. **交互驱动（同步）**：玩家与 NPC 对话/交互时，该 NPC 的认知数据（特质 + 情绪 + top-K 记忆 + 目标）注入 Narrator 上下文，由 Narrator 一并生成其行为与台词——**不单独调用 LLM**。这是 95% 的场景。
2. **世界 tick（低频批量）**：场景切换、幕间、或世界时钟跨阈值时，**一次 LLM 调用批量决策多个在场/相关 NPC**（输入：各 NPC 的目标/情绪/记忆摘要；输出：结构化的 NPC 行动列表），引擎把行动翻译成 WorldEvent。N 个 NPC = 1 次调用，成本与 NPC 数量解耦。

NPC 行动受硬约束：可执行动作集由模式与场景决定（移动/交谈/交易/攻击/离开/使用物品），ApplyEvent 校验合法性（死人不能行动、不在场不能交谈）。

### 6.2 情绪与性格模型

```go
type MoodState struct {
    Valence   int      // -100..100（愉悦度）
    Arousal   int      // 0..100（激活度）
    Tags      []string // 当前情绪标签（愤怒/恐惧/感激…）
    UpdatedAt int64    // 世界时间
}
```

- **事件驱动修改**：规则化映射（被救助 → Valence+30、Tag+感激；被背叛 → Valence-60、Tag+愤怒/仇恨）。
- **随时间衰减**：每世界日向 0 收敛（如 `value *= 0.7/day`），用世界时钟计算，长期离线后情绪自然平复——但 Pinned 记忆仍在，所以"原谅但不遗忘"。
- **性格调制**：Traits 决定反应系数与行为倾向（"记仇"特质的 NPC 衰减减半、报复行动权重加倍；"胆小"特质在高 arousal 时倾向逃跑）。性格不变、情绪可变、态度（Disposition）是两者对玩家的合成输出。
- **对叙事的影响**：Mood + Traits + top-K 记忆 + Goals 组成 NPC 认知卡，注入 Narrator。Narrator 只负责"演得像"，状态变化由结构化输出回到引擎。

### 6.3 世界模拟与离线演化

- **ScheduledEvent**：`{ID, TriggerAt(世界时间) 或 Condition, Type, Payload, Recurring}`。来源：剧本 Triggers（TRPG）、NPC Goals（RPG）、世界规则（市集每 7 天、季节变化）。
- **在线**：每回合时钟推进后检查到期事件，入队并在适当时机由 Narrator 呈现。
- **离线（fastForward）**：玩家回归时，按 `(now - RealLapsed)` 结算：
  1. 到期的 ScheduledEvent 全部结算（规则化部分直接落库）；
  2. NPC Goals 按离线时长推进（规则化进度估算 + 必要时一次批量 LLM 调用生成"期间发生了什么"）；
  3. 情绪衰减、记忆时效更新；
  4. 生成**世界变迁摘要**呈现给玩家（"你离开的三周里，商队抵达了，老陈的店关了张…"）。
- **成本关键**：fastForward 是全量状态的摘要级结算，最多 1 次 LLM 调用，不是逐回合回放。

### 6.4 后果引擎（ConsequenceEngine）

- 输入：WorldEvent；输出：派生的关系/声誉/任务状态变更（规则模板）。
- 因果链记录：`Event.CausedBy` 字段串联因果，供"为什么 B 敌视我"类问题回溯，也是叙事一致性的依据（Narrator 上下文包含相关因果链）。

---

## 七、规则层（Mechanics）与成长闭环

### 7.1 现有资产

`trpg.Service` 已具备：骰子表达式、CoC7/DnD5e 检定、SAN 检定、对抗检定、`SkillGrowth`（技能成长：1d100 > 当前值 → +1d10）、先攻、房规。这一层保持不变，继续作为规则唯一真相。

### 7.2 成长闭环（ProgressionEngine）

把已有的 `SkillGrowth` 从"手动指令"升级为自动闭环：

```
回合内：检定成功 → 记录 SkillUse{Skill, Round, WorldTime}（异步侧链，零 LLM）
幕间结算（场景/幕切换、长休息、或 .rest 指令触发）：
  1. 汇总本幕成功使用过的技能（去重）
  2. 逐个执行 SkillGrowth（CoC7 规则）/ 经验值累计（DnD5e 里程碑）
  3. 更新角色卡（character.Card，数值真相）
  4. 生成成长报告呈现玩家（"侦查 45→52，斗殴 60→63"）
  5. 成长事件写入 EventLog 与相关 NPC/世界记忆（"玩家的枪法越发精准"）
```

- 接口抽象 `ProgressionEngine`，CoC7（技能成长制）与 DnD5e（经验/里程碑制）分别实现；RPG 模拟模式可挂简化版（使用即成长）。
- 幕间结算同时是 Reflector 记忆压缩、NPC tick、世界事件结算的天然触发点——**一个"幕间"钩子统一驱动所有周期性工作**。

---

## 八、叙事层（Narrative）

### 8.1 三个角色（替代现有 Director/Narrator 双层）

| 组件 | 频率 | 模型档位 | 职责 |
|------|------|---------|------|
| **Narrator** | 每回合（同步） | 强模型 | 唯一面向玩家的文本生成；可调用规则工具（骰子/检定）；产出结构化变更请求 |
| **Planner** | 场景/幕级（低频） | 强模型 | 场景规划：本场景的叙事节拍、NPC 目标编排、潜在冲突点；输出存 WorldState.ScenePlan 供各回合引用 |
| **Reflector** | 阈值触发（异步） | 便宜模型 | 记忆压缩、洞察生成、战役摘要（见 5.3） |

与现状的关键区别：**不再有每轮一次的 Director LLM**。现有 Director 的两类产出重新归位——规则可算的（基调/节奏/张力调节）由代码的"规则指导器"生成（现有 `fallbackDirective` 逻辑的转正）；真正需要智能的场景级规划由低频 Planner 承担。每轮成本从 2 次调用降回 1 次，长团规划能力反而增强（Planner 看得比单轮 Director 远）。

### 8.2 上下文包组装（ContextBuilder）

每回合按 token 预算（可配置，默认 4-6K）组装，优先级从高到低：

1. 模式系统提示词 + 输出格式约束（结构化变更请求的 schema）
2. **锁定事实**（StateLock）与当前场景硬状态——不可省略
3. 在场 NPC 认知卡（特质 + 情绪 + 目标 + top-K 记忆）
4. Planner 场景计划（若有）
5. 规则指导标志（玩家卡住/场景陈旧/目标完成——粗粒度枚举，替代现状伪精确的百分制指标喂 prompt）
6. 战役摘要 + 相关世界记忆（检索）
7. 近期对话窗口（短期记忆，超窗即弃——短期记忆压缩兜底）
8. 玩家输入

**声明式注入条目（lorebook 式，借鉴 SillyTavern World Info）**：剧本内容（场景描述、线索、NPC 背景、阵营信息、随机遭遇表）不塞进静态 prompt，而是结构化为带触发规则的注入条目，由 ContextBuilder 确定性装配——**零 LLM 调用、完全可测试**：

```go
type LoreEntry struct {
    Content   string   // 注入文本
    Keys      []string // 触发关键词（中文场景主用向量触发，关键词仅辅助）
    Vector    bool     // 是否 embedding 相似度触发
    Constant  bool     // 常驻条目（如世界观核心设定）
    Sticky    int      // 激活后持续 N 回合
    Cooldown  int      // 激活后冷却 N 回合
    Probability int    // 1-100，随机遭遇表用（"提到古神名 1% 惊醒它"）
    Position  int      // 注入位置（离输出越近影响力越强，锁定状态放最近）
    Budget    int      // 条目 token 预算
}
```

Sticky/Cooldown 本质是基于回合计数的触发器状态机，天然适合"中毒 N 回合""警戒状态持续"类效果；Probability + 分组互斥可做随机事件表。这套机制同时是 TRPG 剧本系统的增强：剧本解析产物直接编译为 LoreEntry 集合。

预算不足时从 7→6→5 的顺序裁剪。**这条流水线替代现状"全量 GameState JSON + 无限累积的会话历史"**，是长团上下文可控的根本机制。

### 8.3 结构化变更与输出校验

- Narrator 输出 = 叙事文本 + 尾部结构化块（JSON mode 或工具调用）：`{state_requests: [...], time_advance: 15, skill_uses: [...]}`。
- `state_requests` 经 ApplyEvent 校验落库；非法请求（复活锁定死亡 NPC、添加不存在的物品）拒绝并把**错误原因回喂给 Narrator 重述**（MemGPT 的校验回环实践：校验器是代码而非提示词，最多重试 1 次防死循环，失败则丢弃该变更仅保留叙事）。
- **轻量输出校验（可选开关）**：便宜模型或规则检查叙事文本与锁定事实的冲突（"已死 NPC 又开口说话"类），冲突时重写该段。TRPG 模式默认关（剧本约束已较强），RPG 开放模式默认开。

### 8.4 模型分级

| 任务 | 档位 | 说明 |
|------|------|------|
| 叙事生成、场景规划 | 强模型（deepseek-chat 级） | 质量敏感，每回合/每场景各一次 |
| 重要性评分、记忆压缩、摘要、输出校验、意图分类 | 便宜模型（deepseek-chat 低参数档/本地小模型） | 量大价低，全部异步 |
| 指标计算、检索打分、关系传播、成长结算、情绪衰减 | 不调模型 | 纯代码 |

---

## 九、模式层（Game Modes）

```go
type GameMode struct {
    Name           string
    Ruleset        string          // coc7 / dnd5e / lite / none
    Subsystems     SubsystemFlags  // 各层开关
    PromptTemplate string          // 系统提示词模板
    ToolSet        []string        // 允许的工具集
    TickPolicy     TickPolicy      // NPC tick / 世界事件 / 离线演化策略
}
```

| 子系统 | TRPG 跑团 | 文字 RPG 模拟 | AI 角色扮演 |
|--------|----------|--------------|------------|
| 规则层（检定/成长） | ✅ 完整 | ✅ 简化版 | ❌ |
| 剧本/时间轴 | ✅ | ❌（Planner 动态生成节拍） | ❌ |
| Planner | ✅ 场景级 | ✅ 世界节拍级 | ❌ |
| NPC 记忆/情绪/关系 | ✅ | ✅ | ✅（单 NPC 全开） |
| NPC tick | 场景切换时 | 时钟阈值驱动 | 仅情绪衰减 |
| 离线演化 | 弱（剧本内时间） | ✅ 全量 fastForward | ❌ |
| 输出校验 | 关 | 开 | 关 |
| 每回合 LLM 预算 | 1 次 | 1-2 次 | 1 次 |

**模式即配置的收益**：角色扮演模式 = 世界引擎内核（记忆+情绪+关系全开）+ 关掉规则与剧本，新增成本几乎为零；RPG 模式复用全部模拟层，只是没有剧本约束。现有 TRPG 模式是配置最全的一个实例，迁移即验证。

---

## 十、关键流程详述

### 10.1 TRPG 模式回合流程

```
玩家消息 → 会话锁 → Load WorldState
  → ContextBuilder（锁定事实+在场NPC卡+场景计划+规则标志+战役摘要+检索记忆+对话窗）
  → Narrator（叙事+工具检定+结构化变更请求）
  → ApplyEvent（校验落库：NPC态度/线索发现/事件触发/目标完成）
  → 冲突校验（可选）→ 回复
  → 异步：记忆写入+技能记录+时钟推进+到期事件入队+Reflector阈值检查
```

### 10.2 幕间结算流程（统一周期钩子）

```
触发（场景切换/长休息/.rest 指令）
  → ProgressionEngine：技能成长结算 → 角色卡更新 → 成长报告
  → Reflector：短期记忆压缩、洞察生成、战役摘要更新
  → NPC tick（批量）：场景相关 NPC 目标推进
  → 世界时钟结算：到期事件、情绪衰减
  → EventLog 归档检查
  → Planner：下一幕/场景计划生成
一次幕间 ≈ 2-3 次便宜/强模型调用，替代现状散落在各工具里的零散副作用。
```

### 10.3 RPG 模式离线回归流程

```
玩家回归 → 计算离线时长（现实时间 → 世界时间映射）
  → fastForward：到期事件结算 + NPC 目标推进 + 情绪衰减
  → 批量 LLM 生成世界变迁摘要（1 次）
  → 呈现摘要 + 当前场景状态 → 进入正常回合循环
```

### 10.4 角色扮演模式会话流程

```
玩家消息 → Load 目标 NPC 的完整认知档案（核心身份+情绪+关系+top-K 记忆）
  → Narrator（persona 约束模板：语气/口癖/边界）
  → 回复 → 异步：记忆写入+情绪更新+重要性评估
长会话（数百轮）由 Reflector 周期性压缩维持上下文预算。
```

---

## 十一、一致性保障体系（防幻觉）

| 机制 | 层面 | 说明 |
|------|------|------|
| **事实注入** | 输入 | 硬状态与锁定事实每轮强制注入上下文（ContextBuilder 优先级 2） |
| **单一写入者** | 写入 | 一切变更经 ApplyEvent 校验落库，LLM 无权直接改状态 |
| **状态锁定** | 存储 | 死亡/任务完成等关键事实不可逆，冲突变更被拒绝 |
| **结构化解耦** | 输出 | 叙事文本与状态变更分离产出，变更走 schema 校验 |
| **输出校验** | 后验 | 便宜模型/规则抽查叙事与锁定事实冲突（开放模式默认开） |
| **因果链** | 追溯 | Event.CausedBy 记录因果，异常可回溯定位 |
| **确定性传播** | 派生 | 关系/声誉/情绪的变化规则化，可测试可回放 |

---

## 十二、成本与性能预算

### 12.1 每回合 LLM 调用预算（同步路径）

| 模式 | 同步调用 | 异步调用（摊薄） | 说明 |
|------|---------|-----------------|------|
| TRPG 跑团 | 1（Narrator） | 0.2-0.5（记忆/评分按阈值触发） | 较现状 2 次降 50% |
| 文字 RPG | 1-2（Narrator + 可选校验） | 0.5-1（NPC tick 摊到场景切换） | NPC 批量 tick 是关键 |
| 角色扮演 | 1 | 0.3 | 记忆写频繁但异步 |

### 12.2 长团上下文预算

- 每回合上下文包 4-6K tokens（ContextBuilder 硬预算），与会话长度**无关**——这是支撑数千回合的核心不等式：**成本 = f(回合复杂度)，而非 f(历史长度)**。
- 记忆/事件存储随回合线性增长，但检索与摘要把进入上下文的量控制在常数级。

### 12.3 延迟

- 同步路径单次 LLM 调用（5-20s），消息到达即回"思考中"ack（被动回复不占配额）。
- 异步侧链（记忆/评分/压缩）goroutine 执行，失败仅记日志不影响玩家体验。

---

## 十三、与现有代码的映射及迁移路线

### 13.1 组件映射

| 现有组件 | 去向 |
|---------|------|
| `GameState` / `GameStateStore` | 扩展为 `WorldState` / `StateRepository`（合并 Progress 文件） |
| `Director` + `MetricsEvaluator` | 拆解：规则部分 → 规则指导器（代码）；规划部分 → 低频 Planner |
| `Narrator` | 保留为核心叙事组件，接入 ContextBuilder |
| `ProgressTracker` / `Progress` | 并入 WorldState（Quests + EventLog 关键决策标签） |
| `TimelineEngine`（空转定时器） | 重构为 ScheduledEvent + 世界时钟；提醒接 SendGroupMessage 真推送或删除 |
| `trpg.Service` / ruleset | 原样保留（规则层） |
| `script.Script` 结构 | TRPG 模式的世界初始化模板（Timeline→Quests, Characters→CharacterState, Scenes→Locations） |
| OpenViking 客户端 | 记忆层存储与检索后端 |
| `KPPipeline` | 演化为通用 `TurnEngine`（模式路由 + 回合生命周期） |
| `fallbackDirective` | 转正为规则指导器 |

### 13.2 分阶段实施

| 阶段 | 内容 | 交付标准 |
|------|------|---------|
| **P1 状态统一** | GameState+Progress 合并为 WorldState；ApplyEvent 单写入；StateRepository 接口；StoryContext 拆分 Background/CampaignSummary | 现有 TRPG 功能回归通过，状态文件合一 |
| **P2 上下文工程** | ContextBuilder + token 预算；Director 下线，规则指导器转正；Narrator 无状态会话；JSON mode | 长团上下文体积恒定（实测 50+ 回合不增长） |
| **P3 记忆层** | MemoryEntry + 三因子检索 + Reflector 压缩；OpenViking 对接与降级；NPC 认知卡注入 | 50 回合后 NPC 能准确回忆早期关键事件 |
| **P4 成长闭环 + 情绪关系** | ProgressionEngine 幕间结算；MoodState + 关系图谱 + 规则化后果传播 | 技能成长全自动；NPC 态度随事件合理变化 |
| **P5 世界模拟与多模式** | 世界时钟 + ScheduledEvent + fastForward；GameMode 配置抽象；RPG/角色扮演模式上线 | 三种模式共享引擎运行 |

每阶段独立可交付、可回滚，TRPG 模式全程可用。

---

## 十四、风险与缓解

| 风险 | 缓解 |
|------|------|
| **一致性边界**：极端长链因果下规则传播失真 | EventLog 全量溯源 + 定期 Reflector 校验摘要；提供 `.world check` 管理指令人工巡检 |
| **软层幻觉残留**：检索遗漏、LLM 对记忆"添油加醋"（GA 论文自评的头号失败模式，无法根除） | 重要事实强制走硬状态通道（ApplyEvent + StateLock），隔离爆炸半径；洞察带证据引用可回溯；叙事校验抽查 |
| **记忆写路径延迟**：图构建/反思类管线阻塞玩家 | 全量图构建与反思只进异步批处理（Zep 构建延迟高的教训）；写路径只保留 ADD/UPDATE/DELETE/NOOP 轻量判定 |
| **成本失控**：异步任务堆积（记忆评分/tick/压缩） | 每世界每回合异步调用硬上限；便宜模型兜底；任务可降级为纯规则 |
| **QQ 平台限制**：主动消息月配额、单条长度 | 推送仅用于重要世界事件；长叙事分段；ack 用被动回复 |
| **复杂度失控**：子系统过多导致调试困难 | 模式开关可逐层关闭降级到"单 Agent + 状态工具"；EventLog 提供完整回放能力 |
| **存储膨胀**：EventLog/记忆无限增长 | 归档压缩机制（旧事件→摘要事件，旧记忆→洞察+归档） |
| **框架摩擦**：trpc-agent-go session 模型持续不适配 | Agent 调用全部无状态化（sessionID 每次唯一）；隔离在 Narrator/Planner/Reflector 三个薄封装内，必要时整体换薄客户端 |

---

## 十五、设计决策摘要

1. **保留并强化**：GameState → WorldState 的结构化状态思路（这是架构最正确的决策）、trpg.Service 规则层、OpenViking 记忆后端、剧本三阶段解析。
2. **拆解重组**：每轮 Director LLM → 规则指导器（代码）+ 低频 Planner（智能）；双线状态写入 → ApplyEvent 单写入。
3. **新增核心**：四层记忆 + 三因子检索 + Reflector、世界时钟 + ScheduledEvent + fastForward、情绪/关系/后果引擎、ProgressionEngine 成长闭环。
4. **架构不变量**：硬状态/软叙事分离；单一写入者；上下文按预算组装（成本与历史长度解耦）；模式即配置；能写代码的不用 LLM。

---

## 附录 A：方案选型速查

| 方案 | 一句话定位 | 写路径开销 | 时序能力 | 本设计采用的部分 |
|------|-----------|-----------|---------|----------------|
| Generative Agents（arXiv:2304.03442） | NPC 认知循环蓝本 | 高（评分+反思） | 弱（时间衰减） | 三因子检索公式、阈值反思、带证据引用的洞察、反思树 |
| SillyTavern World Info | 声明式上下文装配 | 零（确定性） | Sticky/Cooldown 状态机 | LoreEntry 注入机制、剧本内容编译为条目 |
| MemGPT/Letta（arXiv:2310.08560） | 自我分页的单 Agent 记忆 | 高（函数链） | 弱 | 记忆压力思路（转化为 ContextBuilder 硬预算）、校验回环 |
| Mem0（arXiv:2504.19413） | 轻量记忆 CRUD | 中 | 中（DELETE 语义） | ADD/UPDATE/DELETE/NOOP 写入算子集（先文本版，图非银弹） |
| Zep/Graphiti（arXiv:2501.13956） | 双时序知识图 | 高（异步批处理） | **强（valid/invalid 区间）** | 双时序事实模型（只借数据模型，不进写路径） |
| AI Town（a16z 开源） | 事务化模拟引擎 | 低 | 引擎侧 | tick 边界事务化落账 = ApplyEvent 单写入 |
| Voyage World Engine | 商业级双轨世界引擎 | — | 强 | 总体设计哲学与子系统划分 |

## 附录 B：参考资料

- `Voyage_World_Engine_Technical_Report.md`（本项目目录）：双轨架构、确定性状态管理、NPC 自主智能体、四层记忆、离线演化
- Park et al., "Generative Agents: Interactive Simulacra of Human Behavior" (UIST 2023, arXiv:2304.03442)：记忆流、三因子检索（recency 0.995^Δh 按上次访问时间）、反思阈值 150、证据引用洞察、规划层级
- Packer et al., "MemGPT: Towards LLMs as Operating Systems" (arXiv:2310.08560)：分层记忆、working context、记忆压力机制、函数调用自编辑
- "Mem0: Building Production-Ready AI Agents with Scalable Long-Term Memory" (arXiv:2504.19413)：两阶段抽取-更新管线、ADD/UPDATE/DELETE/NOOP 算子、图变体对比结论（图不是银弹）
- "Zep: A Temporal Knowledge Graph Architecture for Agent Memory" (arXiv:2501.13956)：双时序模型（valid/invalid 区间）、三层知识图、混合检索
- a16z-infra/ai-town（GitHub, MIT）：Generative Agents 工程化复刻，事务化状态写入、tick 驱动模拟
- SillyTavern 官方文档 World Info 章节：lorebook 触发模型、Sticky/Cooldown/Delay、注入位置与预算控制
- `AI多层架构分析.md`（本项目目录）：现状 17 项问题与 P0 修复记录
- 本项目现有实现：`internal/agent/`（GameState/Director/Narrator）、`internal/trpg/`（Service/ruleset/timeline）、`internal/store/openviking.go`（记忆与检索 API）

---

*文档完*

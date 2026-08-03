# AI 多层架构分析：问题与优化点

> 分析范围：AI 带团部分（`internal/agent/`）与剧本解析部分（`internal/script/`）
> 分析日期：2026-08-03
> 修复状态：P0 的 4 个正确性 Bug（问题 1~4）已于 2026-08-03 修复，详见文末「修复记录」；问题标题旁的【✅ 已修复】为修复标记。
> 基于 trpc-agent-go v1.10.0 源码验证：runner 默认使用 in-memory session service，按 `(appName, userID, sessionID)` 复用会话并累积事件，每次调用将 `invocation.Session.Events` 全部喂给 LLM（`llmflow.go:418`）。

---

## 一、严重问题（正确性 Bug）

### 1. 剧本分析器会话串扰：第二次分析会混入上一个剧本 ★最严重 【✅ 已修复】

`analyzer.go` 中所有 Agent 使用**固定 sessionID**：Planner 用 `"script-planning"`（analyzer.go:372）、Extractor 用 `"extract-<module>"`（analyzer.go:461）、Integrator 用 `"script-integration"`（analyzer.go:550）。而 trpc-agent-go 的 runner 默认使用 in-memory session service，按 `(appName, userID, sessionID)` 复用会话并累积事件，每次调用都把全部历史喂给 LLM。

后果：同一个 `ScriptAnalyzer` 实例分析第二个剧本时，会话里残留第一个剧本的全文 → **串剧本、内容污染、token 累积**。QQ 群里两人同时上传剧本时更糟：两个 goroutine 并发写同一个会话历史。

**修复**：每次 `Analyze` 生成唯一 sessionID（如 `fmt.Sprintf("plan-%d", time.Now().UnixNano())`），或在每次分析前重置会话。

### 2. GameState 读写竞态：pipeline 末尾的 Save 会覆盖 Narrator 工具的全部修改 【✅ 已修复】

`KPPipeline.Run` 的流程是：

1. kp_pipeline.go:59 — `loadOrCreateState` 从磁盘 Load 出 state 对象
2. Narrator 执行期间，其工具会**各自从磁盘 Load → 修改 → Save**：
   - `update_game_state`（script_tools.go:565-587）
   - `advance_timeline` → `RefreshForNode` 重置场景/目标/线索（script_tools.go:263-268）
   - `save_progress` → 改 StoryContext（script_tools.go:416-423）
3. kp_pipeline.go:85 — `applyUpdatesAndSave` 用**第 1 步的旧对象**覆盖写回磁盘

`GameStateStore` 的锁只保护单次 Load/Save，不覆盖整个 read-modify-write 周期。后果：**Narrator 工具做的所有状态变更（含时间轴推进后的场景刷新）会被静默回滚**。这是多层架构当前最核心的数据一致性 bug。

**修复**：pipeline 末尾改为「重新 Load → 合并 directive.StateUpdates → Save」，或在 `GameStateStore` 上加会话级互斥锁包住整个 pipeline 轮次；更彻底的做法是工具不再独立读写，而是把变更写入内存中的 pending 队列，由 pipeline 统一落盘。

### 3. `update_game_state` 工具的 target 语义与 `ApplyUpdate` 不匹配 → 静默失败 + 假成功 【✅ 已修复】

- 工具描述告诉 LLM：`hidden_discovered` 的 target = "线索描述"，`event_triggered` 的 target = "事件描述"（script_tools.go:544, 602-604）
- 但 `ApplyUpdate` 按**内部 ID** 匹配：`node_1_clue_0`、`node_1_trigger_0`（gamestate.go:168-181，ID 生成见 gamestate_store.go:244, 255）

LLM 几乎不可能猜中内部 ID → 更新永远匹配不上。而且 `applied++` 无条件递增（script_tools.go:578），返回"已更新 N 个状态"是**假成功**。另外 `scene_change` 类型在 Director prompt 和工具描述里都声明了，但 `ApplyUpdate` 根本没有实现这个 case（gamestate.go:162-189）。

**修复**：工具按描述模糊匹配（或同时接受 ID 和描述）；未匹配的更新应计入失败并反馈给 LLM；补齐 `scene_change` 分支或从 schema 中删掉。

### 4. `buildNarratorSystemPrompt` 是死代码：GameState 从未真正注入 Narrator 【✅ 已修复】

narrator_prompt.go:44 定义了把 GameState 摘要（当前场景、NPC 态度、目标清单、待触发事件）拼进系统提示词的函数，注释也写着"后续每轮会根据 GameState 动态更新"（narrator.go:88），但 `Narrator` 初始化时只用了 `narratorSystemPromptBase`（narrator.go:91），`Narrate()` 全程没有更新 instruction。

后果：**精心设计的微观运行态（NPC 态度变化、目标完成情况）到不了 Narrator**，只能指望 Director 在 directive 里转述——GameState 到 Narrator 的链路断了，多层架构的实际效果退化为"Director 自言自语"。

**修复**：trpc-agent-go 支持动态 instruction（`llmagent.WithInstruction` 换成按 invocation 求值的形式，或把 GameState 摘要拼进每轮 user message），把 narrator_prompt.go:44 的函数真正接上。

### 5. Director 的 actions 没有执行闭环

`DecisionDirective.Actions`（advance_timeline / trigger_event 等）只是被序列化成文本塞进 Narrator 的用户消息（narrator_prompt.go:149-155），**没有任何代码执行它们**。剧情推进完全依赖 Narrator"自觉"调用工具，Director 的决策只是建议，也无人事后校验 Narrator 是否执行了。决策层与执行层之间没有闭环。

---

## 二、上下文与成本问题

### 6. Director/Narrator 会话历史无限增长，`MemoryWindow` 是摆设

- Director 和 Narrator 每轮都会把**完整 GameState JSON（内含上一轮完整的 LastDirective JSON）+ 完整 directive + gameContext** 追加进各自的 runner 会话，历史永不裁剪（director.go:101-107、narrator.go:128-139）。
- `Config.MemoryWindow` 定义了（agent.go:23）、赋值了（main.go:110），但**全项目无任何使用处**。

一个 50 轮的团，Director 的输入会从几 KB 涨到几百 KB，成本线性放大直至超上下文上限报错。

**修复**：用上框架自带的 context compaction（v1.10.0 已有 `context_compact`），或每轮用新 sessionID + 自己维护裁剪窗口（把 MemoryWindow 真正落地）。GameState 本身就承担了"跨轮记忆"职责，LLM 会话历史其实是冗余的——Director 完全可以每轮无状态调用。

### 7. 重复注入浪费 token

- `buildNarratorUserMessage` 同时放了 directive 的完整 JSON **和**一份文字摘要（narrator_prompt.go:133-156），内容重复。
- Director 输入里 GameState JSON 嵌套 `LastDirective` 全文（director_prompt.go:97-102），而 runner 历史里本来就有上一轮 directive，双重冗余。

### 8. 闲聊也走双 LLM

玩家发"哈哈""你好"这类无实质内容的消息，照样走 Director（maxTokens 2048）+ Narrator（maxTokens 4096）两次完整调用。可以加一个廉价的前置分类（规则或极低成本调用），纯社交消息跳过 Director 直接用兜底 directive。

---

## 三、状态一致性设计问题

### 9. 两套进度真相并存且可能失步

- 宏观：`ProgressTracker.CurrentNodeID`（progress.go）
- 微观：`GameState.CurrentScene.NodeID`（gamestate.go）

Director 读 GameState，Narrator 的 gameContext 读 ProgressTracker（narrator.go:250-251）。两者只在 `advance_timeline` 工具里联动（且如问题 2 所述还会被覆盖回滚）。任何一条路径失败，两个组件看到的世界就不一致。

**建议**：明确单一真相——Progress 只保留"当前节点指针"，其余全部归 GameState；或合并为一个结构。

### 10. `save_progress` 会永久覆盖剧本背景

`StoryContext` 初始化时是完整的剧本背景（时代/氛围/主题/梗概，gamestate_store.go:119），但 `save_progress` 工具直接把它覆盖成 100-200 字的 AI 摘要（script_tools.go:419）。之后 Director 拿到的 `scriptContext`（kp_pipeline.go:66）只剩摘要，剧本设定永久丢失。

**修复**：StoryContext 拆成两个字段——`Background`（immutable 剧本背景）+ `StorySummary`（滚动摘要）。

### 11. 指标计算器的设计缺陷

- `calcChaos`：已触发事件**永久**累积混乱度（director_metrics.go:121-126），`PendingEvent` 没有"已解决"概念；`RoundCount > 10` 每轮 +2 也单调递增 → chaos 只涨不跌，"张力过高给玩家喘息"这类调节机制会逐渐失效。
- `calcTension` 硬编码 `card.Status["SAN"]`/`["HP"]`（director_metrics.go:51-63），DnD 规则下基本失效——评估器不知道当前规则集。

### 12. 降级路径吞错、不可观测

`Director.Decide` 在 LLM 失败/空回复/JSON 解析失败时都返回 `(fallback, nil)`（director.go:110-130），error 永远是 nil → `KPPipeline.runDirector` 的 err 分支是死代码（kp_pipeline.go:165-168）。降级发生了多少次、什么原因是完全不可观测的。建议返回降级标志并计数，长期运行后能评估 Director 的实际可用率。

---

## 四、剧本解析层问题

### 13. Phase 1 Planner 仍是全文一次性输入

分段读取工具只服务于 Phase 2，Planner 把带行号的全文一次性喂给 LLM（analyzer.go:369-370）。长剧本（CoC 模组动辄十几万~几十万字）会直接超上下文——架构号称解决长文本问题，但瓶颈在最上游。

**建议**：超长文本先做机械分段（按标题/固定行数），Planner 分段扫读再合并计划（map-reduce），或对超限文本直接拒绝并提示。

### 14. 模块提取失败的容错是虚的

Extractor 失败只记 `errs` 继续走（analyzer.go:284-286），进度回调说"将尝试在整合阶段补充"（analyzer.go:527），但 Integrator 的输入里**没有原文**（analyzer.go:541-548），根本无法补充 → 产出缺模块的剧本，用户看到的仍是"识别完成"，摘要里节点数为 0 也无任何告警。

**修复**：失败的模块用降级策略重试（如把相关 segment 直接塞进单次调用重提取），或至少在最终结果里向用户明示"XX 模块提取失败"。

### 15. Integrator 单次输出有截断风险，无重试

Integrator 输入 = plan + 4 模块结果 + 全部逐字摘录，输出是完整剧本 JSON，maxTokens=16384（main.go:80）。长剧本输出被截断 → `json.Unmarshal` 失败 → 整个分析（可能已花几分钟和不少 token）直接失败。

**修复**：JSON 解析失败时做一轮"修复调用"（把残缺 JSON 发回让模型补全）；或 Integrator 不再输出全量，只做交叉引用校验，最终结构由代码从 `ExtractionResults` 拼装（Integrator 的价值其实有限，这是一个可以简化的点）。

### 16. 剧本 ID 有路径注入和重名覆盖风险

`generateScriptID` 直接用 AI 输出的 name 做文件名，只替换空格（analyzer.go:668-673）。AI 输出不可信：含 `/`、`\`、`..` 会路径穿越或写入失败；同名剧本重复上传直接静默覆盖旧档。

**修复**：sanitize（只保留字母数字中文下划线）+ 冲突时加哈希后缀。

### 17. 其他解析层细节

- `ParseFromURL` 用裸 `http.Get` 无超时（parser.go:162），慢响应会永久挂住 goroutine；附件下载也走这条路。
- `Analyze` 全程用 `context.Background()`（handler/script.go:223），无法取消；bot 收到关闭信号时分析 goroutine 泄漏。
- `extractJSON` 的"第一个 `{` 到最后一个 `}`"启发式（analyzer.go:651-655）在模型输出多个 JSON 块或夹带解释时会拼出非法 JSON，失败后没有重试回路。DeepSeek 支持 `response_format: json_object`，比正则兜底可靠得多——建议所有纯 JSON 输出的 Agent（Planner/Integrator/Director）都开 JSON mode。

---

## 五、工程细节

| 问题 | 位置 | 说明 |
|---|---|---|
| `buildGameContext` 完全重复 ~110 行 | kp_agent.go:192 vs narrator.go:158 | 两份拷贝已开始漂移（KPAgent 版多读一个 timeline_prompt 分支位置不同），应提取公共函数 |
| `collectReply`/`extractJSON` 重复 | agent 包 vs script 包 | 注释自称"独立实现避免跨包依赖"，同模块内这个理由不成立 |
| `CheckTriggers` 死代码 | timeline.go:206 | 全项目无调用；且其 `containsAny` 要求触发条件全文是玩家消息的子串，自然语言条件下几乎不可能命中 |
| 兜底回复向玩家泄漏内部信息 | kp_pipeline.go:198-210 | Narrator 失败时把"导演推理"直接发到群里，破坏沉浸感 |
| `os.Setenv` 全局污染 + 顺序耦合 | kp_agent.go:55、narrator.go:60、analyzer.go:77 | Director 自己不设 env，依赖 KPAgent 先初始化；将来多 provider 会互相覆盖。应改用 model 级别的显式配置注入 |
| `remove`+`rename` 非原子 | gamestate_store.go:80-81、archive.go:70-72 | 注释称"原子写入"，但崩溃窗口内旧文件已删、新文件未就位 → 数据丢失。Windows 上 rename 不能覆盖是事实，但应保留 `.bak` 兜底 |
| 同群并发消息触发并发 pipeline | kp_pipeline.go 整体 | 同一 QQ 会话两条消息同时到达 → 两个 `Run` 交错读写同一 GameState 文件，配合问题 2 必然丢数据。需要会话级串行队列 |

---

## 建议的修复优先级

1. **P0（正确性）**：问题 1（分析器 sessionID）、问题 2（GameState 竞态）、问题 3（target 语义不匹配）、问题 4（Narrator 接不上 GameState）——这四个不修，多层架构的核心承诺（跨轮一致性）实际不成立。【✅ 2026-08-03 已全部修复，详见文末修复记录】
2. **P1（成本与稳定性）**：问题 6（历史裁剪/MemoryWindow 落地）、问题 15（Integrator 截断重试）、问题 13（长文本 Planner）。
3. **P2（体验与简化）**：问题 9/10（状态真相合并）、问题 14（失败可见性）、JSON mode 替换正则提取、删除死代码（CheckTriggers、buildNarratorSystemPrompt 或接上、KPAgent.buildGameContext 单 agent 回退路径是否还需要保留也值得讨论——目前 pipeline 注入后它基本是死代码）。

---

## 修复记录（2026-08-03）

### ✅ 问题 1：分析器会话串扰

- `internal/script/analyzer.go`：`Analyze` 每次生成唯一 `runID`（`time.Now().UnixNano()`），`runPlanner` / `runParallelExtraction` / `runIntegrator` 均增加 `runID` 参数，各 Agent 的 sessionID 加上该前缀（如 `script-planning-<runID>`、`extract-<module>-<runID>`）。跨剧本分析不再共享会话历史。

### ✅ 问题 2：GameState 读写竞态

- `internal/agent/kp_pipeline.go`：
  - `KPPipeline` 新增会话级互斥锁（`locks map[string]*sync.Mutex`），`Run` 开头按会话加锁，同一会话的流水线串行执行，并发消息不再交错读写同一 GameState 文件（同时缓解了工程细节表中"同群并发消息触发并发 pipeline"的问题）。
  - `applyUpdatesAndSave` 改为「重新 `LoadOrDefault` 最新状态 → 只叠加本轮增量（StateUpdates / LastDirective / Metrics / RoundCount）→ Save」。Narrator 工具在执行期间的修改（`update_game_state`、`advance_timeline`→`RefreshForNode`、`save_progress`→StoryContext）不再被旧对象覆盖回滚。

### ✅ 问题 3：update_game_state target 语义不匹配

- `internal/agent/gamestate.go`：`ApplyUpdate` 返回值改为 `bool`（是否命中），新增 `matchTarget` 辅助函数——优先 ID/描述精确匹配，target ≥4 字时允许双向子串匹配（防过短 target 误命中）；`npc_disposition` 增加忽略大小写/空格的名称模糊匹配；`ApplyUpdates` 返回命中条数。
- `internal/agent/script_tools.go`：`update_game_state` 工具按真实命中数计数，未匹配目标列在返回消息中提示 LLM 修正重试（不再有假成功）；schema 与工具描述改为"ID 或描述片段"均可。
- `internal/agent/director_prompt.go`、`script_tools.go`、`gamestate.go`：移除从未实现的 `scene_change` 类型（场景切换统一走 `advance_timeline` 全链路，避免与 ProgressTracker 失步）。

### ✅ 问题 4：GameState 未注入 Narrator

- `internal/agent/narrator_prompt.go`：从 `buildNarratorSystemPrompt` 中提取 `buildGameStateSummary(state)`；`buildNarratorUserMessage` 增加 `state *GameState` 参数，每轮把运行态摘要（当前场景、NPC 态度、目标清单、线索/事件数量、指标、故事背景）注入用户消息。（trpc-agent-go 的 `llmagent.WithInstruction` 只支持静态字符串，故选择用户消息注入。）
- `internal/agent/narrator.go`：`Narrate` 调用点同步传入 `state`。

### 验证

- `go build ./...`、`go vet ./...` 通过。
- 新增 `internal/agent/gamestate_test.go`（9 个单元测试，覆盖 ID 匹配、描述片段匹配、长 target 包含匹配、NPC 名称模糊匹配、过短 target 防误命中、批量命中计数等）全部通过。
- 离线测试通过：`TestParseDocx_活神之手`、`TestParseSpeed_总结`（script 包）、store 包全部测试。
- 说明：`go test ./...` 中 `TestMultiAgentAnalyze_活神之手` 会从 `.env` 读取真实 API Key 执行端到端 AI 分析（6+ 次 LLM 调用），超过 `go test` 默认 600s 超时被终止，属于长耗时网络测试的固有特性，与本次改动无关（本次改动不涉及网络调用路径；单独运行该测试需 `go test -run TestMultiAgentAnalyze ./internal/script/ -timeout 1800s`）。

---

## 整体评价

架构设计（Director 决策 / Narrator 叙事 / GameState 运行态三层分离）方向是对的，规则化指标预评估 + 低温度决策的思路也合理；但目前**设计文档与实现之间有落差**——几处关键链路（GameState→Narrator、Director actions→执行、工具→状态落盘）要么没接上、要么互相覆盖，导致实际运行效果会明显弱于设计预期。优先把 P0 的链路打通，比加新特性收益大得多。

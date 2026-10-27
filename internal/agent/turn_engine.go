// Package agent - TurnEngine 回合引擎。
//
// 替代旧 KPPipeline（Director -> Narrator 每轮双 LLM）。
// 设计文档 3.3：同步路径上只有 Narrator 一次必需的 LLM 调用；
// Director 降级为低频 Planner（场景切换或每 planInterval 轮触发一次）。
//
// 流程：
//  1. 会话锁 + 加载 WorldState
//  2. 规则化指标评估（确定性，零 LLM）
//  3. 叙事指导：AIGuidance（同步 LLM，可回写 state_updates）优先，
//     超时/失败/禁用时降级为 RuleGuidance 规则版（确定性，零 LLM）
//  4. 低频 Planner：需要时调用 Director LLM 生成场景计划（存 WorldState.ScenePlan）
//  5. ContextBuilder 按预算组装上下文包
//  6. Narrator 无状态调用生成叙事
//  7. 记账（指标/计划/轮次）+ 持久化
package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/config"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/memory"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// DefaultPlanInterval 默认场景计划刷新间隔（轮次）。
const DefaultPlanInterval = 8

// plannerSummaryEntries Planner 输入携带的最近阶段摘要条数上限。
const plannerSummaryEntries = 3

// ExtraKeyRegenRetry MessageContext.Extra 标记：本次为"AI 回复重试"的重跑，
// TurnEngine 跳过回合快照写入（不覆盖原快照，多次重试回滚到同一基点）。
const ExtraKeyRegenRetry = "regen_retry"

// IsRegenRetry 判断本次消息是否为"回复重试"的重跑（Extra 带标记）。
func IsRegenRetry(ctx *core.MessageContext) bool {
	if ctx == nil || ctx.Extra == nil {
		return false
	}
	v, _ := ctx.Extra[ExtraKeyRegenRetry].(bool)
	return v
}

// recentInjectionsCap 每世界保留的最近 lore 注入记录数（环形缓存，供管理端可观测性用）。
const recentInjectionsCap = 20

// TurnEngine 是回合引擎（KP 主循环）。
type TurnEngine struct {
	narrator     *Narrator
	director     *Director   // 低频 Planner 用（可为 nil 禁用规划）
	guider       *AIGuidance // 每轮 AI 叙事指导（可为 nil 退回纯规则指导）
	metrics      *MetricsEvaluator
	worldEngine  *world.Engine
	ctxBuilder   *ContextBuilder
	summary      *SummaryService // 滚动剧情摘要服务（可为 nil 禁用）
	memory       *memory.Store   // 长期记忆存储（可为 nil：仅用 RecentTurns + 摘要）
	cfgStore     *config.Store   // 运行时配置（热更新项每回合读取，可为 nil）
	personaStore *persona.Store  // 全局默认人设存储（可为 nil，nil 时人设功能关闭）
	planInterval int

	// injections 世界 ID -> 最近 N 回合 lore 注入清单（内存环形缓存）。
	injMu      sync.Mutex
	injections map[string][]world.LoreResult

	// dirLogs 导演系统运行记录（按会话分桶的环形缓存，管理端可视化用）。
	dirLogMu sync.Mutex
	dirLogs  map[string][]DirectorLogEntry
}

// NewTurnEngine 创建回合引擎。
func NewTurnEngine(
	narrator *Narrator,
	director *Director,
	metrics *MetricsEvaluator,
	worldEngine *world.Engine,
	ctxBuilder *ContextBuilder,
	planInterval int,
) *TurnEngine {
	if planInterval <= 0 {
		planInterval = DefaultPlanInterval
	}
	if ctxBuilder == nil {
		ctxBuilder = NewContextBuilder(0)
	}
	return &TurnEngine{
		narrator:     narrator,
		director:     director,
		metrics:      metrics,
		worldEngine:  worldEngine,
		ctxBuilder:   ctxBuilder,
		planInterval: planInterval,
		injections:   make(map[string][]world.LoreResult),
		dirLogs:      make(map[string][]DirectorLogEntry),
	}
}

// SetSummary 注入滚动剧情摘要服务（由 main.go 在装配阶段调用）。
func (t *TurnEngine) SetSummary(s *SummaryService) {
	t.summary = s
	log.Printf("[TurnEngine] 剧情摘要服务已注入")
}

// SetMemory 注入长期记忆存储（由 main.go 在装配阶段调用；nil 时只用短期窗口）。
func (t *TurnEngine) SetMemory(s *memory.Store) {
	t.memory = s
	if s != nil {
		log.Printf("[TurnEngine] 长期记忆存储已接入")
	}
}

// Memory 返回长期记忆存储（Web 摘要编辑 API 用，可为 nil）。
func (t *TurnEngine) Memory() *memory.Store { return t.memory }

// SetGuider 注入 AI 叙事指导器（由 main.go 在装配阶段调用；nil 时退回纯规则指导）。
func (t *TurnEngine) SetGuider(g *AIGuidance) {
	t.guider = g
	log.Printf("[TurnEngine] AI 叙事指导已注入")
}

// SetConfigStore 注入运行时配置存储（热更新项每回合读取）。
func (t *TurnEngine) SetConfigStore(s *config.Store) {
	t.cfgStore = s
	log.Printf("[TurnEngine] 运行时配置已接入")
}

// SetPersonaStore 注入全局人设存储（由 main.go 在装配阶段调用）。
func (t *TurnEngine) SetPersonaStore(s *persona.Store) {
	t.personaStore = s
	log.Printf("[TurnEngine] 玩家人设已接入")
}

// contextBudget 返回当前生效的上下文预算（配置热更新优先，缺省用 Builder 默认值）。
func (t *TurnEngine) contextBudget() int {
	budget := t.ctxBuilder.Budget
	if t.cfgStore != nil {
		budget = t.cfgStore.GetInt(config.KeyContextBudget, budget)
	}
	return budget
}

// logContextBudget 记录本回合上下文装配结果；实占超预算时显式告警
// （必需分区不可裁，超预算不是静默行为，线上必须可观测）。
func (t *TurnEngine) logContextBudget(sessionID string, input *NarratorInput) {
	log.Printf("[TurnEngine] 上下文包: 设定 %d 字符 + 历史 %d 轮（预算 %d，实占 %d，裁掉历史 %d 轮）",
		len(input.ContextBlock), len(input.History), input.Budget, input.Used, input.TrimmedHistory)
	if input.Budget > 0 && input.Used > input.Budget {
		log.Printf("[TurnEngine] 警告: 上下文超出预算 %d 字符（必需分区不可裁）: session=%s",
			input.Used-input.Budget, sessionID)
	}
}

// Run 执行一回合。返回叙事文本。
func (t *TurnEngine) Run(
	ctx *core.MessageContext,
	session *core.Session,
) (string, error) {
	start := time.Now()
	sessionID := ctx.SessionID
	userID := ctx.UserID
	playerMessage := ctx.Content

	// 串行化同一世界的回合处理
	if t.worldEngine != nil {
		t.worldEngine.Lock(sessionID)
		defer t.worldEngine.Unlock(sessionID)
	}

	// 1. 加载 WorldState
	// normal 模式 = 纯对话：不加载/注入世界上下文；freechat = 世界内扮演。
	// 会话模式只存在内存（SessionManager），进程重启后回落 normal；
	// 而"离开世界"会删除世界状态，因此世界状态存在即视为世界内扮演——
	// normal 但世界状态仍在（重启前已进入世界）时恢复 freechat，避免
	// 玩家明明在世界中却走纯对话路径（世界观/人设/历史全部不注入）。
	state := t.resolveState(session, sessionID)
	if state == nil {
		// 无世界状态（普通对话）-> 直接叙事（人设只有全局默认一档）
		gameContext := t.narrator.buildGameContext(sessionID)
		p := t.globalPersona(userID)
		history := normalSessionHistory(session)
		input := t.ctxBuilder.BuildNarratorInputWithBudget(t.contextBudget(), nil, nil, nil, gameContext, "", history, playerMessage,
			LengthHintFromSession(session), buildPersonaBlock(p), personaName(p))
		t.logContextBudget(sessionID, input)
		reply, err := t.narrator.NarrateMessage(ctx.Ctx, ResolveMainPrompt(nil, personaName(p)), input, sessionID, userID)
		if err == nil {
			appendNormalSessionHistory(session, playerMessage, reply)
		}
		return reply, err
	}
	roundStart := state.RoundCount

	// 世界行为统一：不再按 simrpg/roleplay 开关子系统，所有世界都走
	// 迁移 + 记忆 + 规划 + 时钟 + 情绪这一套流程。

	// 旧版 Summaries/CampaignSummary 直接迁移进长期记忆（幂等；失败下回合重试）。
	if t.memory != nil {
		t.migrateSummaries(state)
	}

	// 回复重试：世界状态已回滚到快照，上一版叙事产生的事件记忆也要清掉，
	// 否则同一回合会同时存在两版事实。
	if IsRegenRetry(ctx) && t.memory != nil {
		if n, err := t.memory.DeleteRound(sessionID, memory.KindEvent, roundStart); err != nil {
			log.Printf("[Memory] 清理重试回合事件记忆失败: world=%s: %v", sessionID, err)
		} else if n > 0 {
			log.Printf("[Memory] 已清理重试回合事件记忆: world=%s, %d 条", sessionID, n)
		}
	}

	// 回合快照：在任何 mutation 之前留存世界状态 + 本回合用户输入，
	// 作为"AI 回复重试"的回滚基点。重试的重跑带 Extra 标记跳过本步，
	// 保证连续多次重试都回滚到同一基点。
	if !IsRegenRetry(ctx) {
		if err := t.worldEngine.SaveTurnSnapshot(sessionID, state, playerMessage); err != nil {
			return "", fmt.Errorf("保存回合快照失败: %w", err)
		}
	}

	// 1c. 应用运行时配置（热更新项：上下文预算与 lore 检索参数每回合读取）。
	// budget 保存在本地变量，ContextBuilder 不持有共享可变回合配置。
	contextBudget := t.contextBudget()
	loreBudget := world.DefaultLoreBudget
	loreScanRounds := world.DefaultLoreScanRounds
	loreRecursion := false
	if t.cfgStore != nil {
		loreBudget = t.cfgStore.GetInt(config.KeyLoreBudget, loreBudget)
		loreScanRounds = t.cfgStore.GetInt(config.KeyLoreScanRounds, loreScanRounds)
		loreRecursion = t.cfgStore.GetBool(config.KeyLoreRecursion, false)
	}

	// 1d. lore 检索：设定条目按需召回 + 预算装配（替代旧 Background 全量注入）
	scanText := t.buildLoreScanText(state, playerMessage, loreScanRounds)
	lore := world.Resolve(state, scanText, loreBudget, loreRecursion)

	// 1e. 长期记忆检索：相关回忆（事件/旧情节块）+ 剧情回顾（阶段摘要）。
	memoriesBlock, episodesBlock := t.buildMemoryBlocks(sessionID, state, scanText, contextBudget)

	// 1b. 世界时钟：离线演化（回归结算）+ 本回合时间推进
	worldEventsBlock := t.advanceWorldClock(state)

	// 2. 规则化指标评估（确定性）
	if t.metrics != nil {
		state.Metrics = t.metrics.Evaluate(state)
	}

	// 3. 叙事指导：AI 指导（同步，可回写状态）优先，超时/失败/禁用降级规则版
	guidance := t.applyGuidance(ctx.Ctx, state, playerMessage, sessionID)

	// 4. 低频 Planner：场景切换 / 间隔到期 / 无计划时生成场景计划
	if t.needsPlan(state) {
		t.refreshPlan(ctx.Ctx, state, playerMessage, sessionID, scanText, loreBudget, loreRecursion)
	}

	// 5. ContextBuilder 按预算组装结构化上下文（近期对话窗口以消息对承载；
	// 远期由长期记忆 world_memory 的相关回忆与阶段摘要承载）
	gameContext := t.narrator.buildGameContext(sessionID)
	eventsBlock := worldEventsBlock
	dialoguePlayer := state.EffectivePersona(userID, t.personaStore)
	input := t.ctxBuilder.BuildNarratorInputWithMemory(contextBudget, state, &lore, guidance, gameContext, eventsBlock, state.RecentTurns, playerMessage,
		LengthHintFromSession(session), buildPersonaBlock(dialoguePlayer), personaName(dialoguePlayer), memoriesBlock, episodesBlock)
	t.recordInjection(sessionID, input.InjectedLore)
	if n := len(input.InjectedLore.Front) + len(input.InjectedLore.Tail) + len(input.InjectedLore.Dropped); n > 0 {
		log.Printf("[TurnEngine] lore 实际注入: front=%d tail=%d 裁掉=%d（预算 %d）",
			len(input.InjectedLore.Front), len(input.InjectedLore.Tail), len(input.InjectedLore.Dropped), loreBudget)
	}
	t.logContextBudget(sessionID, input)

	// 6. Narrator 无状态调用（系统提示词按世界预设/自定义每轮解析）
	mainPrompt := ResolveMainPrompt(state.Style, personaName(dialoguePlayer))
	reply, err := t.narrator.NarrateMessage(ctx.Ctx, mainPrompt, input, sessionID, userID)
	if err != nil {
		return "", fmt.Errorf("Narrator 执行失败: %w", err)
	}

	// 7. 记账 + 持久化：当前 state 是本回合唯一聚合对象。
	if err := t.bookkeep(state, sessionID, playerMessage, reply, roundStart); err != nil {
		return "", fmt.Errorf("回合状态提交失败: %w", err)
	}

	// 8. 剧情记忆异步整理：阶段摘要（episode）+ 够批后合并为篇章摘要（arc）。
	if t.summary != nil {
		go func() {
			t.summary.MaybeSummarize(sessionID)
			t.summary.MaybeCompact(sessionID)
		}()
	}

	log.Printf("[TurnEngine] 回合完成 (%.1fs): session=%s, round=%d",
		time.Since(start).Seconds(), sessionID, state.RoundCount)

	return reply, nil
}

const normalHistoryKey = "normal_dialogue_history"
const normalHistoryCap = 10

func normalSessionHistory(session *core.Session) []world.DialogueTurn {
	value, ok := session.Get(normalHistoryKey)
	if !ok {
		return nil
	}
	turns, _ := value.([]world.DialogueTurn)
	return append([]world.DialogueTurn(nil), turns...)
}

func appendNormalSessionHistory(session *core.Session, player, reply string) {
	turns := normalSessionHistory(session)
	turns = append(turns, world.DialogueTurn{Player: player, Narrator: reply})
	if len(turns) > normalHistoryCap {
		turns = turns[len(turns)-normalHistoryCap:]
	}
	session.Set(normalHistoryKey, turns)
}

// globalPersona 读取玩家全局默认人设（存储未配置或读取出错返回 nil）。
func (t *TurnEngine) globalPersona(userID string) *persona.Profile {
	if t.personaStore == nil {
		return nil
	}
	p, err := t.personaStore.Get(userID)
	if err != nil {
		return nil
	}
	return p
}

// personaName 返回人设名（nil 或空名返回 ""，由调用方决定回退文案）。
func personaName(p *persona.Profile) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(p.Name)
}

// buildDialogueBlock 把短期对话窗口格式化为注入文本（无记录时返回空串）。
// 玩家侧标签用人设名（对齐 SillyTavern 的 {{user}} 效果），空则回退"玩家"；
// AI 侧统一标"世界"（其输出混合场景叙述与 NPC 言行，不赋予主持人身份）。
func buildDialogueBlock(state *world.WorldState, playerName string) string {
	if state == nil || len(state.RecentTurns) == 0 {
		return ""
	}
	return formatDialogueTurns(state.RecentTurns, playerName)
}

// formatDialogueTurns 把对话轮列表格式化为【近期对话】文本块。
// 这是 NarratorInput.Flatten（测试/调试展示）与旧版单消息布局共用的格式化。
// 实际请求中对话窗口以真实 user/assistant 消息对发送（buildNarratorMessages）。
func formatDialogueTurns(turns []world.DialogueTurn, playerName string) string {
	if len(turns) == 0 {
		return ""
	}
	if playerName == "" {
		playerName = "玩家"
	}
	var sb strings.Builder
	sb.WriteString("【近期对话】\n")
	for _, turn := range turns {
		sb.WriteString(playerName + ": " + turn.Player + "\n")
		sb.WriteString("世界: " + turn.Narrator + "\n")
	}
	return sb.String()
}

// buildLoreScanText 构建 lore 关键词层的扫描文本（设计文档 §4.2）：
// 本回合玩家消息 + 最近 K 轮决策记录 + 当前场景名与在场 NPC 名。
// （近期逐字对话已由 RecentTurns 窗口承载，决策记录作为更远期的关键词来源。）
func (t *TurnEngine) buildLoreScanText(state *world.WorldState, playerMessage string, rounds int) string {
	var sb strings.Builder
	sb.WriteString(playerMessage)
	if t.worldEngine != nil && rounds > 0 {
		for _, d := range t.worldEngine.RecentDecisions(state, rounds) {
			sb.WriteString("\n" + d.Value)
		}
	}
	for _, name := range world.SceneEntityNames(state) {
		sb.WriteString("\n" + name)
	}
	return sb.String()
}

// recordInjection 记录本回合 lore 注入清单（环形缓存，每世界保留最近 20 回合）。
func (t *TurnEngine) recordInjection(worldID string, r world.LoreResult) {
	t.injMu.Lock()
	defer t.injMu.Unlock()
	list := append(t.injections[worldID], r)
	if len(list) > recentInjectionsCap {
		list = list[len(list)-recentInjectionsCap:]
	}
	t.injections[worldID] = list
}

// RecentInjections 返回指定世界最近的 lore 注入记录（旧->新，P3 管理端 API 用）。
func (t *TurnEngine) RecentInjections(worldID string) []world.LoreResult {
	t.injMu.Lock()
	defer t.injMu.Unlock()
	src := t.injections[worldID]
	out := make([]world.LoreResult, len(src))
	copy(out, src)
	return out
}

// resolveState 决定本回合的世界状态：freechat 正常加载；normal 模式下
// 若世界状态仍在（进程重启导致内存模式回落），恢复为 freechat 并加载。
// 无世界状态时返回 nil，由调用方走纯对话路径。
func (t *TurnEngine) resolveState(session *core.Session, sessionID string) *world.WorldState {
	if session.ModeValue() != core.ModeNormal {
		return t.loadOrCreateState(sessionID)
	}
	if t.worldEngine != nil {
		if ws := t.worldEngine.LoadOrNil(sessionID); ws != nil {
			session.SetMode(core.ModeFreeChat)
			log.Printf("[TurnEngine] 会话 %s 世界状态仍在，已从 normal 恢复为 freechat", sessionID)
			return ws
		}
	}
	return nil
}

// loadOrCreateState 加载 WorldState（不存在时返回 nil，由调用方走纯对话路径）。
func (t *TurnEngine) loadOrCreateState(sessionID string) *world.WorldState {
	if t.worldEngine == nil {
		return nil
	}
	return t.worldEngine.LoadOrNil(sessionID)
}

// applyGuidance 生成本回合叙事指导：AI 指导（同步，超时 5s）优先，
// 超时/失败/配置禁用/非规划模式时降级为规则版 BuildGuidance。
// AI 指导附带的状态更新经 world 引擎单写入口回写（Actor="guidance"），
// 使规则指标能感知叙事变化（闭环）。每轮记录一条运行日志供管理端可视化。
func (t *TurnEngine) applyGuidance(
	ctx context.Context,
	state *world.WorldState,
	playerMessage string,
	sessionID string,
) *RuleGuidance {
	entry := DirectorLogEntry{
		SessionID: sessionID,
		Round:     state.RoundCount,
		Kind:      "guidance",
	}

	aiEnabled := t.guider != nil
	if aiEnabled && t.cfgStore != nil {
		aiEnabled = t.cfgStore.GetBool(config.KeyAIGuidance, true)
	}

	// 纯规则路径（AI 未装配 / 配置关闭 / roleplay 模式）
	if !aiEnabled {
		g := BuildGuidance(state)
		entry.Source = "rule"
		entry.Tone, entry.Pacing = g.Tone, g.Pacing
		entry.Focus, entry.Constraints = g.Focus, g.Constraints
		t.recordDirectorLog(entry)
		return g
	}

	res := t.guider.Guide(ctx, state, playerMessage, sessionID)

	// 状态更新回写（与 Planner 同一单写入口、同一命中校验）
	applied := 0
	if len(res.StateUpdates) > 0 {
		events := make([]world.WorldEvent, 0, len(res.StateUpdates))
		for _, u := range res.StateUpdates {
			events = append(events, world.WorldEvent{
				Type: u.Type, Actor: "guidance", Target: u.Target, Value: u.Value,
			})
		}
		applied = t.worldEngine.ApplyEvents(state, events)
		log.Printf("[TurnEngine] 叙事指导状态更新: %d/%d 条命中", applied, len(res.StateUpdates))
	}

	entry.Source = res.Source
	entry.ElapsedMs = res.ElapsedMs
	entry.Tone, entry.Pacing = res.Guidance.Tone, res.Guidance.Pacing
	entry.Focus, entry.Constraints = res.Guidance.Focus, res.Guidance.Constraints
	entry.StateUpdates = res.StateUpdates
	entry.AppliedUpdates = applied
	entry.Error = res.Err
	t.recordDirectorLog(entry)

	return res.Guidance
}

// needsPlan 判断是否需要刷新场景计划。
func (t *TurnEngine) needsPlan(state *world.WorldState) bool {
	if t.director == nil {
		return false
	}
	if state.ScenePlan == "" {
		return true
	}
	if state.PlanNodeID != state.Scene.NodeID {
		return true
	}
	// 计划间隔支持运行时配置热更新
	interval := t.planInterval
	if t.cfgStore != nil {
		interval = t.cfgStore.GetInt(config.KeyPlanInterval, interval)
	}
	if state.RoundCount-state.PlanRound >= interval {
		return true
	}
	return false
}

// refreshPlan 调用 Director LLM 生成场景计划（低频）。
// 计划文本存入 WorldState.ScenePlan；Planner 的 StateUpdates 经 ApplyEvent 落库。
// 设定文本由 LoreResolver 按需产出（预算 lore_budget*2），替代旧 Background 全文注入。
func (t *TurnEngine) refreshPlan(ctx context.Context, state *world.WorldState, playerMessage, sessionID, scanText string, loreBudget int, loreRecursion bool) {
	pStart := time.Now()

	planLore := world.Resolve(state, scanText, loreBudget*2, loreRecursion)
	scriptContext := formatLoreHits("【世界设定】", planLore.Front) +
		formatLoreHits("【补充设定】", planLore.Tail)
	if scriptContext == "" {
		// 兜底：无 lore 条目命中时回退 Background（如条目被清空的旧世界）
		scriptContext = state.Background
	}
	// 只送最近几条：Planner 是低频决策（每 plan_interval 轮），没有逐字
	// 对话可锚，送全部摘要会让它的输入随团龄线性增长（与"成本 =
	// f(复杂度)"相悖），最近几条已足够支撑场景级规划。
	// 摘要来源优先长期记忆（直接迁移），未迁移的世界回退 Summaries。
	var summaries []string
	if t.memory != nil {
		if eps, err := t.memory.Episodes(sessionID, plannerSummaryEntries); err == nil {
			for _, e := range eps {
				summaries = append(summaries, e.Text)
			}
		}
	} else if entries := state.SummaryEntries(); len(entries) > 0 {
		from := len(entries) - plannerSummaryEntries
		if from < 0 {
			from = 0
		}
		for _, e := range entries[from:] {
			summaries = append(summaries, e.Text)
		}
	}
	if len(summaries) > 0 {
		scriptContext += "\n剧情摘要: " + strings.Join(summaries, "；")
	}

	directive, err := t.director.Decide(ctx, state, playerMessage, scriptContext, sessionID)
	if err != nil || directive == nil {
		log.Printf("[TurnEngine] Planner 失败，沿用旧计划: %v", err)
		return
	}

	state.ScenePlan = formatScenePlan(directive)
	state.PlanNodeID = state.Scene.NodeID
	state.PlanRound = state.RoundCount

	// Planner 的状态变更请求经单写入入口落库
	applied := 0
	if len(directive.StateUpdates) > 0 {
		events := make([]world.WorldEvent, 0, len(directive.StateUpdates))
		for _, u := range directive.StateUpdates {
			events = append(events, world.WorldEvent{
				Type: u.Type, Actor: "planner", Target: u.Target, Value: u.Value,
			})
		}
		applied = t.worldEngine.ApplyEvents(state, events)
		log.Printf("[TurnEngine] Planner 状态更新: %d/%d 条命中", applied, len(directive.StateUpdates))
	}

	// 运行记录（管理端可视化）：Decide 内部 LLM 失败时已显式标注 Degraded
	planSource := "ai"
	if directive.Degraded {
		planSource = "fallback"
	}
	t.recordDirectorLog(DirectorLogEntry{
		SessionID:      sessionID,
		Round:          state.RoundCount,
		Kind:           "plan",
		Source:         planSource,
		ElapsedMs:      time.Since(pStart).Milliseconds(),
		StateUpdates:   directive.StateUpdates,
		AppliedUpdates: applied,
		ScenePlan:      state.ScenePlan,
		Reasoning:      directive.Reasoning,
	})

	log.Printf("[TurnEngine] 场景计划已刷新 (%.1fs): %s", time.Since(pStart).Seconds(),
		truncate(state.ScenePlan, 80))
}

// formatScenePlan 将 DecisionDirective 格式化为场景计划文本。
func formatScenePlan(d *DecisionDirective) string {
	var sb string
	if d.NarrationGuide.Tone != "" {
		sb += "叙事基调: " + d.NarrationGuide.Tone + "\n"
	}
	if d.NarrationGuide.Pacing != "" {
		sb += "节奏: " + d.NarrationGuide.Pacing + "\n"
	}
	if d.NarrationGuide.FocusPoints != "" {
		sb += "叙事重点: " + d.NarrationGuide.FocusPoints + "\n"
	}
	if d.NarrationGuide.NPCBehavior != "" {
		sb += "NPC 行为: " + d.NarrationGuide.NPCBehavior + "\n"
	}
	if d.NarrationGuide.Constraints != "" {
		sb += "约束: " + d.NarrationGuide.Constraints + "\n"
	}
	for _, a := range d.Actions {
		sb += fmt.Sprintf("计划动作: [%s] %s\n", a.Type, a.Description)
	}
	if d.Assessment.OverallSituation != "" {
		sb += "局势判断: " + d.Assessment.OverallSituation + "\n"
	}
	return sb
}

// advanceWorldClock 推进世界时钟：离线演化结算 + 本回合时间推进。
// 返回注入上下文的世界事件文本块（无事件时为空串）。
// 所有世界统一启用离线演化与时钟（不再按类型开关）。
func (t *TurnEngine) advanceWorldClock(state *world.WorldState) string {
	var sb string

	// 离线演化（回归结算）
	if ff := world.FastForward(state, time.Now()); ff != nil && len(ff.FiredEvents) > 0 {
		sb += "【世界变迁】你离开期间世界发生了变化：\n"
		for _, ev := range ff.FiredEvents {
			sb += "  - " + ev + "\n"
		}
	}

	// 本回合时间推进与到期事件
	due := world.AdvanceClock(state, world.DefaultRoundMinutes)
	if len(due) > 0 {
		sb += "【世界事件】以下事件到点触发：\n"
		for _, ev := range due {
			sb += "  - " + ev.Description + "\n"
		}
	}

	return sb
}

// bookkeep 回合记账：直接提交本回合唯一的 state，保留所有 ApplyEvent/Planner/时钟变更。
// roundStart 为本回合开始时的 RoundCount，用于筛出本回合新产生的事件写长期记忆。
func (t *TurnEngine) bookkeep(state *world.WorldState, sessionID, playerMessage, reply string, roundStart int) error {
	if t.worldEngine == nil || state == nil {
		return fmt.Errorf("世界引擎或状态为空")
	}

	// 短期窗口淘汰轮进入待总结队列（攒够批次后由 SummaryService 并入剧情摘要）。
	evicted := state.AppendTurn(playerMessage, reply)
	state.PendingSummaryTurns = append(state.PendingSummaryTurns, evicted...)
	state.RoundCount++

	// 本轮状态更新已经全部发生在 state 上；仅在提交前计算最终指标和情绪衰减。
	world.DecayMoods(state)
	if t.metrics != nil {
		state.Metrics = t.metrics.Evaluate(state)
	}
	if strings.TrimSpace(playerMessage) != "" {
		if _, err := t.worldEngine.ApplyEvent(state, world.WorldEvent{
			Type: "decision", Actor: "player", Target: state.Scene.NodeID,
			Value: truncate(playerMessage, 120),
		}); err != nil {
			return fmt.Errorf("记录玩家决策失败: %w", err)
		}
	}
	if before := state.RoundCount - world.EventLogKeepRounds; before > 0 {
		world.ArchiveEvents(state, before)
	}
	if len(state.PendingSummaryTurns) > world.MaxPendingSummaryTurns {
		drop := len(state.PendingSummaryTurns) - world.MaxPendingSummaryTurns
		state.PendingSummaryTurns = state.PendingSummaryTurns[drop:]
		log.Printf("[TurnEngine] 待总结队列超上限，保留最近 %d 轮: session=%s", world.MaxPendingSummaryTurns, sessionID)
	}
	if err := t.worldEngine.Save(state); err != nil {
		return err
	}

	// 长期记忆：本回合新产生的事件规则化落库（零 LLM；失败只记日志，不影响回合）。
	if t.memory != nil {
		t.writeEventMemories(state, roundStart)
	}
	return nil
}

// migrateSummaries 把旧版 WorldState.Summaries/CampaignSummary 直接迁移为
// 长期记忆中的 episode 块并清空旧字段（幂等：DedupeKey + 部分唯一索引）。
// 部分失败时保留旧字段，下回合重试（已写入的条目会被去重）。
func (t *TurnEngine) migrateSummaries(state *world.WorldState) {
	chunks := LegacySummaryChunks(state)
	if len(chunks) == 0 {
		state.Summaries = nil
		state.CampaignSummary = ""
		return
	}
	written, err := t.memory.AppendBatch(chunks)
	if err != nil || written < len(chunks) {
		log.Printf("[Memory] 旧摘要迁移未完成（保留待重试）: world=%s, 成功 %d/%d, err=%v",
			state.WorldID, written, len(chunks), err)
		return
	}
	state.Summaries = nil
	state.CampaignSummary = ""
	if err := t.worldEngine.Save(state); err != nil {
		log.Printf("[Memory] 迁移后清理旧摘要失败: world=%s: %v", state.WorldID, err)
		return
	}
	log.Printf("[Memory] 旧摘要已迁移到长期记忆: world=%s, %d 条", state.WorldID, written)
}

// writeEventMemories 把本回合新事件写入长期记忆（按 DedupeKey 幂等）。
func (t *TurnEngine) writeEventMemories(state *world.WorldState, roundStart int) {
	chunks := make([]memory.Chunk, 0, 8)
	for _, ev := range state.EventLog {
		if ev.Round < roundStart {
			continue
		}
		if c, ok := worldEventToMemory(state, ev); ok {
			chunks = append(chunks, c)
		}
	}
	if len(chunks) == 0 {
		return
	}
	if _, err := t.memory.AppendBatch(chunks); err != nil {
		log.Printf("[Memory] 事件记忆写入部分失败: world=%s: %v", state.WorldID, err)
	}
}

// buildMemoryBlocks 组装本回合的相关回忆与剧情回顾文本（记忆未启用时返回空串）。
func (t *TurnEngine) buildMemoryBlocks(sessionID string, state *world.WorldState, scanText string, contextBudget int) (string, string) {
	if t.memory == nil {
		return "", ""
	}
	timeline, err := t.memory.Timeline(sessionID, timelineArcLimit, recentEpisodeLimit)
	if err != nil {
		log.Printf("[Memory] 读取剧情时间线失败: world=%s: %v", sessionID, err)
	}
	timelineIDs := make(map[int64]bool, len(timeline))
	for _, c := range timeline {
		timelineIDs[c.ID] = true
	}
	// 预算过小时不检索回忆（BudgetChars=0 在存储层表示"不限"，不能直接传）。
	recalls := make([]memory.Chunk, 0, memoryRecallLimit)
	if recallBudget := contextBudget / memoryBudgetDivisor; recallBudget > 0 {
		chunks, err := t.memory.Retrieve(memory.Query{
			WorldID:     sessionID,
			Text:        scanText,
			Entities:    world.SceneEntityNames(state),
			Round:       state.RoundCount,
			Limit:       memoryRecallLimit,
			BudgetChars: recallBudget,
		})
		if err != nil {
			log.Printf("[Memory] 记忆检索失败: world=%s: %v", sessionID, err)
		}
		// 已进入【剧情回顾】时间线的块不在【相关回忆】里重复注入。
		for _, c := range chunks {
			if timelineIDs[c.ID] {
				continue
			}
			recalls = append(recalls, c)
			timelineIDs[c.ID] = true
		}
	}
	// 在场 NPC 的记忆要点（与回忆/时间线去重）。
	npcBlock := t.buildNPCMemoryBlock(sessionID, state, scanText, timelineIDs)
	if len(recalls) > 0 || len(timeline) > 0 {
		log.Printf("[Memory] 记忆注入: 回忆=%d 剧情回顾=%d NPC记忆=%d（世界=%s）",
			len(recalls), len(timeline), strings.Count(npcBlock, "\n  · "), sessionID)
	}
	return npcBlock + formatMemoryRecall(recalls), formatEpisodeReview(timeline)
}

// buildNPCMemoryBlock 为在场 NPC 生成"记忆要点"块：每人取 importance 最高的
// 若干条归属于该 NPC 的记忆，排除已进入其他分区的块。
func (t *TurnEngine) buildNPCMemoryBlock(sessionID string, state *world.WorldState, scanText string, exclude map[int64]bool) string {
	if t.memory == nil || state == nil {
		return ""
	}
	names := pickRelevantNPCs(world.PresentNPCNames(state), scanText, npcMemoryLimit)
	if len(names) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, name := range names {
		chunks, err := t.memory.SubjectMemories(sessionID, name, npcMemoryPerNPC)
		if err != nil {
			continue
		}
		lines := make([]string, 0, len(chunks))
		for _, c := range chunks {
			if exclude[c.ID] {
				continue
			}
			lines = append(lines, c.Text)
			exclude[c.ID] = true
		}
		if len(lines) == 0 {
			continue
		}
		if sb.Len() == 0 {
			sb.WriteString("【在场 NPC 的记忆】\n")
		}
		sb.WriteString("  · " + name + "：" + strings.Join(lines, "；") + "\n")
	}
	return sb.String()
}

// pickRelevantNPCs 优先取本轮文本提到的 NPC，不足时按名字顺序补齐。
func pickRelevantNPCs(names []string, scanText string, limit int) []string {
	if limit <= 0 || len(names) == 0 {
		return nil
	}
	picked := make([]string, 0, limit)
	seen := make(map[string]bool, limit)
	for _, n := range names {
		if len(picked) >= limit {
			break
		}
		if strings.Contains(scanText, n) {
			picked = append(picked, n)
			seen[n] = true
		}
	}
	for _, n := range names {
		if len(picked) >= limit {
			break
		}
		if seen[n] {
			continue
		}
		picked = append(picked, n)
		seen[n] = true
	}
	return picked
}

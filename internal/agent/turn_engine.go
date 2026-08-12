// Package agent - TurnEngine 回合引擎。
//
// 替代旧 KPPipeline（Director -> Narrator 每轮双 LLM）。
// 设计文档 3.3：同步路径上只有 Narrator 一次必需的 LLM 调用；
// Director 降级为低频 Planner（场景切换或每 planInterval 轮触发一次）。
//
// 流程：
//   1. 会话锁 + 加载 WorldState
//   2. 规则化指标评估（确定性，零 LLM）
//   3. 叙事指导：AIGuidance（同步 LLM，可回写 state_updates）优先，
//      超时/失败/禁用时降级为 RuleGuidance 规则版（确定性，零 LLM）
//   4. 低频 Planner：需要时调用 Director LLM 生成场景计划（存 WorldState.ScenePlan）
//   5. ContextBuilder 按预算组装上下文包
//   6. Narrator 无状态调用生成叙事
//   7. 记账（指标/计划/轮次）+ 持久化
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
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// DefaultPlanInterval 默认场景计划刷新间隔（轮次）。
const DefaultPlanInterval = 8

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
	director     *Director // 低频 Planner 用（可为 nil 禁用规划）
	guider       *AIGuidance // 每轮 AI 叙事指导（可为 nil 退回纯规则指导）
	metrics      *MetricsEvaluator
	worldEngine  *world.Engine
	ctxBuilder   *ContextBuilder
	summary      *SummaryService // 滚动剧情摘要服务（可为 nil 禁用）
	cfgStore     *config.Store  // 运行时配置（热更新项每回合读取，可为 nil）
	personaStore *persona.Store // 全局默认人设存储（可为 nil，nil 时人设功能关闭）
	planInterval int

	// injections 世界 ID -> 最近 N 回合 lore 注入清单（内存环形缓存）。
	injMu      sync.Mutex
	injections map[string][]world.LoreResult

	// dirLogs 导演系统运行记录（全局环形缓存，管理端可视化用）。
	dirLogMu sync.Mutex
	dirLogs  []DirectorLogEntry
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
	}
}

// SetSummary 注入滚动剧情摘要服务（由 main.go 在装配阶段调用）。
func (t *TurnEngine) SetSummary(s *SummaryService) {
	t.summary = s
	log.Printf("[TurnEngine] 剧情摘要服务已注入")
}

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
		input := t.ctxBuilder.BuildNarratorInput(nil, nil, nil, gameContext, "", nil, playerMessage,
			LengthHintFromSession(session), buildPersonaBlock(p), personaName(p))
		return t.narrator.NarrateMessage(ctx.Ctx, ResolveMainPrompt(nil, personaName(p)), input, sessionID, userID)
	}
	mode := world.GetMode(state.Mode)

	// 回合快照：在任何 mutation 之前留存世界状态 + 本回合用户输入，
	// 作为"AI 回复重试"的回滚基点。重试的重跑带 Extra 标记跳过本步，
	// 保证连续多次重试都回滚到同一基点。
	if !IsRegenRetry(ctx) {
		if err := t.worldEngine.SaveTurnSnapshot(sessionID, state, playerMessage); err != nil {
			log.Printf("[TurnEngine] 回合快照写入失败: %v", err)
		}
	}

	// 1c. 应用运行时配置（热更新项：上下文预算与 lore 检索参数每回合读取）
	loreBudget := world.DefaultLoreBudget
	loreScanRounds := world.DefaultLoreScanRounds
	loreRecursion := false
	if t.cfgStore != nil {
		t.ctxBuilder.Budget = t.cfgStore.GetInt(config.KeyContextBudget, t.ctxBuilder.Budget)
		loreBudget = t.cfgStore.GetInt(config.KeyLoreBudget, loreBudget)
		loreScanRounds = t.cfgStore.GetInt(config.KeyLoreScanRounds, loreScanRounds)
		loreRecursion = t.cfgStore.GetBool(config.KeyLoreRecursion, false)
	}

	// 1d. lore 检索：设定条目按需召回 + 预算装配（替代旧 Background 全量注入）
	scanText := t.buildLoreScanText(state, playerMessage, loreScanRounds)
	lore := world.Resolve(state, scanText, loreBudget, loreRecursion)
	t.recordInjection(sessionID, lore)
	if len(lore.Front)+len(lore.Tail)+len(lore.Dropped) > 0 {
		log.Printf("[TurnEngine] lore 注入: front=%d tail=%d 裁掉=%d（预算 %d）",
			len(lore.Front), len(lore.Tail), len(lore.Dropped), loreBudget)
	}

	// 1b. 世界时钟：离线演化（回归结算）+ 本回合时间推进
	worldEventsBlock := t.advanceWorldClock(state, mode)

	// 2. 规则化指标评估（确定性）
	if t.metrics != nil {
		state.Metrics = t.metrics.Evaluate(state)
	}

	// 3. 叙事指导：AI 指导（同步，可回写状态）优先，超时/失败/禁用降级规则版
	guidance := t.applyGuidance(ctx.Ctx, state, playerMessage, sessionID, mode)

	// 4. 低频 Planner：场景切换 / 间隔到期 / 无计划时生成场景计划
	if mode.EnablePlanner && t.needsPlan(state) {
		t.refreshPlan(ctx.Ctx, state, playerMessage, sessionID, scanText, loreBudget, loreRecursion)
	}

	// 5. ContextBuilder 按预算组装结构化上下文（近期对话窗口以消息对承载；远期由 CampaignSummary 承载）
	gameContext := t.narrator.buildGameContext(sessionID)
	eventsBlock := worldEventsBlock
	dialoguePlayer := state.EffectivePersona(userID, t.personaStore)
	input := t.ctxBuilder.BuildNarratorInput(state, &lore, guidance, gameContext, eventsBlock, state.RecentTurns, playerMessage,
		LengthHintFromSession(session), buildPersonaBlock(dialoguePlayer), personaName(dialoguePlayer))
	log.Printf("[TurnEngine] 上下文包: 设定 %d 字符 + 历史 %d 轮（预算 %d）",
		len(input.ContextBlock), len(input.History), t.ctxBuilder.Budget)

	// 6. Narrator 无状态调用（系统提示词按世界预设/自定义每轮解析）
	mainPrompt := ResolveMainPrompt(state.Style, personaName(dialoguePlayer))
	reply, err := t.narrator.NarrateMessage(ctx.Ctx, mainPrompt, input, sessionID, userID)
	if err != nil {
		return "", fmt.Errorf("Narrator 执行失败: %w", err)
	}

	// 7. 记账 + 持久化（重新加载最新状态，叠加本轮增量；含短期对话窗口）
	saved := t.bookkeep(state, sessionID, playerMessage, reply)

	// 8. 滚动剧情摘要：窗口淘汰轮已记入 PendingSummaryTurns，攒够批次异步合并进摘要
	if mode.EnableMemory && t.summary != nil && saved != nil {
		go t.summary.MaybeSummarize(sessionID)
	}

	log.Printf("[TurnEngine] 回合完成 (%.1fs): session=%s, round=%d",
		time.Since(start).Seconds(), sessionID, state.RoundCount)

	return reply, nil
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
	if session.Mode != core.ModeNormal {
		return t.loadOrCreateState(sessionID)
	}
	if t.worldEngine != nil {
		if ws := t.worldEngine.LoadOrNil(sessionID); ws != nil {
			session.Mode = core.ModeFreeChat
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
	mode world.GameMode,
) *RuleGuidance {
	entry := DirectorLogEntry{
		SessionID: sessionID,
		Round:     state.RoundCount,
		Kind:      "guidance",
	}

	aiEnabled := t.guider != nil && mode.EnablePlanner
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
	if entries := state.SummaryEntries(); len(entries) > 0 {
		parts := make([]string, 0, len(entries))
		for _, e := range entries {
			parts = append(parts, e.Text)
		}
		scriptContext += "\n剧情摘要: " + strings.Join(parts, "；")
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

	// 运行记录（管理端可视化）：Decide 内部 LLM 失败时已降级为规则指令
	planSource := "ai"
	if strings.HasPrefix(directive.Reasoning, "降级模式") {
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
	})

	log.Printf("[TurnEngine] 场景计划已刷新 (%.1fs): %s", time.Since(pStart).Seconds(),
		truncate(state.ScenePlan, 80))
}

// formatScenePlan 将 DecisionDirective 格式化为场景计划文本。
func formatScenePlan(d *DecisionDirective) string {
	var sb string
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
func (t *TurnEngine) advanceWorldClock(state *world.WorldState, mode world.GameMode) string {
	var sb string

	// 离线演化（回归结算）
	if mode.EnableOffline {
		if ff := world.FastForward(state, time.Now()); ff != nil && len(ff.FiredEvents) > 0 {
			sb += "【世界变迁】你离开期间世界发生了变化：\n"
			for _, ev := range ff.FiredEvents {
				sb += "  - " + ev + "\n"
			}
		}
	} else {
		world.TouchClock(state, time.Now())
	}

	// 本回合时间推进与到期事件
	if mode.EnableClock {
		due := world.AdvanceClock(state, world.DefaultRoundMinutes)
		if len(due) > 0 {
			sb += "【世界事件】以下事件到点触发：\n"
			for _, ev := range due {
				sb += "  - " + ev.Description + "\n"
			}
		}
	}

	return sb
}

// bookkeep 回合记账：重新加载最新状态（Narrator 工具可能已修改），
// 叠加本轮指标/计划/轮次增量与短期对话窗口后持久化。返回保存后的状态快照。
func (t *TurnEngine) bookkeep(state *world.WorldState, sessionID, playerMessage, reply string) *world.WorldState {
	if t.worldEngine == nil || state == nil {
		return nil
	}

	latest := t.worldEngine.LoadOrNil(sessionID)
	if latest == nil {
		latest = state
	}

	latest.Metrics = state.Metrics
	if state.ScenePlan != "" {
		latest.ScenePlan = state.ScenePlan
		latest.PlanNodeID = state.PlanNodeID
		latest.PlanRound = state.PlanRound
	}
	// 短期窗口淘汰轮进入待总结队列（攒够批次后由 SummaryService 并入剧情摘要）
	evicted := latest.AppendTurn(playerMessage, reply)
	latest.PendingSummaryTurns = append(latest.PendingSummaryTurns, evicted...)
	latest.RoundCount++

	// 合并世界时钟（本回合已推进）与事件触发标记（按 ID 合并，
	// 避免覆盖本回合工具调用刷新的事件队列）
	latest.Clock = state.Clock
	triggered := make(map[string]bool)
	for _, ev := range state.EventQueue {
		if ev.Triggered {
			triggered[ev.ID] = true
		}
	}
	for i := range latest.EventQueue {
		if triggered[latest.EventQueue[i].ID] {
			latest.EventQueue[i].Triggered = true
		}
	}

	// 情绪随世界时间衰减（P4）
	mode := world.GetMode(latest.Mode)
	if mode.EnableMood {
		world.DecayMoods(latest)
	}

	if err := t.worldEngine.Save(latest); err != nil {
		log.Printf("[TurnEngine] 持久化 WorldState 失败: %v", err)
	}
	return latest
}

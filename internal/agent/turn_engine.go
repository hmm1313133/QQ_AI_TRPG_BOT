// Package agent - TurnEngine 回合引擎。
//
// 替代旧 KPPipeline（Director -> Narrator 每轮双 LLM）。
// 设计文档 3.3：同步路径上只有 Narrator 一次必需的 LLM 调用；
// Director 降级为低频 Planner（场景切换或每 planInterval 轮触发一次）。
//
// 流程：
//   1. 会话锁 + 加载 WorldState
//   2. 规则化指标评估（确定性，零 LLM）
//   3. RuleGuidance 规则叙事指导（确定性，零 LLM）
//   4. 低频 Planner：需要时调用 Director LLM 生成场景计划（存 WorldState.ScenePlan）
//   5. ContextBuilder 按预算组装上下文包
//   6. Narrator 无状态调用生成叙事
//   7. 记账（指标/计划/轮次）+ 持久化
package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/config"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// DefaultPlanInterval 默认场景计划刷新间隔（轮次）。
const DefaultPlanInterval = 8

// TurnEngine 是回合引擎（KP 主循环）。
type TurnEngine struct {
	narrator     *Narrator
	director     *Director // 低频 Planner 用（可为 nil 禁用规划）
	metrics      *MetricsEvaluator
	worldEngine  *world.Engine
	ctxBuilder   *ContextBuilder
	memory       *MemoryService // 记忆服务（可为 nil 禁用）
	cfgStore     *config.Store  // 运行时配置（热更新项每回合读取，可为 nil）
	planInterval int
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
	}
}

// SetMemory 注入记忆服务（由 main.go 在装配阶段调用）。
func (t *TurnEngine) SetMemory(m *MemoryService) {
	t.memory = m
	log.Printf("[TurnEngine] 记忆服务已注入")
}

// SetConfigStore 注入运行时配置存储（热更新项每回合读取）。
func (t *TurnEngine) SetConfigStore(s *config.Store) {
	t.cfgStore = s
	log.Printf("[TurnEngine] 运行时配置已接入")
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
	state := t.loadOrCreateState(sessionID, session)
	if state == nil {
		// 无世界状态（自由模式）-> 直接叙事
		gameContext := t.narrator.buildGameContext(sessionID, userID)
		userMessage := t.ctxBuilder.BuildNarratorMessage(nil, nil, gameContext, "", playerMessage)
		return t.narrator.NarrateMessage(ctx.Ctx, userMessage, sessionID, userID)
	}
	eventLogBase := len(state.EventLog) // 本回合开始前的事件数，供 AfterTurn 取增量
	mode := world.GetMode(state.Mode)

	// 1c. 应用运行时配置（热更新项：上下文预算每回合读取）
	if t.cfgStore != nil {
		t.ctxBuilder.Budget = t.cfgStore.GetInt(config.KeyContextBudget, t.ctxBuilder.Budget)
	}

	// 1b. 世界时钟：离线演化（回归结算）+ 本回合时间推进
	worldEventsBlock := t.advanceWorldClock(state, mode)

	// 2. 规则化指标评估（确定性）
	if t.metrics != nil {
		state.Metrics = t.metrics.Evaluate(state, sessionID)
	}

	// 3. 规则叙事指导（确定性，替代每轮 Director LLM）
	guidance := BuildGuidance(state)

	// 4. 低频 Planner：场景切换 / 间隔到期 / 无计划时生成场景计划
	if mode.EnablePlanner && t.needsPlan(state) {
		t.refreshPlan(ctx.Ctx, state, playerMessage, sessionID)
	}

	// 5. ContextBuilder 按预算组装上下文包（含记忆检索块与世界事件）
	gameContext := t.narrator.buildGameContext(sessionID, userID)
	memoryBlock := worldEventsBlock
	if mode.EnableMemory && t.memory != nil {
		memoryBlock += t.memory.BuildMemoryBlock(state, playerMessage)
	}
	userMessage := t.ctxBuilder.BuildNarratorMessage(state, guidance, gameContext, memoryBlock, playerMessage)
	log.Printf("[TurnEngine] 上下文包: %d 字符（预算 %d）", len(userMessage), t.ctxBuilder.Budget)

	// 6. Narrator 无状态调用
	reply, err := t.narrator.NarrateMessage(ctx.Ctx, userMessage, sessionID, userID)
	if err != nil {
		return "", fmt.Errorf("Narrator 执行失败: %w", err)
	}

	// 7. 记账 + 持久化（重新加载最新状态，叠加本轮增量）
	saved := t.bookkeep(state, sessionID)

	// 8. 异步记忆写入（本回合事件增量 + 对话）
	if t.memory != nil && saved != nil {
		newEvents := []world.WorldEvent{}
		if len(saved.EventLog) > eventLogBase {
			newEvents = saved.EventLog[eventLogBase:]
		}
		go t.memory.AfterTurn(saved, playerMessage, reply, newEvents)
	}

	log.Printf("[TurnEngine] 回合完成 (%.1fs): session=%s, round=%d",
		time.Since(start).Seconds(), sessionID, state.RoundCount)

	return reply, nil
}

// loadOrCreateState 加载 WorldState，不存在且有剧本则初始化。
func (t *TurnEngine) loadOrCreateState(sessionID string, session *core.Session) *world.WorldState {
	if t.worldEngine == nil {
		return nil
	}

	if state := t.worldEngine.LoadOrNil(sessionID); state != nil {
		return state
	}

	if t.narrator == nil || t.narrator.scriptDeps == nil || t.narrator.scriptDeps.Archive == nil {
		return nil
	}

	scriptIDVal, ok := session.Get("script_id")
	if !ok || scriptIDVal == nil {
		return nil
	}
	scriptID, ok := scriptIDVal.(string)
	if !ok || scriptID == "" {
		return nil
	}

	scr, err := t.narrator.scriptDeps.Archive.Get(scriptID)
	if err != nil {
		return nil
	}

	state, err := t.worldEngine.InitFromScript(sessionID, scr)
	if err != nil {
		log.Printf("[TurnEngine] 初始化 WorldState 失败: %v", err)
		return nil
	}
	return state
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
func (t *TurnEngine) refreshPlan(ctx context.Context, state *world.WorldState, playerMessage, sessionID string) {
	pStart := time.Now()

	scriptContext := state.Background
	if state.CampaignSummary != "" {
		scriptContext += "\n剧情摘要: " + state.CampaignSummary
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
	if len(directive.StateUpdates) > 0 {
		events := make([]world.WorldEvent, 0, len(directive.StateUpdates))
		for _, u := range directive.StateUpdates {
			events = append(events, world.WorldEvent{
				Type: u.Type, Actor: "planner", Target: u.Target, Value: u.Value,
			})
		}
		applied := t.worldEngine.ApplyEvents(state, events)
		log.Printf("[TurnEngine] Planner 状态更新: %d/%d 条命中", applied, len(directive.StateUpdates))
	}

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
// 叠加本轮指标/计划/轮次增量后持久化。返回保存后的状态快照。
func (t *TurnEngine) bookkeep(state *world.WorldState, sessionID string) *world.WorldState {
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
	latest.RoundCount++

	// 合并世界时钟（本回合已推进）与事件触发标记（按 ID 合并，
	// 避免覆盖 advance_timeline 刷新的事件队列）
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

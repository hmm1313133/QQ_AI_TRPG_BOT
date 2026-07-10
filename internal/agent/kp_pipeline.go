// Package agent - KPPipeline 流水线协调器。
//
// KPPipeline 编排 Director -> Narrator -> StateUpdate 流程，
// 替换原有 KPAgent.Chat() 的单次 LLM 调用。
//
// 流程：
//   1. 加载 GameState（不存在则尝试从剧本初始化）
//   2. Director.Decide() - 规则化预评估 + LLM 决策 -> DecisionDirective
//   3. Narrator.Narrate() - 注入 DecisionDirective -> 叙事文本
//   4. 应用 StateUpdates + 持久化 GameState
//
// 降级策略：
//   - Director 失败 -> 规则化基础指令，Narrator 正常运行
//   - Narrator 失败 -> 返回 Director 的叙事指导作为兜底回复
//   - GameState 加载失败 -> 无剧本模式，跳过 Director 直接走 Narrator
package agent

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
)

// KPPipeline 是 KP 多层流水线协调器。
type KPPipeline struct {
	director   *Director
	narrator   *Narrator
	stateStore *GameStateStore
}

// NewKPPipeline 创建流水线协调器。
func NewKPPipeline(
	director *Director,
	narrator *Narrator,
	stateStore *GameStateStore,
) *KPPipeline {
	return &KPPipeline{
		director:   director,
		narrator:   narrator,
		stateStore: stateStore,
	}
}

// Run 执行一轮完整的 KP 流水线。
// 返回叙事文本和可能的错误。
func (p *KPPipeline) Run(
	ctx *core.MessageContext,
	session *core.Session,
) (string, error) {
	start := time.Now()
	sessionID := ctx.SessionID
	userID := ctx.UserID
	playerMessage := ctx.Content

	// 1. 加载 GameState
	state := p.loadOrCreateState(sessionID, session)

	var directive *DecisionDirective
	var scriptContext string

	if state != nil {
		// 有 GameState -> 走完整 Director -> Narrator 流水线
		scriptContext = state.StoryContext

		// 2. Director 决策
		dStart := time.Now()
		directive = p.runDirector(ctx.Ctx, state, playerMessage, scriptContext, sessionID)
		log.Printf("[KPPipeline] Director 阶段耗时: %.1fs", time.Since(dStart).Seconds())

		// 3. Narrator 叙事
		nStart := time.Now()
		gameContext := p.narrator.buildGameContext(sessionID, userID)
		reply, err := p.narrator.Narrate(ctx.Ctx, state, directive, gameContext, playerMessage, sessionID, userID)
		log.Printf("[KPPipeline] Narrator 阶段耗时: %.1fs", time.Since(nStart).Seconds())

		if err != nil {
			log.Printf("[KPPipeline] Narrator 失败，降级兜底: %v", err)
			reply = p.fallbackReply(directive, playerMessage)
		}

		// 4. 应用 StateUpdates + 持久化 GameState
		p.applyUpdatesAndSave(state, directive, sessionID)

		log.Printf("[KPPipeline] 流水线完成 (%.1fs): session=%s, round=%d",
			time.Since(start).Seconds(), sessionID, state.RoundCount)

		return reply, nil
	}

	// 无 GameState（无剧本模式） -> 直接走 Narrator
	log.Printf("[KPPipeline] 无 GameState，直接走 Narrator: session=%s", sessionID)
	gameContext := p.narrator.buildGameContext(sessionID, userID)
	reply, err := p.narrator.Narrate(ctx.Ctx, nil, nil, gameContext, playerMessage, sessionID, userID)
	if err != nil {
		return "", fmt.Errorf("Narrator 执行失败: %w", err)
	}

	log.Printf("[KPPipeline] 直接叙事完成 (%.1fs): session=%s",
		time.Since(start).Seconds(), sessionID)

	return reply, nil
}

// loadOrCreateState 加载 GameState。
// 如果 GameStateStore 可用且会话已加载剧本，加载或创建 GameState。
// 无剧本模式返回 nil。
func (p *KPPipeline) loadOrCreateState(sessionID string, session *core.Session) *GameState {
	if p.stateStore == nil {
		return nil
	}

	// 尝试加载已有 GameState
	state := p.stateStore.LoadOrDefault(sessionID)
	if state != nil {
		return state
	}

	// 尝试从已加载的剧本初始化
	if p.narrator == nil || p.narrator.scriptDeps == nil || p.narrator.scriptDeps.Archive == nil {
		return nil
	}

	// 检查会话是否有 script_id
	scriptIDVal, ok := session.Get("script_id")
	if !ok || scriptIDVal == nil {
		return nil
	}
	scriptID, ok := scriptIDVal.(string)
	if !ok || scriptID == "" {
		return nil
	}

	scr, err := p.narrator.scriptDeps.Archive.Get(scriptID)
	if err != nil {
		return nil
	}

	// 初始化 GameState
	state, err = p.stateStore.InitFromScript(sessionID, scr)
	if err != nil {
		log.Printf("[KPPipeline] 初始化 GameState 失败: %v", err)
		return nil
	}

	return state
}

// runDirector 执行 Director 决策。
// Director 内部已有降级逻辑，这里只处理 panic 恢复。
func (p *KPPipeline) runDirector(
	ctx context.Context,
	state *GameState,
	playerMessage string,
	scriptContext string,
	sessionID string,
) *DecisionDirective {
	if p.director == nil {
		return nil
	}

	directive, err := p.director.Decide(ctx, state, playerMessage, scriptContext, sessionID)
	if err != nil {
		log.Printf("[KPPipeline] Director 异常，使用 nil 指令: %v", err)
		return nil
	}

	return directive
}

// applyUpdatesAndSave 应用 DecisionDirective 中的 StateUpdates 并持久化 GameState。
func (p *KPPipeline) applyUpdatesAndSave(state *GameState, directive *DecisionDirective, sessionID string) {
	if p.stateStore == nil || state == nil {
		return
	}

	// 应用 StateUpdates
	if directive != nil && len(directive.StateUpdates) > 0 {
		state.ApplyUpdates(directive.StateUpdates)
		log.Printf("[KPPipeline] 应用 %d 个状态更新", len(directive.StateUpdates))
	}

	// 保存 LastDirective
	state.LastDirective = directive

	// 增加轮次计数
	state.RoundCount++

	// 持久化
	if err := p.stateStore.Save(state); err != nil {
		log.Printf("[KPPipeline] 持久化 GameState 失败: %v", err)
	}
}

// fallbackReply 在 Narrator 失败时生成兜底回复。
func (p *KPPipeline) fallbackReply(directive *DecisionDirective, playerMessage string) string {
	if directive != nil && directive.NarrationGuide.FocusPoints != "" {
		reply := fmt.Sprintf("（系统提示：Narrator 暂时不可用，以下是导演指导）\n\n")
		reply += fmt.Sprintf("叙事基调: %s\n", directive.NarrationGuide.Tone)
		reply += fmt.Sprintf("本轮重点: %s\n", directive.NarrationGuide.FocusPoints)
		if directive.NarrationGuide.NPCBehavior != "" {
			reply += fmt.Sprintf("NPC行为: %s\n", directive.NarrationGuide.NPCBehavior)
		}
		if directive.Reasoning != "" {
			reply += fmt.Sprintf("\n导演推理: %s\n", directive.Reasoning)
		}
		return reply
	}

	return "（KP 暂时无法响应，请稍后重试）"
}

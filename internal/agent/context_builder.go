// Package agent - ContextBuilder 上下文包组装器。
//
// 设计文档 8.2：每回合按 token 预算组装上下文，替代旧版
// "全量 GameState JSON + 无限累积的会话历史"。
// 这是长团上下文可控的根本机制：
//
//	成本 = f(回合复杂度)，而非 f(历史长度)。
//
// 预算以字符数（rune）近似（中文 1 token ≈ 1.5-2 字符，取保守值），
// 各分区按优先级贪心装配，超预算的低优先级分区被裁剪。
//
// 预算是"用户侧载荷"的硬上限（设定块 + 近期对话 + 本轮玩家消息 + 尾部
// 指令区），system 主提示词与工具声明另计。必需分区不参与裁剪，因此
// 极端情况下仍可能超出（见 NarratorInput.Used/Budget 日志）。
package agent

import (
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

const (
	// DefaultContextBudget 默认上下文预算（字符数，约 40000 tokens，按 1 token ≈ 1.5 字符）。
	// 2026-10 从 45000 上调到 60000：lite 模型窗口普遍 ≥128k，多出的空间用于
	// 长期记忆召回与在场实体详情，仍给 system 提示词与工具声明留足余量。
	DefaultContextBudget = 60000

	// historyBudgetShare 近期对话窗口最多占用的预算比例。
	// 对话是风格的锚点，给较高配额；超出时从最旧开始裁，至少保留最近一轮。
	historyBudgetShare = 0.4
)

// NarratorInput 一轮 Narrator 调用的结构化输入。
//
// 上下文以标准 messages 结构发送（system 主提示词 + 设定资料 user 消息 +
// 近期对话 user/assistant 消息对 + 本轮玩家 user 消息），对齐
// DeepSeek/OpenAI chat completions 与 SillyTavern 的用法，不再把全部
// 分区拼接进单条 user 消息——对话历史以独立消息对呈现，模型对
// "谁说的话"的边界理解明显更好。
type NarratorInput struct {
	ContextBlock  string               // 设定资料块（规则指导/基调/人设/lore/状态摘要/计划等）
	History       []world.DialogueTurn // 近期对话窗口（按预算裁剪，至少保留最近一轮）
	PlayerName    string               // 玩家显示名（人设名，空则"玩家"）
	PlayerMessage string               // 本轮玩家消息原文
	Tail          string               // 尾部指令区（长度/风格/CoT/lore tail，Author's Note 位置）

	// InjectedLore 本回合实际装配进上下文的 lore 命中（被预算裁掉的进 Dropped）。
	// 与 TurnEngine.recordInjection 配合，避免"管理端显示已注入、实际没进模型"。
	InjectedLore world.LoreResult

	Budget         int // 本回合生效的字符预算
	Used           int // 实际占用（rune，含玩家消息与历史）
	TrimmedHistory int // 因预算被裁掉的对话轮数
}

// Flatten 把结构化输入平铺为单条文本（测试与调试展示用，布局同旧版单消息）。
func (in *NarratorInput) Flatten() string {
	var sb strings.Builder
	sb.WriteString(in.ContextBlock)
	if len(in.History) > 0 {
		sb.WriteString(formatDialogueTurns(in.History, in.PlayerName))
	}
	name := in.PlayerName
	if name == "" {
		name = "玩家"
	}
	sb.WriteString("\n" + name + ": " + in.PlayerMessage + "\n")
	sb.WriteString(in.Tail)
	return sb.String()
}

// contextSection 一个待装配的上下文分区。
type contextSection struct {
	priority int    // 数值越小优先级越高
	name     string // 日志用
	group    string // 分组键：同一 group 的首个入选分区前输出一次 header（用于 lore 逐条装配）
	header   string // 分组标题（仅 group 首次入选时输出）
	content  string
	required bool // 必需分区不参与裁剪
}

// ContextBuilder 按预算组装 Narrator 的每轮用户消息。
type ContextBuilder struct {
	Budget int // 字符预算
}

// NewContextBuilder 创建上下文组装器。budget <= 0 时用默认值。
func NewContextBuilder(budget int) *ContextBuilder {
	if budget <= 0 {
		budget = DefaultContextBudget
	}
	return &ContextBuilder{Budget: budget}
}

// BuildNarratorInput 组装 Narrator 一轮调用的结构化输入。
//
// 分区按"相对稳定 → 每轮剧变"排序（设定资料块内部保持稳定顺序；system 主
// 提示词 + 设定资料块构成稳定前缀，吃厂商前缀缓存，对话历史与本轮玩家消息
// 追加在后）：
//
//	设定资料块（合并为一条 user 消息）：
//	  1. 世界基调（必需；Style.Tone，恒定稳定）
//	  2. 玩家人设（必需；本世界覆盖 > 全局默认；稳定设定，当作"谁在行动"的背景）
//	  3. lore front 世界观区（恒定条目稳定，关键词命中随回合变化；逐条装配，
//	     预算不足时只裁掉放不下的条目，不会整块消失）
//	  4. 游戏上下文：会话级状态（基本稳定，尾部骰点等小幅波动）
//	  5. 世界事件块（时钟推进/离线演化产生的本回合事件）
//	  6. 游戏运行态摘要核心 + 锁定事实（必需；每轮都变）
//	  7. 在场 NPC/物品详情（可选高优先级；创作字段全文注入，真超预算才整段裁剪）
//	  8. 剧情回顾（可选；阶段摘要，条数受限，超预算整段裁剪）
//	  9. 场景计划 ScenePlan（按计划间隔刷新）
//	对话历史：近期对话窗口（最多占 40% 预算，超出从最旧开始裁，至少保留一轮）
//	本轮玩家消息：最终 user 消息正文
//	尾部指令区（拼接在玩家消息之后，Author's Note 位置）：
//	  10. 叙事指导（必需；AI 指导每轮变化，放尾部避免切断稳定前缀缓存）
//	  11. 回复长度要求（必需）12. 回复风格要求（必需；Style.Core，空则回退旧
//	      ReplyStyle 字段）13. 输出格式要求（必需；Style.EnableCoT 时的思维链
//	      指导）14. lore tail 风格指令区（逐条装配）
//
// 裁剪优先级与位置对齐：超预算时优先裁尾部可选分区，保住稳定前缀。
func (b *ContextBuilder) BuildNarratorInput(
	state *world.WorldState,
	lore *world.LoreResult,
	guidance *RuleGuidance,
	gameContext string,
	eventsBlock string,
	recentTurns []world.DialogueTurn,
	playerMessage string,
	lengthHint string,
	personaBlock string,
	playerName string,
) *NarratorInput {
	return b.BuildNarratorInputWithBudget(b.Budget, state, lore, guidance, gameContext, eventsBlock, recentTurns, playerMessage, lengthHint, personaBlock, playerName)
}

// BuildNarratorInputWithBudget 使用本次调用的局部预算，避免共享 Builder 状态产生竞态。
func (b *ContextBuilder) BuildNarratorInputWithBudget(
	budget int,
	state *world.WorldState,
	lore *world.LoreResult,
	guidance *RuleGuidance,
	gameContext string,
	eventsBlock string,
	recentTurns []world.DialogueTurn,
	playerMessage string,
	lengthHint string,
	personaBlock string,
	playerName string,
) *NarratorInput {
	return b.BuildNarratorInputWithMemory(budget, state, lore, guidance, gameContext, eventsBlock,
		recentTurns, playerMessage, lengthHint, personaBlock, playerName, "", "")
}

// BuildNarratorInputWithMemory 在标准装配基础上追加长期记忆分区
// （memories=相关回忆，episodes=剧情回顾）。空串表示无记忆或记忆未启用。
func (b *ContextBuilder) BuildNarratorInputWithMemory(
	budget int,
	state *world.WorldState,
	lore *world.LoreResult,
	guidance *RuleGuidance,
	gameContext string,
	eventsBlock string,
	recentTurns []world.DialogueTurn,
	playerMessage string,
	lengthHint string,
	personaBlock string,
	playerName string,
	memories string,
	episodes string,
) *NarratorInput {
	if budget <= 0 {
		budget = DefaultContextBudget
	}

	// 1. 玩家消息与对话窗口先占预算（对话是风格锚点，最多 40%，从最旧裁）。
	playerCost := runeCost(playerMessage) + runeCost(playerName) + 16
	historyCap := int(float64(budget) * historyBudgetShare)
	history, trimmedHistory := trimHistoryToBudget(recentTurns, historyCap)
	historyCost := 0
	for _, turn := range history {
		historyCost += runeCost(turn.Player) + runeCost(turn.Narrator) + 8
	}
	contextBudget := budget - playerCost - historyCost
	if contextBudget < 0 {
		contextBudget = 0
	}

	var ctxSections, tailSections []contextSection

	// lore 命中逐条装配：预算不足时只裁掉放不下的条目（整块装配会出现
	// "一条超长条目带崩整个世界观区"）。记录 section 下标用于回填实际注入。
	type loreRef struct {
		hit     world.LoreHit
		section int
		tail    bool
	}
	var loreRefs []loreRef

	// 必需：世界基调（恒定稳定，放最前保前缀缓存）
	if state != nil && state.Style != nil && strings.TrimSpace(state.Style.Tone) != "" {
		ctxSections = append(ctxSections, contextSection{
			priority: 1, name: "style_tone",
			content: "【世界基调】\n" + state.Style.Tone, required: true,
		})
	}

	// 必需：玩家人设（稳定设定，前区注入；模型把它当作"谁在行动"的背景）
	if personaBlock != "" {
		ctxSections = append(ctxSections, contextSection{
			priority: 1, name: "persona",
			content: personaBlock, required: true,
		})
	}

	// 可选：lore front（世界观区，状态摘要之前）
	if lore != nil {
		for _, h := range lore.Front {
			idx := len(ctxSections)
			ctxSections = append(ctxSections, contextSection{
				priority: 2, name: "lore_front",
				group: "lore_front", header: "【世界设定】\n",
				content: formatLoreHit(h),
			})
			loreRefs = append(loreRefs, loreRef{hit: h, section: idx})
		}
	}

	// 可选：游戏上下文（会话级状态，基本稳定）
	if gameContext != "" {
		ctxSections = append(ctxSections, contextSection{
			priority: 3, name: "game_context",
			content: gameContext,
		})
	}

	// 可选：世界事件块（时钟推进/离线演化产生的本回合事件）
	if eventsBlock != "" {
		ctxSections = append(ctxSections, contextSection{
			priority: 4, name: "events",
			content: eventsBlock,
		})
	}

	// 必需：运行态摘要核心（场景/目标/锁定事实/指标等，每轮变化）
	if state != nil {
		ctxSections = append(ctxSections, contextSection{
			priority: 1, name: "state_summary",
			content: buildGameStateSummary(state, lore), required: true,
		})
		// 可选（高优先级）：在场 NPC + 相关物品详情。创作字段全文注入，
		// 预算充裕时全量保留；真超预算时整段让位于核心状态。
		if details := buildEntityDetailBlock(state, lore); details != "" {
			ctxSections = append(ctxSections, contextSection{
				priority: 2, name: "entity_details",
				content: details,
			})
		}
	}

	// 可选：相关回忆（长期记忆检索结果；每轮随玩家行动变化）
	if memories != "" {
		ctxSections = append(ctxSections, contextSection{
			priority: 2, name: "memory_recall",
			content: memories,
		})
	}

	// 可选：剧情回顾（阶段摘要；优先用长期记忆里的 episode 块，
	// 旧世界尚未迁移时回退 WorldState.Summaries）
	review := episodes
	if review == "" && state != nil {
		review = buildStoryReview(state)
	}
	if review != "" {
		ctxSections = append(ctxSections, contextSection{
			priority: 3, name: "story_review",
			content: review,
		})
	}

	// 可选：场景计划
	if state != nil && state.ScenePlan != "" {
		ctxSections = append(ctxSections, contextSection{
			priority: 5, name: "scene_plan",
			content: "【场景计划】\n" + state.ScenePlan + "\n",
		})
	}

	// 必需：叙事指导（每轮变化，放尾部 Author's Note 位置；
	// 稳定前缀留给基调/人设/lore 恒定条目，避免每轮切断前缀缓存）
	if guidance != nil {
		tailSections = append(tailSections, contextSection{
			priority: 1, name: "guidance",
			content: guidance.String(), required: true,
		})
	}

	// 必需：回复长度要求（会话级偏好，玩家消息之后）
	if lengthHint != "" {
		tailSections = append(tailSections, contextSection{
			priority: 1, name: "length_hint",
			content: lengthHint, required: true,
		})
	}

	// 必需：回复风格要求（Style.Core，空则回退旧 ReplyStyle 字段，Author's Note 位置）
	if state != nil {
		if core := strings.TrimSpace(state.EffectiveStyleCore()); core != "" {
			tailSections = append(tailSections, contextSection{
				priority: 1, name: "reply_style",
				content: "【回复风格要求】\n" + core, required: true,
			})
		}
	}

	// 必需：输出格式要求（思维链指导，回复风格之后；自定义 CoTGuide 覆盖内置默认）
	if state != nil && state.Style != nil && state.Style.EnableCoT {
		tailSections = append(tailSections, contextSection{
			priority: 1, name: "cot_guide",
			content: CoTGuideFor(state.Style.CoTGuide), required: true,
		})
	}

	// 可选：lore tail（风格指令区，玩家消息之后，Author's Note 位置）
	if lore != nil {
		for _, h := range lore.Tail {
			idx := len(tailSections)
			tailSections = append(tailSections, contextSection{
				priority: 6, name: "lore_tail",
				group: "lore_tail", header: "【补充设定】\n",
				content: formatLoreHit(h),
			})
			loreRefs = append(loreRefs, loreRef{hit: h, section: idx, tail: true})
		}
	}

	contextBlock, includedCtx := assembleWithBudget(ctxSections, contextBudget)
	tailBudget := budget - playerCost - historyCost - runeCost(contextBlock)
	if tailBudget < 0 {
		tailBudget = 0
	}
	tail, includedTail := assembleWithBudget(tailSections, tailBudget)

	// 回填实际注入的 lore 命中（Resolver 已按 lore_budget 裁掉的也算 Dropped）。
	injected := world.LoreResult{}
	if lore != nil && len(lore.Dropped) > 0 {
		injected.Dropped = append(injected.Dropped, lore.Dropped...)
	}
	for _, ref := range loreRefs {
		ok := includedCtx[ref.section]
		if ref.tail {
			ok = includedTail[ref.section]
		}
		switch {
		case !ok:
			injected.Dropped = append(injected.Dropped, ref.hit)
		case ref.tail:
			injected.Tail = append(injected.Tail, ref.hit)
		default:
			injected.Front = append(injected.Front, ref.hit)
		}
	}

	return &NarratorInput{
		ContextBlock:   contextBlock,
		History:        history,
		PlayerName:     playerName,
		PlayerMessage:  playerMessage,
		Tail:           tail,
		InjectedLore:   injected,
		Budget:         budget,
		Used:           playerCost + historyCost + runeCost(contextBlock) + runeCost(tail),
		TrimmedHistory: trimmedHistory,
	}
}

func runeCost(s string) int { return len([]rune(s)) }

// assembleWithBudget 按优先级贪心装配分区，返回渲染文本与入选下标集合。
// required 分区无条件入选；budget < 0 视为不限预算，budget == 0 只保留必需分区。
func assembleWithBudget(sections []contextSection, budget int) (string, map[int]bool) {
	included := make(map[int]bool, len(sections))
	used := 0
	for i, s := range sections {
		if s.required {
			included[i] = true
			used += runeCost(s.content)
		}
	}
	order := make([]int, 0, len(sections))
	for i, s := range sections {
		if !s.required {
			order = append(order, i)
		}
	}
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if sections[order[j]].priority < sections[order[i]].priority {
				order[i], order[j] = order[j], order[i]
			}
		}
	}
	for _, i := range order {
		cost := runeCost(sections[i].content)
		if budget < 0 || (budget > 0 && used+cost <= budget) {
			included[i] = true
			used += cost
		}
	}
	var sb strings.Builder
	emitted := make(map[string]bool, 2)
	for i, s := range sections {
		if !included[i] {
			continue
		}
		if s.group != "" && s.header != "" && !emitted[s.group] {
			sb.WriteString(s.header)
			emitted[s.group] = true
		}
		sb.WriteString(s.content)
		if !strings.HasSuffix(s.content, "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String(), included
}

// trimHistoryToBudget 按预算裁剪近期对话窗口：从最旧开始丢弃，至少保留最近一轮
// （历史是风格的锚点，不能整体消失）。返回保留轮次与被裁掉的轮数。
func trimHistoryToBudget(turns []world.DialogueTurn, maxChars int) ([]world.DialogueTurn, int) {
	if len(turns) == 0 {
		return nil, 0
	}
	if maxChars < 0 {
		maxChars = 0
	}
	used := 0
	kept := make([]world.DialogueTurn, 0, len(turns))
	for i := len(turns) - 1; i >= 0; i-- {
		cost := runeCost(turns[i].Player) + runeCost(turns[i].Narrator) + 8
		if len(kept) > 0 && used+cost > maxChars {
			break
		}
		kept = append(kept, turns[i])
		used += cost
	}
	for i, j := 0, len(kept)-1; i < j; i, j = i+1, j-1 {
		kept[i], kept[j] = kept[j], kept[i]
	}
	return kept, len(turns) - len(kept)
}

// formatLoreHit 单条 lore 命中格式化（分组标题由装配器统一输出）。
func formatLoreHit(h world.LoreHit) string {
	var sb strings.Builder
	if title := strings.TrimSpace(h.Entry.Title); title != "" {
		sb.WriteString("◆ " + title + "\n")
	}
	sb.WriteString(h.Entry.Content + "\n")
	return sb.String()
}

// formatLoreHits 把 lore 命中格式化为注入文本块（无命中返回空串）。
func formatLoreHits(header string, hits []world.LoreHit) string {
	if len(hits) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(header + "\n")
	for _, h := range hits {
		sb.WriteString("◆ " + h.Entry.Title + "\n")
		sb.WriteString(h.Entry.Content + "\n")
	}
	return sb.String()
}

// buildPersonaBlock 把生效人设格式化为注入文本（nil 或全空返回空串）。
// 形如：【玩家人设】林月: 冷静果断的私家侦探，短发（名字为空则省略名字段）。
func buildPersonaBlock(p *persona.Profile) string {
	if p.Empty() {
		return ""
	}
	if p.Name == "" {
		return "【玩家人设】\n" + p.Description
	}
	if p.Description == "" {
		return "【玩家人设】\n" + p.Name
	}
	return "【玩家人设】\n" + p.Name + ": " + p.Description
}

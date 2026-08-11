// Package agent - ContextBuilder 上下文包组装器。
//
// 设计文档 8.2：每回合按 token 预算组装上下文，替代旧版
// "全量 GameState JSON + 无限累积的会话历史"。
// 这是长团上下文可控的根本机制：
//   成本 = f(回合复杂度)，而非 f(历史长度)。
//
// 预算以字符数近似（中文 1 token ≈ 1.5-2 字符，取保守值），
// 各分区按优先级贪心装配，超预算的低优先级分区被裁剪。
package agent

import (
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// DefaultContextBudget 默认上下文预算（字符数，约 30000 tokens，按 1 token ≈ 1.5 字符）。
const DefaultContextBudget = 45000

// NarratorInput 一轮 Narrator 调用的结构化输入。
//
// 上下文以标准 messages 结构发送（system 主提示词 + 设定资料 user 消息 +
// 近期对话 user/assistant 消息对 + 本轮玩家 user 消息），对齐
// DeepSeek/OpenAI chat completions 与 SillyTavern 的用法，不再把全部
// 分区拼接进单条 user 消息——对话历史以独立消息对呈现，模型对
// "谁说的话"的边界理解明显更好。
type NarratorInput struct {
	ContextBlock  string               // 设定资料块（规则指导/基调/人设/lore/状态摘要/计划等）
	History       []world.DialogueTurn // 近期对话窗口（固定轮数，不裁剪）
	PlayerName    string               // 玩家显示名（人设名，空则"玩家"）
	PlayerMessage string               // 本轮玩家消息原文
	Tail          string               // 尾部指令区（长度/风格/CoT/lore tail，Author's Note 位置）
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
//	  1. 规则叙事指导（必需；整局不变）
//	  2. 世界基调（必需；Style.Tone，恒定稳定）
//	  3. 玩家人设（必需；本世界覆盖 > 全局默认；稳定设定，当作"谁在行动"的背景）
//	  4. 游戏上下文：会话级状态（基本稳定，尾部骰点等小幅波动）
//	  5. lore front 世界观区（恒定条目稳定，关键词命中随回合变化）
//	  6. 世界事件块（时钟推进/离线演化产生的本回合事件）
//	  7. 游戏运行态摘要核心 + 锁定事实（必需；每轮都变）
//	  8. 在场 NPC/物品详情（可选高优先级；创作字段全文注入，真超预算才整段裁剪）
//	  9. 场景计划 ScenePlan（按计划间隔刷新）
//	对话历史：近期对话窗口（固定轮数不裁剪，user/assistant 消息对——SillyTavern
//	  式处理：历史是风格的锚点，裁历史比裁设定更伤体验）
//	本轮玩家消息：最终 user 消息正文
//	尾部指令区（拼接在玩家消息之后，Author's Note 位置）：
//	  10. 回复长度要求（必需）11. 回复风格要求（必需；Style.Core，空则回退旧
//	      ReplyStyle 字段）12. 输出格式要求（必需；Style.EnableCoT 时的思维链
//	      指导）13. lore tail 风格指令区
//
// 裁剪优先级与位置对齐：超预算时优先裁尾部可选分区，保住稳定前缀。
// lore 已经 LoreResolver 按 lore_budget 裁剪，此处作为可选分区接入。
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
	var ctxSections, tailSections []contextSection

	// 必需：规则指导（整局稳定，放最前）
	if guidance != nil {
		ctxSections = append(ctxSections, contextSection{
			priority: 1, name: "guidance",
			content: guidance.String(), required: true,
		})
	}

	// 必需：世界基调（恒定稳定，紧跟 guidance 保前缀缓存）
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

	// 可选：游戏上下文（会话级状态，基本稳定）
	if gameContext != "" {
		ctxSections = append(ctxSections, contextSection{
			priority: 3, name: "game_context",
			content: gameContext,
		})
	}

	// 可选：lore front（世界观区，状态摘要之前）
	if lore != nil && len(lore.Front) > 0 {
		ctxSections = append(ctxSections, contextSection{
			priority: 2, name: "lore_front",
			content: formatLoreHits("【世界设定】", lore.Front),
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

	// 可选：场景计划
	if state != nil && state.ScenePlan != "" {
		ctxSections = append(ctxSections, contextSection{
			priority: 5, name: "scene_plan",
			content: "【场景计划】\n" + state.ScenePlan + "\n",
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
	if lore != nil && len(lore.Tail) > 0 {
		tailSections = append(tailSections, contextSection{
			priority: 6, name: "lore_tail",
			content: formatLoreHits("【补充设定】", lore.Tail),
		})
	}

	return &NarratorInput{
		ContextBlock:  b.assemble(ctxSections),
		History:       recentTurns,
		PlayerName:    playerName,
		PlayerMessage: playerMessage,
		Tail:          b.assemble(tailSections),
	}
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

// assemble 按优先级贪心装配分区，超预算的可选分区被裁剪（高数值优先级先裁）。
func (b *ContextBuilder) assemble(sections []contextSection) string {
	// 计算必需分区总开销
	used := 0
	for _, s := range sections {
		if s.required {
			used += len(s.content)
		}
	}

	// 可选分区按优先级升序尝试加入
	included := make(map[int]bool)
	order := []int{}
	for i, s := range sections {
		if !s.required {
			order = append(order, i)
		} else {
			included[i] = true
		}
	}
	// 按 priority 排序（简单插入排序，分区数量小）
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if sections[order[j]].priority < sections[order[i]].priority {
				order[i], order[j] = order[j], order[i]
			}
		}
	}
	for _, i := range order {
		if used+len(sections[i].content) <= b.Budget {
			included[i] = true
			used += len(sections[i].content)
		}
		// 超预算则跳过（裁剪）
	}

	// 按原始顺序拼接
	var sb strings.Builder
	for i, s := range sections {
		if included[i] {
			sb.WriteString(s.content)
			if !strings.HasSuffix(s.content, "\n") {
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

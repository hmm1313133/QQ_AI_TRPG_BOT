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

// BuildNarratorMessage 组装 Narrator 的每轮用户消息。
//
// 分区按"相对稳定 → 每轮剧变"排序（Narrator 为无状态调用，请求 = 系统提示词
// + 单条用户消息；厂商前缀缓存按请求前缀命中，稳定分区靠前可让连续回合的
// 用户消息共享前缀缓存块，降低成本）：
//   1. 规则叙事指导（必需；整局不变）
//   2. 世界基调（必需；Style.Tone，恒定稳定，吃前缀缓存）
//   3. 游戏上下文：规则集/角色卡（基本稳定，尾部骰点等小幅波动）
//   4. lore front 世界观区（恒定条目稳定，关键词命中随回合变化）
//   5. 记忆检索块（随回合检索结果变化）
//   6. 游戏运行态摘要 + 锁定事实（必需；每轮都变）
//   7. 场景计划 ScenePlan（按计划间隔刷新）
//   8. 近期对话窗口（每轮追加一轮，短记忆）
//   9. 玩家人设（必需；本世界覆盖 > 全局默认，玩家消息之前）
//   10. 玩家消息（必需；每轮必变，最靠后）
//   11. 回复长度要求（必需；会话级偏好，玩家消息之后）
//   12. 回复风格要求（必需；Style.Core，空则回退旧 ReplyStyle 字段，Author's Note 位置）
//   13. 输出格式要求（必需；Style.EnableCoT 时的思维链指导）
//   14. lore tail 风格指令区（Author's Note 位置，玩家消息之后）
//
// 裁剪优先级与位置对齐：超预算时优先裁尾部可选分区，保住稳定前缀。
// lore 已经 LoreResolver 按 lore_budget 裁剪，此处作为可选分区接入。
func (b *ContextBuilder) BuildNarratorMessage(
	state *world.WorldState,
	lore *world.LoreResult,
	guidance *RuleGuidance,
	gameContext string,
	memoryBlock string,
	dialogueBlock string,
	playerMessage string,
	lengthHint string,
	personaBlock string,
) string {
	var sections []contextSection

	// 必需：规则指导（整局稳定，放最前）
	if guidance != nil {
		sections = append(sections, contextSection{
			priority: 1, name: "guidance",
			content: guidance.String(), required: true,
		})
	}

	// 必需：世界基调（恒定稳定，紧跟 guidance 保前缀缓存）
	if state != nil && state.Style != nil && strings.TrimSpace(state.Style.Tone) != "" {
		sections = append(sections, contextSection{
			priority: 1, name: "style_tone",
			content: "【世界基调】\n" + state.Style.Tone, required: true,
		})
	}

	// 可选：游戏上下文（角色卡/规则集，基本稳定）
	if gameContext != "" {
		sections = append(sections, contextSection{
			priority: 3, name: "game_context",
			content: gameContext,
		})
	}

	// 可选：lore front（世界观区，状态摘要之前）
	if lore != nil && len(lore.Front) > 0 {
		sections = append(sections, contextSection{
			priority: 2, name: "lore_front",
			content: formatLoreHits("【世界设定】", lore.Front),
		})
	}

	// 可选：记忆检索块
	if memoryBlock != "" {
		sections = append(sections, contextSection{
			priority: 4, name: "memory",
			content: memoryBlock,
		})
	}

	// 必需：运行态摘要（含锁定事实，每轮变化）
	if state != nil {
		sections = append(sections, contextSection{
			priority: 1, name: "state_summary",
			content: buildGameStateSummary(state, lore), required: true,
		})
	}

	// 可选：场景计划
	if state != nil && state.ScenePlan != "" {
		sections = append(sections, contextSection{
			priority: 5, name: "scene_plan",
			content: "【场景计划】\n" + state.ScenePlan + "\n",
		})
	}

	// 可选：近期对话窗口（短记忆，承接上文；每轮追加，靠后放置）
	if dialogueBlock != "" {
		sections = append(sections, contextSection{
			priority: 5, name: "recent_turns",
			content: dialogueBlock,
		})
	}

	// 必需：玩家人设（玩家消息之前，紧邻其指代的"玩家"）
	if personaBlock != "" {
		sections = append(sections, contextSection{
			priority: 1, name: "persona",
			content: personaBlock, required: true,
		})
	}

	// 必需：玩家消息（每轮必变，放最后）
	sections = append(sections, contextSection{
		priority: 1, name: "player",
		content: "\n玩家: " + playerMessage, required: true,
	})

	// 必需：回复长度要求（会话级偏好，玩家消息之后）
	if lengthHint != "" {
		sections = append(sections, contextSection{
			priority: 1, name: "length_hint",
			content: lengthHint, required: true,
		})
	}

	// 必需：回复风格要求（Style.Core，空则回退旧 ReplyStyle 字段，Author's Note 位置）
	if state != nil {
		if core := strings.TrimSpace(state.EffectiveStyleCore()); core != "" {
			sections = append(sections, contextSection{
				priority: 1, name: "reply_style",
				content: "【回复风格要求】\n" + core, required: true,
			})
		}
	}

	// 必需：输出格式要求（思维链指导，回复风格之后；自定义 CoTGuide 覆盖内置默认）
	if state != nil && state.Style != nil && state.Style.EnableCoT {
		sections = append(sections, contextSection{
			priority: 1, name: "cot_guide",
			content: CoTGuideFor(state.Style.CoTGuide), required: true,
		})
	}

	// 可选：lore tail（风格指令区，玩家消息之后，Author's Note 位置）
	if lore != nil && len(lore.Tail) > 0 {
		sections = append(sections, contextSection{
			priority: 6, name: "lore_tail",
			content: formatLoreHits("【补充设定】", lore.Tail),
		})
	}

	return b.assemble(sections)
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

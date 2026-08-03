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

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// DefaultContextBudget 默认上下文预算（字符数，约 4000 tokens）。
const DefaultContextBudget = 6000

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
// 分区优先级（必需分区始终保留，可选分区超预算时从低到高裁剪）：
//   1. 玩家消息（必需）
//   2. 游戏运行态摘要 + 锁定事实（必需）
//   3. 规则叙事指导（必需）
//   4. 场景计划 ScenePlan（可选）
//   5. 游戏上下文：角色卡/骰点/规则集（可选）
//   6. 记忆检索块（可选，P3 接入）
func (b *ContextBuilder) BuildNarratorMessage(
	state *world.WorldState,
	guidance *RuleGuidance,
	gameContext string,
	memoryBlock string,
	playerMessage string,
) string {
	var sections []contextSection

	// 必需：运行态摘要（含锁定事实）
	if state != nil {
		sections = append(sections, contextSection{
			priority: 2, name: "state_summary",
			content: buildGameStateSummary(state), required: true,
		})
	}

	// 必需：规则指导
	if guidance != nil {
		sections = append(sections, contextSection{
			priority: 3, name: "guidance",
			content: guidance.String(), required: true,
		})
	}

	// 可选：场景计划
	if state != nil && state.ScenePlan != "" {
		sections = append(sections, contextSection{
			priority: 4, name: "scene_plan",
			content: "【场景计划】\n" + state.ScenePlan + "\n",
		})
	}

	// 可选：游戏上下文
	if gameContext != "" {
		sections = append(sections, contextSection{
			priority: 5, name: "game_context",
			content: gameContext,
		})
	}

	// 可选：记忆检索块（P3）
	if memoryBlock != "" {
		sections = append(sections, contextSection{
			priority: 6, name: "memory",
			content: memoryBlock,
		})
	}

	// 必需：玩家消息
	sections = append(sections, contextSection{
		priority: 1, name: "player",
		content: "\n玩家: " + playerMessage, required: true,
	})

	return b.assemble(sections)
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

// Package agent - 规则指导器（RuleGuidance）。
//
// 每轮叙事指导的规则版：从 WorldState 的规则化指标确定性推导
// 叙事指导（基调/节奏/关注点/约束），零 LLM 调用、完全可测试。
// 源自旧 Director.fallbackDirective 的逻辑转正（设计文档 8.2 第 5 条）。
// 现作为 AIGuidance 的降级兜底（AI 未装配/超时/失败/配置禁用时使用）。
package agent

import (
	"fmt"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// 叙事规则阈值（规则指导与 Director 降级指令共用一份，避免两处拷贝产生漂移）。
const (
	tensionBreathThreshold = 80 // 高于此值：给玩家喘息空间，避免继续施压
	tensionHighThreshold   = 70 // 高于此值：基调转"紧张"、节奏转 fast
	tensionLowThreshold    = 20 // 低于此值：引入新的紧迫感/威胁
	chaosHighThreshold     = 60 // 高于此值：基调转"混乱"、节奏转 fast
	chaosCollapseThreshold = 70 // 高于此值：局势接近失控，收束线索
	agencyLowThreshold     = 30 // 低于此值：玩家掌控感不足，补选项/线索
	stagnationRounds       = 5  // 停滞检测：同场景超过该轮数且无进展
)

// RuleGuidance 规则化叙事指导（确定性生成）。
type RuleGuidance struct {
	Tone        string   // 叙事基调
	Pacing      string   // 节奏: slow / medium / fast
	Focus       []string // 本轮关注建议
	Constraints []string // 叙事约束
}

// BuildGuidance 从 WorldState 指标确定性生成叙事指导。
func BuildGuidance(state *world.WorldState) *RuleGuidance {
	g := &RuleGuidance{Tone: "正常", Pacing: "medium"}
	m := state.Metrics

	// 基调
	switch {
	case m.TensionLevel > tensionHighThreshold:
		g.Tone = "紧张"
	case m.ChaosLevel > chaosHighThreshold:
		g.Tone = "混乱"
	case m.TensionLevel < tensionLowThreshold:
		g.Tone = "平静"
	}

	// 节奏
	switch {
	case m.TensionLevel > tensionHighThreshold || m.ChaosLevel > chaosHighThreshold:
		g.Pacing = "fast"
	case m.TensionLevel < tensionLowThreshold && m.ObjectiveProgress < 30:
		g.Pacing = "slow"
	}

	// 张力调节
	if m.TensionLevel > tensionBreathThreshold {
		g.Constraints = append(g.Constraints, "当前张力过高，给予玩家喘息空间，避免继续施压")
	}
	if m.TensionLevel < tensionLowThreshold {
		g.Focus = append(g.Focus, "当前张力过低，引入新的紧迫感或威胁")
	}

	// 混乱调节
	if m.ChaosLevel > chaosCollapseThreshold {
		g.Focus = append(g.Focus, "局势接近失控，帮助玩家收束线索、明确下一步可选行动")
	}

	// 掌控权调节
	if m.PlayerAgency < agencyLowThreshold {
		g.Focus = append(g.Focus, "玩家掌控感不足，提供更多选择和可探索的线索")
	}

	// 目标推进
	if m.ObjectiveProgress >= 100 {
		g.Focus = append(g.Focus, "当前节点目标已全部完成，可引导玩家推进到下一剧情节点")
	}

	// 停滞检测：同一场景轮次过多且目标无进展
	if state.RoundCount-state.PlanRound > stagnationRounds && m.ObjectiveProgress < 50 && m.TensionLevel < 40 {
		g.Focus = append(g.Focus, "玩家可能陷入停滞，主动提供线索提示或 NPC 引导")
	}

	return g
}

// String 渲染为注入 Narrator 的文本块。
func (g *RuleGuidance) String() string {
	var sb strings.Builder
	sb.WriteString("【叙事指导（请遵循）】\n")
	sb.WriteString(fmt.Sprintf("基调: %s | 节奏: %s\n", g.Tone, g.Pacing))
	for _, f := range g.Focus {
		sb.WriteString(fmt.Sprintf("关注: %s\n", f))
	}
	for _, c := range g.Constraints {
		sb.WriteString(fmt.Sprintf("约束: %s\n", c))
	}
	return sb.String()
}

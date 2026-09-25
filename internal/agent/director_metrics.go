// Package agent - 规则化指标评估器。
//
// 从 WorldState 确定性计算四个核心指标：
//   - TensionLevel:      场景激烈程度（活跃威胁、危险等级、未发现线索）
//   - ChaosLevel:        局势失控距离（敌对NPC、偏离主线）
//   - PlayerAgency:      玩家掌控权（待选项、目标完成度）
//   - ObjectiveProgress: 目标推进程度（当前节点目标完成比例）
//
// 这些指标是确定性的：相同 WorldState 必然得到相同分数。
package agent

import (
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// MetricsEvaluator 规则化指标评估器。
type MetricsEvaluator struct{}

// NewMetricsEvaluator 创建指标评估器。
func NewMetricsEvaluator() *MetricsEvaluator {
	return &MetricsEvaluator{}
}

// Evaluate 从 WorldState 计算四个指标。
func (m *MetricsEvaluator) Evaluate(state *world.WorldState) world.Metrics {
	return world.Metrics{
		TensionLevel:      m.calcTension(state),
		ChaosLevel:        m.calcChaos(state),
		PlayerAgency:      m.calcAgency(state),
		ObjectiveProgress: m.calcProgress(state),
	}
}

// calcTension 计算场景张力（0-100）。
// 因素：活跃威胁数 + 未发现线索数 + 场景危险等级
func (m *MetricsEvaluator) calcTension(state *world.WorldState) int {
	score := 0

	// 活跃威胁：每个敌对NPC +20，每个未触发遭遇 +15
	threats := state.ActiveThreatCount()
	score += threats * 20

	// 已触发但未解决的事件代表"正在发生的威胁"，是叙事可反馈张力的抓手
	ongoing := 0
	for _, ev := range state.EventQueue {
		if ev.Triggered && !ev.Resolved {
			ongoing++
		}
	}
	score += ongoing * 5

	// 未发现的线索暗示潜在危险
	undiscovered := state.UndiscoveredCount()
	score += undiscovered * 3

	// 场景危险等级
	switch state.Scene.DangerLevel {
	case "安全":
		score += 0
	case "紧张":
		score += 15
	case "危险":
		score += 30
	case "致命":
		score += 50
	}

	return clampInt(score, 0, 100)
}

// calcChaos 计算局势混乱度（0-100）。
// 因素：敌对NPC比例 + 偏离主线程度（未完成目标比例）+ 已触发未解决事件
func (m *MetricsEvaluator) calcChaos(state *world.WorldState) int {
	score := 0

	// 敌对 NPC 比例
	totalNPCs := len(state.Characters)
	hostileNPCs := 0
	for _, npc := range state.Characters {
		if npc.Disposition == "hostile" && npc.Alive {
			hostileNPCs++
		}
	}
	if totalNPCs > 0 {
		score += (hostileNPCs * 100) / totalNPCs / 2 // 最多贡献 50
	}

	// 偏离主线：未完成目标越多，混乱越高
	totalObj := len(state.Quests)
	completedObj := state.CompletedQuestCount()
	if totalObj > 0 {
		incompleteRatio := (totalObj - completedObj) * 100 / totalObj
		score += incompleteRatio / 4 // 最多贡献 25
	}

	// 已触发但未解决的事件（已解决的不再累积，修复旧版混乱度单调递增问题）
	triggeredUnresolved := 0
	for _, ev := range state.EventQueue {
		if ev.Triggered && !ev.Resolved {
			triggeredUnresolved++
		}
	}
	score += triggeredUnresolved * 10

	// 停滞压力：回合数过多且无进展会累积混乱感。
	// 该分项设上限（最多 +20），否则长团中 chaos 单调贴顶 100，
	// 会持续压低掌控权（calcAgency 有 -chaos/4），使"喘息/收束"规则长期空转。
	if state.RoundCount > 10 {
		stagnation := (state.RoundCount - 10) * 2
		if stagnation > 20 {
			stagnation = 20
		}
		score += stagnation
	}

	return clampInt(score, 0, 100)
}

// calcAgency 计算玩家掌控权（0-100）。
// 因素：待选项数量 + 目标完成度 + 未发现线索（可探索）
func (m *MetricsEvaluator) calcAgency(state *world.WorldState) int {
	score := 50 // 基础分

	// 待触发事件代表可选项
	activeEvents := 0
	for _, ev := range state.EventQueue {
		if !ev.Triggered {
			activeEvents++
		}
	}
	score += activeEvents * 5

	// 目标完成增加掌控感
	totalObj := len(state.Quests)
	if totalObj > 0 {
		completedObj := state.CompletedQuestCount()
		score += (completedObj * 20) / totalObj
	}

	// 未发现线索代表可探索空间
	undiscovered := state.UndiscoveredCount()
	score += undiscovered * 2

	// 混乱度高降低掌控权
	chaos := m.calcChaos(state)
	score -= chaos / 4

	return clampInt(score, 0, 100)
}

// calcProgress 计算目标推进程度（0-100）。
func (m *MetricsEvaluator) calcProgress(state *world.WorldState) int {
	totalObj := len(state.Quests)
	if totalObj == 0 {
		return 0
	}
	completedObj := state.CompletedQuestCount()
	return (completedObj * 100) / totalObj
}

// clampInt 将 v 限制在 [min, max]。
func clampInt(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

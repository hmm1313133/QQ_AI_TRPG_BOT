// Package agent - Director 规则化指标评估器。
//
// 从 GameState + trpg.Service 确定性计算四个核心指标：
//   - TensionLevel:      场景激烈程度（活跃威胁、SAN下降、检定失败）
//   - ChaosLevel:        局势失控距离（敌对NPC、偏离主线）
//   - PlayerAgency:      玩家掌控权（待选项、目标完成度）
//   - ObjectiveProgress: 目标推进程度（当前节点目标完成比例）
//
// 这些指标是确定性的：相同 GameState 必然得到相同分数，
// 保证 Director 决策的稳定性。
package agent

import (
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"
)

// MetricsEvaluator 规则化指标评估器。
type MetricsEvaluator struct {
	svc *trpg.Service
}

// NewMetricsEvaluator 创建指标评估器。
func NewMetricsEvaluator(svc *trpg.Service) *MetricsEvaluator {
	return &MetricsEvaluator{svc: svc}
}

// Evaluate 从 GameState 和会话信息计算四个指标。
// sessionID 用于读取角色卡状态（SAN/HP 等）。
func (m *MetricsEvaluator) Evaluate(state *GameState, sessionID string) GameMetrics {
	return GameMetrics{
		TensionLevel:      m.calcTension(state, sessionID),
		ChaosLevel:        m.calcChaos(state),
		PlayerAgency:      m.calcAgency(state),
		ObjectiveProgress: m.calcProgress(state),
	}
}

// calcTension 计算场景张力（0-100）。
// 因素：活跃威胁数 + SAN下降幅度 + 未发现线索数
func (m *MetricsEvaluator) calcTension(state *GameState, sessionID string) int {
	score := 0

	// 活跃威胁：每个敌对NPC +20，每个未触发遭遇 +15
	threats := state.ActiveThreatCount()
	score += threats * 20

	// SAN 值下降幅度（从角色卡读取）
	if m.svc != nil {
		for _, userID := range m.getSessionUsers(sessionID) {
			if card := m.svc.GetActiveCharacter(sessionID, userID); card != nil {
				if san, ok := card.Status["SAN"]; ok {
					// SAN 低于 60 开始增加张力
					if san < 60 {
						score += (60 - san) / 2
					}
					// SAN 低于 30 大幅增加
					if san < 30 {
						score += 20
					}
				}
				// HP 低也增加张力
				if hp, ok := card.Status["HP"]; ok && hp < 5 {
					score += 15
				}
			}
		}
	}

	// 未发现的线索暗示潜在危险
	undiscovered := state.UndiscoveredCount()
	score += undiscovered * 3

	// 场景危险等级
	switch state.CurrentScene.DangerLevel {
	case "安全":
		score += 0
	case "紧张":
		score += 15
	case "危险":
		score += 30
	case "致命":
		score += 50
	}

	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	return score
}

// calcChaos 计算局势混乱度（0-100）。
// 因素：敌对NPC比例 + 偏离主线程度（未完成目标比例）
func (m *MetricsEvaluator) calcChaos(state *GameState) int {
	score := 0

	// 敌对 NPC 比例
	totalNPCs := len(state.NPCStates)
	hostileNPCs := 0
	for _, npc := range state.NPCStates {
		if npc.Disposition == "hostile" {
			hostileNPCs++
		}
	}
	if totalNPCs > 0 {
		score += (hostileNPCs * 100) / totalNPCs / 2 // 最多贡献 50
	}

	// 偏离主线：未完成目标越多，混乱越高
	totalObj := len(state.Objectives)
	completedObj := state.CompletedObjectiveCount()
	if totalObj > 0 {
		incompleteRatio := (totalObj - completedObj) * 100 / totalObj
		score += incompleteRatio / 4 // 最多贡献 25
	}

	// 已触发但未解决的事件
	triggeredUnresolved := 0
	for _, ev := range state.PendingEvents {
		if ev.Triggered {
			triggeredUnresolved++
		}
	}
	score += triggeredUnresolved * 10

	// Round 过多且无进展增加混乱
	if state.RoundCount > 10 {
		score += (state.RoundCount - 10) * 2
	}

	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	return score
}

// calcAgency 计算玩家掌控权（0-100）。
// 因素：待选项数量 + 目标完成度 + 未发现线索（可探索）
func (m *MetricsEvaluator) calcAgency(state *GameState) int {
	score := 50 // 基础分

	// 待触发事件代表可选项
	activeEvents := 0
	for _, ev := range state.PendingEvents {
		if !ev.Triggered {
			activeEvents++
		}
	}
	score += activeEvents * 5

	// 目标完成增加掌控感
	totalObj := len(state.Objectives)
	if totalObj > 0 {
		completedObj := state.CompletedObjectiveCount()
		score += (completedObj * 20) / totalObj
	}

	// 未发现线索代表可探索空间
	undiscovered := state.UndiscoveredCount()
	score += undiscovered * 2

	// 混乱度高降低掌控权
	chaos := m.calcChaos(state)
	score -= chaos / 4

	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}
	return score
}

// calcProgress 计算目标推进程度（0-100）。
// 因素：当前节点目标完成比例
func (m *MetricsEvaluator) calcProgress(state *GameState) int {
	totalObj := len(state.Objectives)
	if totalObj == 0 {
		return 0
	}
	completedObj := state.CompletedObjectiveCount()
	return (completedObj * 100) / totalObj
}

// getSessionUsers 获取会话中的用户列表。
// 从 trpg.Service 的 Session.Characters 获取已绑定角色的用户。
func (m *MetricsEvaluator) getSessionUsers(sessionID string) []string {
	if m.svc == nil {
		return nil
	}
	session := m.svc.GetSession(sessionID)
	if session == nil {
		return nil
	}
	users := make([]string, 0, len(session.Characters))
	for userID := range session.Characters {
		users = append(users, userID)
	}
	return users
}

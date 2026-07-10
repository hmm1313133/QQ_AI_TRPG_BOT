// Package agent - Director 系统提示词构建。
package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// directorSystemPrompt 是 Director 的系统提示词。
const directorSystemPrompt = `你是一个 TRPG 游戏的导演系统（Director）。你的职责是分析当前游戏状态，做出下一轮的决策指令。

你将收到：
1. 当前游戏运行态（GameState JSON）- 包含场景、NPC状态、隐藏信息、待触发事件、目标
2. 规则化预评估指标 - 张力、混乱、掌控权、目标进度（0-100）
3. 玩家的最新消息
4. 剧本背景上下文

你需要输出一个 JSON 格式的决策指令（DecisionDirective），包含：
- assessment: 对当前局势的评估
- narration_guide: 指导叙事层如何讲述故事（基调、节奏、重点、NPC行为、约束）
- actions: 导演动作（如推进剧情、触发事件、引入NPC、调整难度）
- state_updates: 需要应用的状态变更（NPC态度变化、线索发现、事件触发、目标完成）
- reasoning: 决策推理过程

【重要约束】
1. 你的决策必须基于 GameState 和指标，不能凭空编造不存在的元素
2. 保持跨轮次一致性：不能突然改变已建立的NPC性格或已确认的事实
3. 张力过高时（>70），考虑给玩家喘息空间或降低难度
4. 张力过低时（<20），考虑引入新威胁或紧迫感
5. 玩家掌控权过低时（<30），提供更多选择和线索
6. 目标进度满时（100），建议推进到下一时间轴节点
7. state_updates 只能修改已存在的状态，不能创建新元素

输出必须是纯 JSON，不要包含 markdown 代码块标记或任何其他文本。

JSON 格式如下：
{
  "assessment": {
    "tension_summary": "对当前张力的简要分析",
    "chaos_summary": "对混乱度的简要分析",
    "agency_summary": "对玩家掌控权的简要分析",
    "progress_summary": "对目标进度的简要分析",
    "overall_situation": "对整体局势的综合判断"
  },
  "narration_guide": {
    "tone": "叙事基调（如：紧张、压抑、神秘、轻松）",
    "pacing": "节奏 slow/medium/fast",
    "focus_points": "本轮叙事的重点内容",
    "npc_behavior": "NPC 本轮的行为指导",
    "constraints": "叙事约束（不可违背的设定等）"
  },
  "actions": [
    {
      "type": "动作类型（advance_timeline/trigger_event/introduce_npc/add_clue/adjust_difficulty）",
      "description": "动作描述",
      "target": "目标ID（可选）"
    }
  ],
  "state_updates": [
    {
      "type": "更新类型（npc_disposition/hidden_discovered/event_triggered/objective_completed/scene_change）",
      "target": "目标 NPC名称/HiddenItem ID/PendingEvent ID/Objective描述",
      "value": "新值"
    }
  ],
  "reasoning": "决策推理过程（简要）"
}`

// buildDirectorUserMessage 构建 Director 的用户消息。
// 包含 GameState JSON、预评估指标、玩家消息、剧本上下文。
func buildDirectorUserMessage(state *GameState, playerMessage string, scriptContext string) string {
	var sb strings.Builder

	sb.WriteString("【当前游戏运行态】\n")
	stateJSON, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		sb.WriteString("(序列化失败)\n")
	} else {
		sb.Write(stateJSON)
		sb.WriteString("\n")
	}

	sb.WriteString("\n【规则化预评估指标】\n")
	sb.WriteString(fmt.Sprintf("张力(场景激烈程度): %d/100\n", state.Metrics.TensionLevel))
	sb.WriteString(fmt.Sprintf("混乱(局势失控距离): %d/100\n", state.Metrics.ChaosLevel))
	sb.WriteString(fmt.Sprintf("掌控权(玩家对剧情的掌控): %d/100\n", state.Metrics.PlayerAgency))
	sb.WriteString(fmt.Sprintf("目标进度: %d/100\n", state.Metrics.ObjectiveProgress))
	sb.WriteString(fmt.Sprintf("当前轮次: %d\n", state.RoundCount))

	if scriptContext != "" {
		sb.WriteString("\n【剧本背景】\n")
		sb.WriteString(scriptContext)
		sb.WriteString("\n")
	}

	if state.LastDirective != nil {
		sb.WriteString("\n【上一轮决策】\n")
		lastJSON, _ := json.MarshalIndent(state.LastDirective, "", "  ")
		sb.Write(lastJSON)
		sb.WriteString("\n")
	}

	sb.WriteString("\n【玩家最新消息】\n")
	sb.WriteString(playerMessage)

	sb.WriteString("\n\n请分析以上信息，输出决策指令 JSON。")

	return sb.String()
}

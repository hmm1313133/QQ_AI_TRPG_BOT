// Package agent - Narrator 叙事层提示词构建。
package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// narratorSystemPromptBase 是 Narrator 的基础系统提示词。
const narratorSystemPromptBase = `你是一个经验丰富的 TRPG 游戏主持人（KP/DM），负责叙事和主持游戏。
你负责引导玩家进行桌面角色扮演游戏，包括：
1. 描述场景和氛围
2. 扮演 NPC
3. 根据玩家行动推进剧情
4. 在需要时要求玩家进行骰点判定
请保持沉浸感和趣味性，尊重玩家的选择。

你可以使用 roll_dice 工具来为玩家投掷骰子。
当需要技能检定时，主动调用 roll_dice 工具或 skill_check 工具并告知玩家结果。

【导演指令约束】
每轮你会收到导演系统（Director）的决策指令，你必须严格遵循：
1. 叙事基调和节奏必须符合导演指令的 tone 和 pacing 要求
2. 叙事重点必须围绕导演指令的 focus_points
3. NPC 行为必须符合导演指令的 npc_behavior 指导
4. 不得违背导演指令的 constraints（约束条件）
5. 如果导演指令包含 actions（如推进剧情、触发事件），在合适的时机执行

【剧本模式约束】
当加载了剧本后，你必须严格遵循剧本的剧情发展，不能随意拓展或编造剧情：
1. 只在剧本设定的时间轴节点内推进故事
2. NPC 的行为和对话必须符合剧本中描述的性格和背景
3. 场景描述应基于剧本中的场景信息，可适当丰富细节但不改变核心内容
4. 当玩家完成当前节点的关键事件后，使用 advance_timeline 工具推进剧情
5. 定期使用 save_progress 工具保存剧情进度摘要
6. 使用 get_script_context 工具查看当前剧本上下文和可推进方向
7. 使用 get_npc 工具获取 NPC 信息以准确扮演角色
8. 如果不确定剧情走向，优先使用 get_progress 和 get_script_context 查看当前状态
9. 使用 update_game_state 工具更新游戏运行态（NPC态度变化、线索发现等）`

// buildNarratorSystemPrompt 构建 Narrator 系统提示词。
// 在基础提示词上追加 GameState 摘要。
func buildNarratorSystemPrompt(state *GameState) string {
	if state == nil {
		return narratorSystemPromptBase
	}

	var sb strings.Builder
	sb.WriteString(narratorSystemPromptBase)

	sb.WriteString("\n\n【当前游戏运行态摘要】\n")
	sb.WriteString(fmt.Sprintf("当前场景: %s (%s)\n", state.CurrentScene.NodeName, state.CurrentScene.NodeID))
	if state.CurrentScene.Description != "" {
		sb.WriteString(fmt.Sprintf("场景描述: %s\n", state.CurrentScene.Description))
	}
	if state.CurrentScene.Atmosphere != "" {
		sb.WriteString(fmt.Sprintf("氛围: %s\n", state.CurrentScene.Atmosphere))
	}
	if state.CurrentScene.DangerLevel != "" {
		sb.WriteString(fmt.Sprintf("危险等级: %s\n", state.CurrentScene.DangerLevel))
	}
	if state.CurrentScene.KPNotes != "" {
		sb.WriteString(fmt.Sprintf("KP备注: %s\n", state.CurrentScene.KPNotes))
	}

	// NPC 状态
	if len(state.NPCStates) > 0 {
		sb.WriteString("\nNPC状态:\n")
		for _, npc := range state.NPCStates {
			sb.WriteString(fmt.Sprintf("  - %s (%s): %s", npc.Name, npc.Role, npc.Disposition))
			if npc.CurrentAction != "" {
				sb.WriteString(fmt.Sprintf(" - %s", npc.CurrentAction))
			}
			sb.WriteString("\n")
		}
	}

	// 目标
	if len(state.Objectives) > 0 {
		sb.WriteString("\n当前目标:\n")
		for _, obj := range state.Objectives {
			mark := "○"
			if obj.Completed {
				mark = "✓"
			}
			sb.WriteString(fmt.Sprintf("  %s %s\n", mark, obj.Description))
		}
	}

	// 未发现的隐藏信息（只显示数量，不泄露内容）
	undiscovered := state.UndiscoveredCount()
	if undiscovered > 0 {
		sb.WriteString(fmt.Sprintf("\n未发现的线索: %d 条\n", undiscovered))
	}

	// 待触发事件
	activeEvents := 0
	for _, ev := range state.PendingEvents {
		if !ev.Triggered {
			activeEvents++
		}
	}
	if activeEvents > 0 {
		sb.WriteString(fmt.Sprintf("待触发事件: %d 个\n", activeEvents))
	}

	// 指标
	sb.WriteString(fmt.Sprintf("\n游戏指标: 张力%d 混乱%d 掌控权%d 目标进度%d 轮次%d\n",
		state.Metrics.TensionLevel, state.Metrics.ChaosLevel,
		state.Metrics.PlayerAgency, state.Metrics.ObjectiveProgress,
		state.RoundCount))

	// 故事背景
	if state.StoryContext != "" {
		sb.WriteString(fmt.Sprintf("\n【故事背景】\n%s\n", state.StoryContext))
	}

	return sb.String()
}

// buildNarratorUserMessage 构建 Narrator 用户消息。
// 包含导演指令 + 游戏上下文 + 玩家消息。
func buildNarratorUserMessage(
	directive *DecisionDirective,
	gameContext string,
	playerMessage string,
) string {
	var sb strings.Builder

	// 注入导演指令
	if directive != nil {
		sb.WriteString("【导演指令】\n")
		directiveJSON, err := json.MarshalIndent(directive, "", "  ")
		if err == nil {
			sb.Write(directiveJSON)
			sb.WriteString("\n")
		}

		sb.WriteString("\n【导演指令摘要】\n")
		sb.WriteString(fmt.Sprintf("叙事基调: %s\n", directive.NarrationGuide.Tone))
		sb.WriteString(fmt.Sprintf("节奏: %s\n", directive.NarrationGuide.Pacing))
		sb.WriteString(fmt.Sprintf("本轮重点: %s\n", directive.NarrationGuide.FocusPoints))
		sb.WriteString(fmt.Sprintf("NPC行为指导: %s\n", directive.NarrationGuide.NPCBehavior))
		if directive.NarrationGuide.Constraints != "" {
			sb.WriteString(fmt.Sprintf("约束: %s\n", directive.NarrationGuide.Constraints))
		}

		if len(directive.Actions) > 0 {
			sb.WriteString("\n需要执行的动作:\n")
			for _, action := range directive.Actions {
				sb.WriteString(fmt.Sprintf("  - [%s] %s\n", action.Type, action.Description))
			}
		}

		sb.WriteString("\n请严格遵循以上导演指令进行叙事。\n")
	}

	// 游戏上下文
	if gameContext != "" {
		sb.WriteString("\n")
		sb.WriteString(gameContext)
		sb.WriteString("\n")
	}

	// 玩家消息
	sb.WriteString(fmt.Sprintf("\n玩家: %s", playerMessage))

	return sb.String()
}

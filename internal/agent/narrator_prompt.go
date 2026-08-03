// Package agent - Narrator 叙事层提示词构建。
package agent

import (
	"fmt"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
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
// 在基础提示词上追加 WorldState 摘要。
func buildNarratorSystemPrompt(state *world.WorldState) string {
	if state == nil {
		return narratorSystemPromptBase
	}
	return narratorSystemPromptBase + "\n\n" + buildGameStateSummary(state)
}

// buildGameStateSummary 构建当前游戏运行态摘要文本。
// 每轮通过用户消息注入 Narrator，使其感知微观运行态
// （当前场景、NPC 态度、目标、隐藏信息与待触发事件）。
func buildGameStateSummary(state *world.WorldState) string {
	var sb strings.Builder
	sb.WriteString("【当前游戏运行态摘要】\n")
	sb.WriteString(fmt.Sprintf("当前场景: %s (%s)\n", state.Scene.NodeName, state.Scene.NodeID))
	if state.Scene.Description != "" {
		sb.WriteString(fmt.Sprintf("场景描述: %s\n", state.Scene.Description))
	}
	if state.Scene.Atmosphere != "" {
		sb.WriteString(fmt.Sprintf("氛围: %s\n", state.Scene.Atmosphere))
	}
	if state.Scene.DangerLevel != "" {
		sb.WriteString(fmt.Sprintf("危险等级: %s\n", state.Scene.DangerLevel))
	}
	if state.Scene.KPNotes != "" {
		sb.WriteString(fmt.Sprintf("KP备注: %s\n", state.Scene.KPNotes))
	}

	// NPC 状态（含情绪与性格特质，供拟人化扮演）
	if len(state.Characters) > 0 {
		sb.WriteString("\nNPC状态:\n")
		for _, npc := range state.Characters {
			sb.WriteString(fmt.Sprintf("  - %s (%s): %s", npc.Name, npc.Role, npc.Disposition))
			if npc.CurrentAction != "" {
				sb.WriteString(fmt.Sprintf(" - %s", npc.CurrentAction))
			}
			if len(npc.Traits) > 0 {
				sb.WriteString(fmt.Sprintf(" [性格: %s]", strings.Join(npc.Traits, "/")))
			}
			if npc.Mood.Valence != 0 || npc.Mood.Arousal != 0 || len(npc.Mood.Tags) > 0 {
				moodDesc := fmt.Sprintf(" [情绪: 愉悦%d/激活%d", npc.Mood.Valence, npc.Mood.Arousal)
				if len(npc.Mood.Tags) > 0 {
					moodDesc += " " + strings.Join(npc.Mood.Tags, ",")
				}
				sb.WriteString(moodDesc + "]")
			}
			sb.WriteString("\n")
		}
	}

	// 目标
	if len(state.Quests) > 0 {
		sb.WriteString("\n当前目标:\n")
		for _, obj := range state.Quests {
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

	// 状态锁定（不可违背的硬事实，优先告知）
	if len(state.Locks) > 0 {
		sb.WriteString("\n【锁定事实（不可违背）】\n")
		for _, l := range state.Locks {
			sb.WriteString(fmt.Sprintf("  - %s（%s）\n", l.Key, l.Reason))
		}
	}

	// 待触发事件
	activeEvents := 0
	for _, ev := range state.EventQueue {
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

	// 故事背景（不可变设定）+ 战役摘要（滚动）
	if state.Background != "" {
		sb.WriteString(fmt.Sprintf("\n【故事背景】\n%s\n", state.Background))
	}
	if state.CampaignSummary != "" {
		sb.WriteString(fmt.Sprintf("\n【剧情摘要】\n%s\n", state.CampaignSummary))
	}

	return sb.String()
}

// 情绪模型（设计文档 6.2）：情绪随世界时间衰减，性格特质调制衰减速率。
//
// 核心规则：
//   - 情绪向 0 收敛：每世界日 valence/arousal 乘以 0.7
//   - 性格调制：「记仇」的 NPC 负面情绪衰减减半（原谅但不遗忘）
//   - 衰减按世界时间计算（暂停的团情绪不"跳变"）
package world

import "math"

// moodDecayPerDay 每世界日的情绪保留系数。
const moodDecayPerDay = 0.7

// minutesPerWorldDay 一世界日的分钟数。
const minutesPerWorldDay = 1440

// DecayMoods 对世界内所有 NPC 应用情绪衰减。
// 每回合调用一次，衰减量按距上次更新的世界时间差计算。
func DecayMoods(ws *WorldState) {
	now := ws.Clock.WorldTime
	for _, npc := range ws.Characters {
		if !npc.Alive {
			continue
		}
		mood := &npc.Mood
		if mood.UpdatedAt >= now {
			continue
		}
		days := float64(now-mood.UpdatedAt) / minutesPerWorldDay
		if days <= 0 {
			continue
		}

		factor := math.Pow(moodDecayPerDay, days)

		// 性格调制：记仇的 NPC 负面情绪衰减减半
		if hasTrait(npc, "记仇") && mood.Valence < 0 {
			factor = math.Pow(factor, 0.5)
		}

		mood.Valence = int(float64(mood.Valence) * factor)
		mood.Arousal = int(float64(mood.Arousal) * factor)
		// 情绪平复后清除情绪标签
		if abs(mood.Valence) < 10 && mood.Arousal < 10 {
			mood.Tags = nil
		}
		mood.UpdatedAt = now
	}
}

// hasTrait 判断 NPC 是否拥有指定性格特质。
func hasTrait(npc *CharacterState, trait string) bool {
	for _, t := range npc.Traits {
		if t == trait {
			return true
		}
	}
	return false
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

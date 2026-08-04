// 素材映射：外部素材（人物卡/剧本角色）→ 世界实体。
// InitFromScript、素材导入、创建向导共用，避免多份实现漂移（设计 §4.2）。
package world

import (
	"fmt"
	"sort"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/character"
)

// CharacterFromScript 剧本角色 → 世界角色（中立、存活的初始叙事状态）。
// 创作字段（外貌/性格/背景/技能）一并带入（设计 §11.2）。
func CharacterFromScript(ch script.ScriptCharacter) *CharacterState {
	var skills []string
	for name, v := range ch.Skills {
		if v > 0 {
			skills = append(skills, fmt.Sprintf("%s(%d)", name, v))
		} else {
			skills = append(skills, name)
		}
	}
	sort.Strings(skills)
	return &CharacterState{
		ID:            ch.ID,
		Name:          ch.Name,
		Kind:          "npc",
		Role:          ch.Role,
		Alive:         true,
		Disposition:   "neutral",
		Appearance:    ch.Appearance,
		Personality:   ch.Personality,
		Backstory:     ch.Background,
		Skills:        skills,
		Motivation:    ch.Motivation,
		Secrets:       ch.Secrets,
		DialogueStyle: ch.DialogueStyle,
		KeyDialogue:   ch.KeyDialogue,
		Notes:         ch.Notes,
	}
}

// CharacterStateFromCard 全局人物卡 → 世界角色（CardRef 关联，数值真相仍在卡）。
// 数值技能按 "技能名(数值)" 拍平为描述列表，供叙事注入。
func CharacterStateFromCard(c *character.Card) *CharacterState {
	skills := make([]string, 0, len(c.Skills))
	for name, v := range c.Skills {
		skills = append(skills, fmt.Sprintf("%s(%d)", name, v))
	}
	sort.Strings(skills)
	return &CharacterState{
		Name:        c.Name,
		Kind:        "npc",
		CardRef:     c.ID,
		Alive:       true,
		Disposition: "neutral",
		Appearance:  c.Appearance,
		Personality: c.Personality,
		Backstory:   c.Backstory,
		Skills:      skills,
	}
}

// 剧本条目化（设计文档《世界设定库与按需加载设计.md》§4.6 P4）：
// InitFromScript 时把 StoryBackground/Timeline/Characters 转成 lore 条目，
// 导入剧本后设定库自动填充。原 Background 全量字段保留不变（兼容旧读路径）。
package world

import (
	"fmt"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
)

// 剧本生成条目的 Priority 约定。
const (
	scriptLorePriorityMain      = 100 // 梗概/主题/基调（恒定）
	scriptLorePriorityCharacter = 60  // 角色
	scriptLorePriorityNode      = 50  // 时间轴节点与背景字段
)

// ScriptLoreEntries 从剧本生成 lore 条目（全部 Source=script、Enabled=true）。
// 剧本无 Background/Timeline/Characters 内容时返回空（行为与条目化之前一致）。
func ScriptLoreEntries(scr *script.Script) []LoreEntry {
	if scr == nil {
		return nil
	}
	var entries []LoreEntry
	bg := scr.Background

	// 1. 梗概 + 主题 + 基调 → 1 条恒定条目（整局有效，豁免预算裁剪）
	if bg.Synopsis != "" || bg.MainTheme != "" || bg.Tone != "" {
		var sb strings.Builder
		if bg.Synopsis != "" {
			sb.WriteString("故事梗概：" + bg.Synopsis)
		}
		if bg.MainTheme != "" {
			sb.WriteString("\n主题：" + bg.MainTheme)
		}
		if bg.Tone != "" {
			sb.WriteString("\n叙事基调：" + bg.Tone)
		}
		entries = append(entries, LoreEntry{
			ID:       "lor_script_bg_main",
			Title:    "剧本主线（梗概/主题/基调）",
			Category: LoreCategoryBackground,
			Constant: true,
			Position: LorePositionFront,
			Priority: scriptLorePriorityMain,
			Enabled:  true,
			Content:  sb.String(),
			Source:   LoreSourceScript,
		})
	}

	// 2. Setting/Era/Location/Atmosphere/Backstory 各自成条目（空字段跳过）
	fields := []struct {
		id      string
		title   string
		value   string
		selfKey bool // true = 字段值本身作 key（Location）；false = 字段名+值前 2 个词
	}{
		{"lor_script_bg_setting", "世界观概述", bg.Setting, false},
		{"lor_script_bg_era", "时代背景", bg.Era, false},
		{"lor_script_bg_location", "主要地点", bg.Location, true},
		{"lor_script_bg_atmosphere", "氛围", bg.Atmosphere, false},
		{"lor_script_bg_backstory", "历史背景", bg.Backstory, false},
	}
	for _, f := range fields {
		if strings.TrimSpace(f.value) == "" {
			continue
		}
		keys := scriptFieldKeys(f.title, f.value, f.selfKey)
		entries = append(entries, LoreEntry{
			ID:       f.id,
			Title:    f.title,
			Category: LoreCategoryBackground,
			Keys:     keys,
			Position: LorePositionFront,
			Priority: scriptLorePriorityNode,
			Enabled:  true,
			Content:  f.value,
			Source:   LoreSourceScript,
		})
	}

	// 3. 时间轴节点 → background 条目，Keys = 节点名 + 关联 NPC 名
	for _, node := range scr.Timeline {
		if strings.TrimSpace(node.Name) == "" || strings.TrimSpace(node.Description) == "" {
			continue
		}
		keys := []string{node.Name}
		for _, npc := range node.NPCs {
			if npc = strings.TrimSpace(npc); npc != "" {
				keys = append(keys, npc)
			}
		}
		id := node.ID
		if id == "" {
			id = fmt.Sprintf("order_%d", node.Order)
		}
		entries = append(entries, LoreEntry{
			ID:       "lor_script_node_" + id,
			Title:    node.Name,
			Category: LoreCategoryBackground,
			Keys:     keys,
			Position: LorePositionFront,
			Priority: scriptLorePriorityNode,
			Enabled:  true,
			Content:  node.Description,
			Source:   LoreSourceScript,
		})
	}

	// 4. 角色 → character 条目，Keys = 角色名（剧本角色无别名字段）
	for _, ch := range scr.Characters {
		if strings.TrimSpace(ch.Name) == "" {
			continue
		}
		var sb strings.Builder
		if ch.Personality != "" {
			sb.WriteString("性格：" + ch.Personality)
		}
		if ch.Background != "" {
			sb.WriteString("\n背景：" + ch.Background)
		}
		if ch.Motivation != "" {
			sb.WriteString("\n动机：" + ch.Motivation)
		}
		if ch.Appearance != "" {
			sb.WriteString("\n外貌：" + ch.Appearance)
		}
		if ch.Relationships != "" {
			sb.WriteString("\n关系：" + ch.Relationships)
		}
		content := sb.String()
		if content == "" {
			content = ch.Name // 无任何描述时退化为名字占位，保证条目有效
		}
		id := ch.ID
		if id == "" {
			id = ch.Name
		}
		entries = append(entries, LoreEntry{
			ID:       "lor_script_char_" + id,
			Title:    ch.Name,
			Category: LoreCategoryCharacter,
			Keys:     []string{ch.Name},
			Position: LorePositionFront,
			Priority: scriptLorePriorityCharacter,
			Enabled:  true,
			Content:  content,
			Source:   LoreSourceScript,
		})
	}
	return entries
}

// scriptFieldKeys 从背景字段提取触发关键词（简单策略，无需分词）：
// selfKey=true（Location）时字段值本身作 key；否则取字段名 + 值的前 2 个词。
// 关键词不足 2 字丢弃（避免单字误命中，见设计文档 §4.8 风险 1），超长截断。
func scriptFieldKeys(fieldName, value string, selfKey bool) []string {
	const maxKeyRunes = 12
	clean := func(s string) string {
		s = strings.TrimSpace(s)
		s = strings.Trim(s, "，。,.、；;：:！!？?\"'“”‘’（）()【】[]《》")
		if r := []rune(s); len(r) > maxKeyRunes {
			s = string(r[:maxKeyRunes])
		}
		return s
	}
	if selfKey {
		if k := clean(value); len([]rune(k)) >= 2 {
			return []string{k}
		}
		return nil
	}
	keys := []string{}
	if k := clean(fieldName); k != "" {
		keys = append(keys, k)
	}
	tokens := strings.Fields(value)
	if len(tokens) > 2 {
		tokens = tokens[:2]
	}
	for _, t := range tokens {
		if k := clean(t); len([]rune(k)) >= 2 {
			keys = append(keys, k)
		}
	}
	return keys
}

// Package agent - 长期记忆的注入块格式化与事件→记忆转换。
//
// 写入侧分两类：
//   - episode：由 SummaryService 用 LLM 生成阶段摘要写到 memory.Store；
//   - event：本文件把 WorldEvent 规则化转成记忆块（零 LLM，带重要性分级）。
//
// 读取侧：TurnEngine 按"玩家消息 + 最近决策 + 场景实体"检索 top-K，
// 由 ContextBuilder 作为可选分区注入（超预算整段裁剪）。
package agent

import (
	"fmt"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/memory"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

const (
	// memoryRecallLimit 每回合注入的相关回忆条数上限。
	memoryRecallLimit = 8
	// recentEpisodeLimit 剧情回顾分区最多列出的未归档阶段摘要条数。
	recentEpisodeLimit = 12
	// timelineArcLimit 剧情回顾分区最多列出的篇章摘要条数。
	timelineArcLimit = 6
	// npcMemoryLimit / npcMemoryPerNPC 在场 NPC 记忆块的 NPC 数与每人条数上限。
	npcMemoryLimit  = 3
	npcMemoryPerNPC = 2
	// memoryBudgetDivisor 回忆分区占上下文预算的比例（1/N）。
	memoryBudgetDivisor = 5
)

// formatMemoryRecall 把检索结果格式化为【相关回忆】块（无内容返回空串）。
func formatMemoryRecall(chunks []memory.Chunk) string {
	if len(chunks) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("【相关回忆】（按相关度挑选的过往片段；与当前情境相关时才引用，不要逐字复述）\n")
	for _, c := range chunks {
		sb.WriteString("  · " + memoryChunkLabel(c) + c.Text + "\n")
	}
	return sb.String()
}

// formatEpisodeReview 把阶段摘要格式化为【剧情回顾】块（无内容返回空串）。
func formatEpisodeReview(chunks []memory.Chunk) string {
	if len(chunks) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("【剧情回顾】（按时间先后的阶段摘要；更早的剧情以这些摘要为准，不要与近期对话重复叙述）\n")
	for _, c := range chunks {
		switch {
		case c.Level >= memory.LevelArc && c.RoundTo > 0:
			sb.WriteString(fmt.Sprintf("  · [第%d-%d轮·篇章] %s\n", c.RoundFrom, c.RoundTo, c.Text))
		case c.RoundTo > 0:
			sb.WriteString(fmt.Sprintf("  · [第%d-%d轮] %s\n", c.RoundFrom, c.RoundTo, c.Text))
		default:
			sb.WriteString("  · " + c.Text + "\n")
		}
	}
	return sb.String()
}

func memoryChunkLabel(c memory.Chunk) string {
	parts := make([]string, 0, 3)
	if c.RoundTo > 0 {
		parts = append(parts, fmt.Sprintf("第%d-%d轮", c.RoundFrom, c.RoundTo))
	}
	if c.Scene != "" {
		parts = append(parts, c.Scene)
	}
	if c.Subject != "" && c.Subject != "player" && c.Subject != c.Scene {
		parts = append(parts, c.Subject)
	}
	if len(parts) == 0 {
		return ""
	}
	return "[" + strings.Join(parts, "·") + "] "
}

// LegacySummaryChunks 把旧版 WorldState.Summaries/CampaignSummary 转成 episode 块。
// 供回合引擎与玩家侧摘要 API 共用（APPEND 幂等，重复迁移不会产生重复条目）。
func LegacySummaryChunks(ws *world.WorldState) []memory.Chunk {
	if ws == nil {
		return nil
	}
	entries := ws.SummaryEntries()
	chunks := make([]memory.Chunk, 0, len(entries))
	for _, e := range entries {
		text := strings.TrimSpace(e.Text)
		if text == "" {
			continue
		}
		created := e.CreatedAt
		if created == "" {
			created = ws.LastUpdate
		}
		chunks = append(chunks, memory.Chunk{
			WorldID:    ws.WorldID,
			Kind:       memory.KindEpisode,
			Level:      memory.LevelChunk,
			RoundFrom:  e.RoundFrom,
			RoundTo:    e.RoundTo,
			Text:       text,
			Importance: 6,
			DedupeKey:  fmt.Sprintf("legacy|%d|%d|%d", e.ID, e.RoundFrom, e.RoundTo),
			CreatedAt:  created,
		})
	}
	return chunks
}

// worldEventToMemory 把一个世界事件规则化为记忆块（importance 分级见设计：
// 死亡/锁定 9，目标完成 8，线索/事件触发 7，关系变化 6，普通决策 4）。
func worldEventToMemory(ws *world.WorldState, ev world.WorldEvent) (memory.Chunk, bool) {
	target := strings.TrimSpace(ev.Target)
	value := strings.TrimSpace(ev.Value)

	var text, subject string
	importance := 0
	switch ev.Type {
	case "decision":
		if value == "" {
			return memory.Chunk{}, false
		}
		text, subject, importance = "玩家决定："+truncate(value, 120), "player", 4
	case "npc_disposition":
		if target == "" {
			return memory.Chunk{}, false
		}
		subject = target
		switch value {
		case "dead":
			text, importance = target+" 死亡", 9
		case "hostile":
			text, importance = target+" 转为敌对", 7
		case "friendly":
			text, importance = target+" 转为友好", 6
		default:
			text, importance = fmt.Sprintf("%s 的态度变为 %s", target, value), 5
		}
	case "mood_change":
		if target == "" {
			return memory.Chunk{}, false
		}
		text, subject, importance = fmt.Sprintf("%s 情绪变化：%s", target, value), target, 5
	case "relation_change":
		// Target 是关系起点（通常是 player），Value 里的 to= 才是被记忆的 NPC。
		to := kvField(value, "to")
		if target == "" || to == "" {
			return memory.Chunk{}, false
		}
		text, subject, importance = fmt.Sprintf("%s 与 %s 的关系变化：%s", target, to, value), to, 6
	case "hidden_discovered":
		text, importance = "发现线索："+target, 7
	case "event_triggered":
		text, importance = "事件触发："+target, 7
	case "objective_completed":
		text, importance = "目标完成："+target, 8
	case "quest_advance":
		text, importance = "任务推进："+target, 7
	case "lock":
		text, importance = "事实锁定："+target, 9
	case "fact_add":
		text, importance = "事实："+target, 6
	case "fact_invalidate":
		text, importance = "事实失效："+target, 5
	case "note":
		// 归档计数与管理端审计备注不是叙事事实，不进记忆。
		if target == "archive" || strings.HasPrefix(target, "section:") || value == "" {
			return memory.Chunk{}, false
		}
		text, importance = value, 3
	default:
		return memory.Chunk{}, false
	}
	if strings.TrimSpace(text) == "" {
		return memory.Chunk{}, false
	}

	entities := make([]string, 0, 4)
	seen := make(map[string]bool, 4)
	addEntity := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		entities = append(entities, name)
	}
	addEntity(subject)
	addEntity(ev.Actor)
	addEntity(target)
	addEntity(sceneName(ws))

	return memory.Chunk{
		WorldID:    ws.WorldID,
		Kind:       memory.KindEvent,
		Level:      memory.LevelChunk,
		Subject:    subject,
		RoundFrom:  ev.Round,
		RoundTo:    ev.Round,
		Scene:      sceneName(ws),
		Entities:   entities,
		Text:       text,
		Importance: importance,
		DedupeKey:  fmt.Sprintf("%d|%s|%s|%s|%s", ev.Round, ev.Type, ev.Actor, target, truncate(value, 40)),
	}, true
}

// kvField 从 "k1=v1,k2=v2" 形式的事件 Value 中取字段（与 world 包 parser 同格式，
// 但不导出内部 helper，这里保持最小实现）。
func kvField(value, key string) string {
	for _, part := range strings.Split(value, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func sceneName(ws *world.WorldState) string {
	if ws == nil {
		return ""
	}
	return strings.TrimSpace(ws.Scene.NodeName)
}

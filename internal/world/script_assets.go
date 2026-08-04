// 剧本 → 世界实体派生映射（《世界编辑器与素材联动设计.md》§11.2）。
// TRPG 创建世界时 InitFromScript 自动带入模组的主线/地点/势力，
// 「收藏到素材库」API 复用同一组映射，避免两份实现漂移。
package world

import (
	"fmt"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
)

// StorylineFromScript 剧本时间轴 → 导演主线镜像：
// 优先取 act 类型节点，无则取关键节点，再无则全部节点；首幕 active 其余 pending。
func StorylineFromScript(scr *script.Script) *Storyline {
	if scr == nil || len(scr.Timeline) == 0 {
		return nil
	}
	pick := func(match func(*script.TimelineNode) bool) []script.TimelineNode {
		var out []script.TimelineNode
		for _, n := range scr.Timeline {
			if match(&n) {
				out = append(out, n)
			}
		}
		return out
	}
	nodes := pick(func(n *script.TimelineNode) bool { return n.Type == "act" })
	if len(nodes) == 0 {
		nodes = pick(func(n *script.TimelineNode) bool { return n.IsKeyNode })
	}
	if len(nodes) == 0 {
		nodes = scr.Timeline
	}
	sl := &Storyline{
		Title:   scr.Title,
		Premise: scr.Background.Synopsis,
	}
	if sl.Title == "" {
		sl.Title = scr.Name
	}
	for i, n := range nodes {
		status := "pending"
		if i == 0 {
			status = "active"
		}
		sl.Acts = append(sl.Acts, StoryAct{
			ID:      fmt.Sprintf("act_%d", i+1),
			Title:   n.Name,
			Summary: n.Description,
			Status:  status,
		})
	}
	return sl
}

// LocationsFromScript 剧本场景 → 世界地点（氛围/危险度/兴趣点直映）。
func LocationsFromScript(scr *script.Script) []*Location {
	if scr == nil {
		return nil
	}
	var out []*Location
	for i, sc := range scr.Scenes {
		if sc.Name == "" {
			continue
		}
		id := sc.ID
		if id == "" {
			id = fmt.Sprintf("loc_%d", i+1)
		}
		out = append(out, &Location{
			ID:         id,
			Name:       sc.Name,
			Exits:      sc.Exits,
			Atmosphere: sc.Atmosphere,
			Danger:     sc.DangerLevel,
			Points:     sc.InvestigationPoints,
		})
	}
	return out
}

// FactionsFromScript 剧本关键组织 → 世界势力（仅名称，详情由 lore 条目承载）。
func FactionsFromScript(scr *script.Script) []*Faction {
	if scr == nil {
		return nil
	}
	var out []*Faction
	for i, name := range scr.Background.KeyOrganizations {
		if name == "" {
			continue
		}
		out = append(out, &Faction{
			ID:   fmt.Sprintf("fac_%d", i+1),
			Name: name,
		})
	}
	return out
}

// WorldviewFromScript 剧本背景 → 世界观素材（收藏入素材库用）。
func WorldviewFromScript(scr *script.Script) *Worldview {
	if scr == nil {
		return nil
	}
	bg := &scr.Background
	return &Worldview{
		Setting:    bg.Setting,
		Era:        bg.Era,
		Location:   bg.Location,
		Atmosphere: bg.Atmosphere,
		Tone:       bg.Tone,
		Backstory:  bg.Backstory,
		Themes:     bg.KeyThemes,
	}
}

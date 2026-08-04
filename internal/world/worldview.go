// 世界观素材（world 类型）的 payload 结构与导入应用。
// 设计见《世界编辑器与素材联动设计.md》§11.3。
package world

import (
	"fmt"
	"strings"
)

// Worldview 世界观素材 payload：一段可复用的世界背景设定，
// 可附带设定条目（Lorebook），导入世界时一键写入背景 + 设定库。
type Worldview struct {
	Setting    string      `json:"setting,omitempty"`    // 世界观概述
	Era        string      `json:"era,omitempty"`        // 时代
	Location   string      `json:"location,omitempty"`   // 主要地点
	Atmosphere string      `json:"atmosphere,omitempty"` // 氛围
	Tone       string      `json:"tone,omitempty"`       // 叙事基调
	Backstory  string      `json:"backstory,omitempty"`  // 详细背景（长文）
	Themes     []string    `json:"themes,omitempty"`     // 主题
	Lore       []LoreEntry `json:"lore,omitempty"`       // 附带设定条目（导入时重发 ID）
}

// BuildText 把结构化字段组合为背景文本（导入世界时写入 state.Background）。
func (w *Worldview) BuildText() string {
	var sb strings.Builder
	if w.Setting != "" {
		fmt.Fprintf(&sb, "【世界观】%s\n", w.Setting)
	}
	var meta []string
	if w.Era != "" {
		meta = append(meta, "时代："+w.Era)
	}
	if w.Location != "" {
		meta = append(meta, "地点："+w.Location)
	}
	if w.Atmosphere != "" {
		meta = append(meta, "氛围："+w.Atmosphere)
	}
	if w.Tone != "" {
		meta = append(meta, "基调："+w.Tone)
	}
	if len(meta) > 0 {
		fmt.Fprintf(&sb, "【设定】%s\n", strings.Join(meta, "；"))
	}
	if len(w.Themes) > 0 {
		fmt.Fprintf(&sb, "【主题】%s\n", strings.Join(w.Themes, "、"))
	}
	if w.Backstory != "" {
		sb.WriteString(w.Backstory)
	}
	return strings.TrimSpace(sb.String())
}

// Empty 判断世界观是否没有任何实质内容。
func (w *Worldview) Empty() bool {
	return w.BuildText() == "" && len(w.Lore) == 0
}

// ApplyWorldview 把世界观素材应用到世界状态：
// 背景按 overwrite 策略写入，附带 lore 条目按标题去重追加（重发 ID 避免跨世界冲突）。
// 返回是否有变更。
func ApplyWorldview(ws *WorldState, wv *Worldview, overwrite bool) bool {
	changed := false
	text := wv.BuildText()
	if text != "" && (overwrite || strings.TrimSpace(ws.Background) == "") {
		ws.Background = text
		changed = true
	}
	existing := make(map[string]bool, len(ws.Lore))
	for _, e := range ws.Lore {
		existing[e.Title] = true
	}
	for i, e := range wv.Lore {
		if e.Content == "" || existing[e.Title] {
			continue
		}
		entry := e // 复制，避免切片别名
		entry.ID = fmt.Sprintf("lor_wv_%d_%d", len(ws.Lore), i)
		if entry.Category == "" {
			entry.Category = LoreCategoryBackground
		}
		if entry.Position == "" {
			entry.Position = LorePositionFront
		}
		if entry.Source == "" {
			entry.Source = LoreSourceManual
		}
		entry.Enabled = true
		ws.Lore = append(ws.Lore, entry)
		existing[entry.Title] = true
		changed = true
	}
	return changed
}

// WorldviewFromState 从世界状态提取世界观素材（收藏入素材库用）：
// Background → backstory，手工录入的设定条目随附。
func WorldviewFromState(ws *WorldState) *Worldview {
	wv := &Worldview{Backstory: ws.Background}
	for _, e := range ws.Lore {
		if e.Source != LoreSourceManual {
			continue
		}
		wv.Lore = append(wv.Lore, e)
	}
	return wv
}

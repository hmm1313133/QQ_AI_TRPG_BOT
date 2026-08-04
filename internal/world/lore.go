// 世界设定库（Lorebook）数据模型与兼容迁移。
//
// 设计见《世界设定库与按需加载设计.md》§4.1：
// 设定条目是"一个概念一张卡"的静态设定单元，按需检索注入，
// 替代旧版 Background 全量注入（预算缺陷）。
package world

import "strings"

// 条目分类（设计文档 §3.1 八种）。
const (
	LoreCategoryBackground = "background" // 背景
	LoreCategoryGeo        = "geo"        // 地理
	LoreCategoryFaction    = "faction"    // 势力
	LoreCategoryCharacter  = "character"  // 人物
	LoreCategoryItem       = "item"       // 物品
	LoreCategoryRule       = "rule"       // 规则
	LoreCategoryHistory    = "history"    // 历史
	LoreCategoryStyle      = "style"      // 风格
)

// 插入位置。
const (
	LorePositionFront = "front" // 前部（世界观区，状态摘要之前）
	LorePositionTail  = "tail"  // 尾部（风格指令区，玩家消息之后，对应 Author's Note 位置）
)

// 次键逻辑模式（对扫描文本判断）。
const (
	LoreSecondaryAndAny = "and_any" // 任一次键同现才命中
	LoreSecondaryAndAll = "and_all" // 全部次键同现才命中
	LoreSecondaryNotAny = "not_any" // 任一次键出现则排除
)

// 条目来源（便于筛选与回滚）。
const (
	LoreSourceManual = "manual" // 管理后台手工录入
	LoreSourceScript = "script" // 剧本导入生成（P4）
	LoreSourceSystem = "system" // 系统生成（迁移/演化回写）
)

// LegacyBackgroundEntryID 旧 Background 迁移条目的固定 ID。
const LegacyBackgroundEntryID = "lor_legacy_background"

// LoreEntry 设定条目（Lorebook 基本单元）。
type LoreEntry struct {
	ID            string   `json:"id"`                      // lor_xxx，世界内唯一
	Title         string   `json:"title"`                   // 卡片名，仅管理用
	Category      string   `json:"category"`                // background/geo/faction/character/item/rule/history/style
	Keys          []string `json:"keys"`                    // 主触发词（子串匹配，不区分大小写）
	SecondaryKeys []string `json:"secondary_keys,omitempty"` // 次键
	SecondaryMode string   `json:"secondary_mode,omitempty"` // and_any / and_all / not_any
	Constant      bool     `json:"constant"`                // 恒定注入（无视关键词，豁免预算裁剪）
	Position      string   `json:"position"`                // front（世界观区）/ tail（风格指令区）
	Priority      int      `json:"priority"`                // 0-100，预算裁剪依据
	Enabled       bool     `json:"enabled"`                 // 停用的条目不进检索
	Content       string   `json:"content"`                 // 设定正文
	Source        string   `json:"source"`                  // manual / script / system
}

// MigrateLegacyBackground 旧世界兼容迁移（设计文档 §4.1）：
// Background 非空且 Lore 为空时，把整段 Background 转为一条恒定条目，
// 行为与旧的"背景全量注入"完全等价。
// 不修改 Background 字段（其他地方可能还在读），内存中转换，下次 Save 自然落盘。
func MigrateLegacyBackground(ws *WorldState) {
	if ws == nil || len(ws.Lore) > 0 || strings.TrimSpace(ws.Background) == "" {
		return
	}
	ws.Lore = append(ws.Lore, LoreEntry{
		ID:       LegacyBackgroundEntryID,
		Title:    "世界背景（迁移）",
		Category: LoreCategoryBackground,
		Constant: true,
		Position: LorePositionFront,
		Priority: 100,
		Enabled:  true,
		Content:  ws.Background,
		Source:   LoreSourceSystem,
	})
}

// PresentNPCNames 返回当前场景"在场"NPC 名列表。
// 判定规则：存在带 Location 信息的 NPC 时，在场 = Location 与当前场景名匹配
// （忽略大小写）；所有 NPC 都无 Location（如 TRPG 剧本世界）时退化为全部 NPC
// （与旧"全量 NPC 注入"行为等价）。
// 同时是 lore 场景关联层与 NPC 状态注入共用的在场判据。
func PresentNPCNames(ws *WorldState) []string {
	if ws == nil || len(ws.Characters) == 0 {
		return nil
	}
	hasLocation := false
	for _, c := range ws.Characters {
		if c.Location != "" {
			hasLocation = true
			break
		}
	}
	names := make([]string, 0, len(ws.Characters))
	if !hasLocation {
		for name := range ws.Characters {
			names = append(names, name)
		}
		return names
	}
	scene := strings.ToLower(strings.TrimSpace(ws.Scene.NodeName))
	for name, c := range ws.Characters {
		if !c.Alive || c.Location == "" {
			continue
		}
		loc := strings.ToLower(strings.TrimSpace(c.Location))
		if scene != "" && (loc == scene || strings.Contains(scene, loc) || strings.Contains(loc, scene)) {
			names = append(names, name)
		}
	}
	return names
}

// SceneEntityNames 返回场景关联层使用的场景实体名（当前场景名 + 在场 NPC 名）。
func SceneEntityNames(ws *WorldState) []string {
	if ws == nil {
		return nil
	}
	var names []string
	if n := strings.TrimSpace(ws.Scene.NodeName); n != "" {
		names = append(names, n)
	}
	return append(names, PresentNPCNames(ws)...)
}

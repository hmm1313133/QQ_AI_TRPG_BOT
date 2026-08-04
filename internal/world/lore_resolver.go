// 设定条目检索引擎 LoreResolver（设计文档《世界设定库与按需加载设计.md》§4.2）。
//
// 纯函数、无 LLM、零依赖。召回分四层，按序合并去重：
//  1. 恒定层：Constant && Enabled，无条件入选
//  2. 关键词层：scanText 对主键做子串匹配（大小写不敏感），命中后检查次键逻辑
//  3. 场景关联层：当前场景名/在场 NPC 名与条目 Title/Keys 匹配
//  4. 递归层（可选）：已命中条目的 Content 再跑关键词层，最多 2 步，防链式爆炸
//
// 预算装配：命中条目按 Priority 降序贪心装入 budget（按字符数）；
// 恒定条目豁免裁剪；Position=front/tail 分流。
package world

import (
	"fmt"
	"sort"
	"strings"
)

// 配置默认值（config 注册表 seed 与 TurnEngine 兜底共用）。
const (
	DefaultLoreBudget     = 4000 // lore 分区预算（字符，从 context_budget 中切出）
	DefaultLoreScanRounds = 4    // 关键词扫描的最近轮数
	loreRecursionMaxSteps = 2    // 递归层最大步数
)

// LoreHit 一条命中的设定条目（附命中原因与预算记账）。
type LoreHit struct {
	Entry  LoreEntry `json:"entry"`
	Reason string    `json:"reason"` // 命中原因：恒定 / 关键词"寒鸦堡" / 场景关联: NPC 老王 / 递归自"北境"
	Chars  int       `json:"chars"`  // Content 字符数（预算记账）
}

// LoreResult 一次检索的装配结果。
type LoreResult struct {
	Front   []LoreHit `json:"front,omitempty"`   // Position=front，注入状态摘要之前
	Tail    []LoreHit `json:"tail,omitempty"`    // Position=tail，注入玩家消息之后
	Dropped []LoreHit `json:"dropped,omitempty"` // 被预算裁掉的命中（注入日志/管理端可观测性用）
}

// Resolve 检索并装配本回合应注入的设定条目。
// scanText = 本回合玩家消息 + 最近 K 轮决策 + 当前场景名与在场 NPC 名（调用方构建）。
// budget <= 0 视为不限预算；recursion 为递归层开关。
func Resolve(ws *WorldState, scanText string, budget int, recursion bool) LoreResult {
	var result LoreResult
	if ws == nil || len(ws.Lore) == 0 {
		return result
	}

	// 召回：index -> hit（按 ws.Lore 下标去重）
	matched := make(map[int]LoreHit)
	addHit := func(idx int, reason string) {
		if _, ok := matched[idx]; ok {
			return // 先到的层决定 Reason（恒定 > 关键词 > 场景 > 递归）
		}
		e := ws.Lore[idx]
		matched[idx] = LoreHit{Entry: e, Reason: reason, Chars: len(e.Content)}
	}

	// 1. 恒定层
	for i, e := range ws.Lore {
		if e.Enabled && e.Constant {
			addHit(i, "恒定")
		}
	}

	// 2. 关键词层
	scan := strings.ToLower(scanText)
	for i, e := range ws.Lore {
		if !e.Enabled || e.Constant {
			continue
		}
		if key, ok := matchPrimaryKey(e, scan); ok && matchSecondaryKeys(e, scan) {
			addHit(i, fmt.Sprintf("关键词%q", key))
		}
	}

	// 3. 场景关联层（当前场景名/在场 NPC 名 对 条目 Title/Keys）
	for _, entity := range SceneEntityNames(ws) {
		for i, e := range ws.Lore {
			if !e.Enabled || e.Constant {
				continue
			}
			if _, ok := matched[i]; ok {
				continue
			}
			if matchSceneEntity(e, entity) {
				kind := "NPC"
				if strings.EqualFold(strings.TrimSpace(entity), strings.TrimSpace(ws.Scene.NodeName)) {
					kind = "场景"
				}
				addHit(i, fmt.Sprintf("场景关联: %s %s", kind, entity))
			}
		}
	}

	// 4. 递归层：已命中条目的 Content 再跑关键词层，最多 2 步
	if recursion {
		// 快照当前命中作为第 1 步扫描源（frontier 必须与 matched 脱钩，
		// 否则步内新命中会被同步继续扫描，突破步数上限）
		frontier := make(map[int]LoreHit, len(matched))
		for i, h := range matched {
			frontier[i] = h
		}
		for step := 0; step < loreRecursionMaxSteps && len(frontier) > 0; step++ {
			next := make(map[int]LoreHit)
			for _, src := range frontier {
				content := strings.ToLower(src.Entry.Content)
				if content == "" {
					continue
				}
				for i, e := range ws.Lore {
					if !e.Enabled || e.Constant {
						continue
					}
					if _, ok := matched[i]; ok {
						continue
					}
					if _, ok := matchPrimaryKey(e, content); ok && matchSecondaryKeys(e, content) {
						addHit(i, fmt.Sprintf("递归自%q", src.Entry.Title))
						next[i] = matched[i]
					}
				}
			}
			frontier = next
		}
	}

	// 预算装配：Priority 降序贪心（相同优先级保持召回顺序，稳定排序）
	hits := make([]LoreHit, 0, len(matched))
	for _, h := range matched {
		hits = append(hits, h)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].Entry.Priority > hits[j].Entry.Priority
	})

	used := 0
	for _, h := range hits {
		// 恒定条目豁免裁剪，也不占用预算（对应 ST 的 ignore budget）
		if h.Entry.Constant || budget <= 0 || used+h.Chars <= budget {
			used += h.Chars
			if h.Entry.Position == LorePositionTail {
				result.Tail = append(result.Tail, h)
			} else {
				result.Front = append(result.Front, h)
			}
		} else {
			result.Dropped = append(result.Dropped, h)
		}
	}
	return result
}

// matchPrimaryKey 主键子串匹配（大小写不敏感，中文友好）。
// scan 须已转小写；返回第一个命中的主键原文。
func matchPrimaryKey(e LoreEntry, scan string) (string, bool) {
	if scan == "" {
		return "", false
	}
	for _, k := range e.Keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if strings.Contains(scan, strings.ToLower(k)) {
			return k, true
		}
	}
	return "", false
}

// matchSecondaryKeys 次键逻辑（对扫描文本判断，scan 须已转小写）。
// 无次键时无条件通过；未知模式按 and_any 处理。
func matchSecondaryKeys(e LoreEntry, scan string) bool {
	keys := make([]string, 0, len(e.SecondaryKeys))
	for _, k := range e.SecondaryKeys {
		if k = strings.TrimSpace(k); k != "" {
			keys = append(keys, strings.ToLower(k))
		}
	}
	if len(keys) == 0 {
		return true
	}
	present := func(k string) bool { return scan != "" && strings.Contains(scan, k) }
	switch e.SecondaryMode {
	case LoreSecondaryAndAll:
		for _, k := range keys {
			if !present(k) {
				return false
			}
		}
		return true
	case LoreSecondaryNotAny:
		for _, k := range keys {
			if present(k) {
				return false
			}
		}
		return true
	default: // and_any
		for _, k := range keys {
			if present(k) {
				return true
			}
		}
		return false
	}
}

// matchSceneEntity 场景实体名与条目 Title/Keys 匹配（大小写不敏感）。
// 实体名短于 2 字不参与匹配，避免单字误命中（设计文档 §4.8 风险 1）。
func matchSceneEntity(e LoreEntry, entity string) bool {
	entity = strings.ToLower(strings.TrimSpace(entity))
	if len([]rune(entity)) < 2 {
		return false
	}
	title := strings.ToLower(strings.TrimSpace(e.Title))
	if title != "" && (strings.Contains(title, entity) ||
		(len([]rune(title)) >= 2 && strings.Contains(entity, title))) {
		return true
	}
	for _, k := range e.Keys {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			continue
		}
		if k == entity || strings.Contains(k, entity) ||
			(len([]rune(k)) >= 2 && strings.Contains(entity, k)) {
			return true
		}
	}
	return false
}

package world

import (
	"strings"
	"testing"
)

// loreEntry 测试用条目构造简写。
func loreEntry(id, title string, keys []string, constant bool, position string, priority int, content string) LoreEntry {
	return LoreEntry{
		ID: id, Title: title, Category: LoreCategoryGeo,
		Keys: keys, Constant: constant, Position: position,
		Priority: priority, Enabled: true, Content: content, Source: LoreSourceManual,
	}
}

// findHit 在 front+tail 中按条目 ID 查找命中。
func findHit(r LoreResult, id string) *LoreHit {
	for i := range r.Front {
		if r.Front[i].Entry.ID == id {
			return &r.Front[i]
		}
	}
	for i := range r.Tail {
		if r.Tail[i].Entry.ID == id {
			return &r.Tail[i]
		}
	}
	return nil
}

func findDropped(r LoreResult, id string) *LoreHit {
	for i := range r.Dropped {
		if r.Dropped[i].Entry.ID == id {
			return &r.Dropped[i]
		}
	}
	return nil
}

func TestMigrateLegacyBackground(t *testing.T) {
	// Background 非空且 Lore 为空 -> 生成恒定迁移条目
	ws := NewWorldState("w1", ModeSimRPG)
	ws.Background = "  这是一个剑与魔法的世界。  "
	MigrateLegacyBackground(ws)
	if len(ws.Lore) != 1 {
		t.Fatalf("期望 1 条迁移条目, 实际 %d", len(ws.Lore))
	}
	e := ws.Lore[0]
	if e.ID != LegacyBackgroundEntryID || !e.Constant || !e.Enabled ||
		e.Source != LoreSourceSystem || e.Position != LorePositionFront ||
		e.Content != ws.Background {
		t.Fatalf("迁移条目字段不符: %+v", e)
	}
	// 幂等：再次调用不重复生成
	MigrateLegacyBackground(ws)
	if len(ws.Lore) != 1 {
		t.Fatalf("迁移不幂等: %d 条", len(ws.Lore))
	}

	// 已有 Lore -> 不迁移
	ws2 := NewWorldState("w2", ModeSimRPG)
	ws2.Background = "背景"
	ws2.Lore = []LoreEntry{loreEntry("lor_a", "甲", nil, false, "front", 50, "内容")}
	MigrateLegacyBackground(ws2)
	if len(ws2.Lore) != 1 || ws2.Lore[0].ID != "lor_a" {
		t.Fatalf("已有 Lore 时不应迁移: %+v", ws2.Lore)
	}

	// Background 为空 -> 不迁移
	ws3 := NewWorldState("w3", ModeSimRPG)
	MigrateLegacyBackground(ws3)
	if len(ws3.Lore) != 0 {
		t.Fatalf("空背景不应迁移: %+v", ws3.Lore)
	}
}

func TestResolve_ConstantLayer(t *testing.T) {
	ws := NewWorldState("w", ModeSimRPG)
	ws.Lore = []LoreEntry{
		loreEntry("lor_const", "核心设定", nil, true, "front", 10, "永远注入"),
		loreEntry("lor_disabled", "停用恒定", nil, true, "front", 10, "不应出现"),
		loreEntry("lor_normal", "普通", []string{"龙"}, false, "front", 10, "普通条目"),
	}
	ws.Lore[1].Enabled = false

	r := Resolve(ws, "无关文本", 1000, false)
	h := findHit(r, "lor_const")
	if h == nil || h.Reason != "恒定" {
		t.Fatalf("恒定条目应命中且原因为恒定: %+v", h)
	}
	if findHit(r, "lor_disabled") != nil {
		t.Fatal("停用条目不进入检索")
	}
	if findHit(r, "lor_normal") != nil {
		t.Fatal("关键词未命中的普通条目不应注入")
	}
}

func TestResolve_KeywordLayer(t *testing.T) {
	ws := NewWorldState("w", ModeSimRPG)
	ws.Lore = []LoreEntry{
		loreEntry("lor_hanya", "北境·寒鸦堡", []string{"寒鸦堡", "北境长城"}, false, "front", 60, "寒鸦堡是北境最古老的城堡"),
		loreEntry("lor_case", "StormKeep", []string{"stormkeep"}, false, "front", 60, "A keep."),
	}
	// 中文关键词命中，Reason 带触发词
	r := Resolve(ws, "玩家决定去寒鸦堡探查", 1000, false)
	h := findHit(r, "lor_hanya")
	if h == nil || !strings.Contains(h.Reason, "寒鸦堡") {
		t.Fatalf("中文关键词应命中: %+v", h)
	}
	// 大小写不敏感
	r2 := Resolve(ws, "go to StormKeep now", 1000, false)
	if findHit(r2, "lor_case") == nil {
		t.Fatal("关键词匹配应大小写不敏感")
	}
}

func TestResolve_SecondaryKeys(t *testing.T) {
	mk := func(mode string, sec []string) LoreEntry {
		e := loreEntry("lor_sec", "次键条目", []string{"港口"}, false, "front", 50, "内容")
		e.SecondaryKeys = sec
		e.SecondaryMode = mode
		return e
	}
	cases := []struct {
		name string
		mode string
		sec  []string
		scan string
		hit  bool
	}{
		{"and_any 次键同现", LoreSecondaryAndAny, []string{"守夜人", "商人"}, "港口有守夜人巡逻", true},
		{"and_any 次键缺失", LoreSecondaryAndAny, []string{"守夜人"}, "港口很热闹", false},
		{"and_all 全部同现", LoreSecondaryAndAll, []string{"守夜人", "宵禁"}, "港口的守夜人执行宵禁", true},
		{"and_all 缺一个", LoreSecondaryAndAll, []string{"守夜人", "宵禁"}, "港口有守夜人", false},
		{"not_any 未出现", LoreSecondaryNotAny, []string{"废墟"}, "港口很繁华", true},
		{"not_any 出现则排除", LoreSecondaryNotAny, []string{"废墟"}, "港口已成废墟", false},
		{"无次键无条件通过", "", nil, "港口", true},
	}
	for _, c := range cases {
		ws := NewWorldState("w", ModeSimRPG)
		ws.Lore = []LoreEntry{mk(c.mode, c.sec)}
		r := Resolve(ws, c.scan, 1000, false)
		got := findHit(r, "lor_sec") != nil
		if got != c.hit {
			t.Errorf("%s: 期望命中=%v, 实际=%v", c.name, c.hit, got)
		}
	}
}

func TestResolve_SceneLayer(t *testing.T) {
	ws := NewWorldState("w", ModeSimRPG)
	ws.Scene.NodeName = "寒鸦堡大厅"
	ws.Characters["老王"] = &CharacterState{Name: "老王", Kind: "npc", Alive: true}
	ws.Lore = []LoreEntry{
		loreEntry("lor_castle", "北境·寒鸦堡", []string{"寒鸦堡"}, false, "front", 60, "城堡设定"),
		loreEntry("lor_wang", "老王的人物小传", nil, false, "front", 60, "老王的过去"),
		loreEntry("lor_far", "南海诸岛", nil, false, "front", 60, "无关设定"),
	}
	r := Resolve(ws, "玩家环顾四周", 1000, false)
	h := findHit(r, "lor_castle")
	if h == nil || !strings.HasPrefix(h.Reason, "场景关联: 场景 寒鸦堡大厅") {
		t.Fatalf("场景名关联应命中: %+v", h)
	}
	h2 := findHit(r, "lor_wang")
	if h2 == nil || !strings.HasPrefix(h2.Reason, "场景关联: NPC 老王") {
		t.Fatalf("在场 NPC 关联应命中: %+v", h2)
	}
	if findHit(r, "lor_far") != nil {
		t.Fatal("与场景无关的条目不应命中")
	}
}

func TestResolve_RecursionLayer(t *testing.T) {
	// A 关键词命中 -> A.Content 提到 B 的关键词 -> B.Content 提到 C 的关键词
	ws := NewWorldState("w", ModeSimRPG)
	ws.Lore = []LoreEntry{
		loreEntry("lor_a", "北境", []string{"北境"}, false, "front", 60, "北境的核心是寒鸦城"),
		loreEntry("lor_b", "寒鸦城", []string{"寒鸦城"}, false, "front", 60, "寒鸦城地下沉睡着远古巨龙"),
		loreEntry("lor_c", "远古巨龙", []string{"远古巨龙"}, false, "front", 60, "巨龙设定（不应到达：限 2 步）"),
	}
	// recursion 关闭：只有 A
	r := Resolve(ws, "玩家前往北境", 1000, false)
	if findHit(r, "lor_a") == nil || findHit(r, "lor_b") != nil {
		t.Fatalf("递归关闭时只应命中 A: %+v", r)
	}
	// recursion 开启：A -> B（递归自"北境"），B -> C 是第 2 步内的新命中，
	// C 的 Content 不再扫描（最多 2 步）
	r2 := Resolve(ws, "玩家前往北境", 1000, true)
	hb := findHit(r2, "lor_b")
	if hb == nil || hb.Reason != `递归自"北境"` {
		t.Fatalf("递归命中 B 且原因指向 A: %+v", hb)
	}
	if findHit(r2, "lor_c") == nil {
		t.Fatal("第 2 步递归应命中 C")
	}

	// 限步验证：D 只在 C 的 Content 里出现，需要第 3 步，不应命中
	ws.Lore = append(ws.Lore, loreEntry("lor_d", "龙语魔法", []string{"龙语魔法"}, false, "front", 60, "x"))
	ws.Lore[2].Content = "远古巨龙掌握着龙语魔法"
	r3 := Resolve(ws, "玩家前往北境", 1000, true)
	if findHit(r3, "lor_d") != nil {
		t.Fatal("递归最多 2 步，第 3 步的 D 不应命中")
	}
}

func TestResolve_BudgetTrimAndExempt(t *testing.T) {
	ws := NewWorldState("w", ModeSimRPG)
	ws.Lore = []LoreEntry{
		loreEntry("lor_big_const", "恒定大条目", nil, true, "front", 10, strings.Repeat("恒", 500)),
		loreEntry("lor_high", "高优先", []string{"龙"}, false, "front", 90, strings.Repeat("h", 60)),
		loreEntry("lor_mid", "中优先", []string{"龙"}, false, "front", 50, strings.Repeat("m", 60)),
		loreEntry("lor_low", "低优先", []string{"龙"}, false, "front", 10, strings.Repeat("l", 60)),
	}
	// budget 100：恒定豁免且不占预算；非恒定按优先级降序，60+60>100，只装下 high
	r := Resolve(ws, "龙", 100, false)
	if findHit(r, "lor_big_const") == nil {
		t.Fatal("恒定条目应豁免裁剪")
	}
	if findHit(r, "lor_high") == nil {
		t.Fatal("高优先级条目应入选")
	}
	if findHit(r, "lor_mid") != nil || findDropped(r, "lor_mid") == nil {
		t.Fatal("中优先级条目应被裁剪并记入 Dropped")
	}
	if findDropped(r, "lor_low") == nil {
		t.Fatal("低优先级条目应被裁剪")
	}
	// Dropped 也按优先级降序
	if len(r.Dropped) != 2 || r.Dropped[0].Entry.ID != "lor_mid" || r.Dropped[1].Entry.ID != "lor_low" {
		t.Fatalf("Dropped 顺序应为优先级降序: %+v", r.Dropped)
	}
	// 恒定不计预算：budget 恰好装下 high(60)+mid(60)? 100 不够 -> 仅 high，已验证
}

func TestResolve_FrontTailSplit(t *testing.T) {
	ws := NewWorldState("w", ModeSimRPG)
	ws.Lore = []LoreEntry{
		loreEntry("lor_front", "世界观", []string{"龙"}, false, "front", 60, "世界观设定"),
		loreEntry("lor_tail", "语气", []string{"龙"}, false, "tail", 60, "始终保持冷峻语气"),
		loreEntry("lor_default", "默认位置", []string{"龙"}, false, "", 60, "空 Position 按 front 处理"),
	}
	r := Resolve(ws, "龙", 1000, false)
	if len(r.Front) != 2 || len(r.Tail) != 1 {
		t.Fatalf("front/tail 分流错误: front=%d tail=%d", len(r.Front), len(r.Tail))
	}
	if r.Tail[0].Entry.ID != "lor_tail" {
		t.Fatalf("tail 分区内容错误: %+v", r.Tail)
	}
}

func TestResolve_PriorityOrder(t *testing.T) {
	ws := NewWorldState("w", ModeSimRPG)
	ws.Lore = []LoreEntry{
		loreEntry("lor_p10", "低", []string{"龙"}, false, "front", 10, "a"),
		loreEntry("lor_p90", "高", []string{"龙"}, false, "front", 90, "b"),
		loreEntry("lor_p50", "中", []string{"龙"}, false, "front", 50, "c"),
	}
	r := Resolve(ws, "龙", 1000, false)
	if len(r.Front) != 3 ||
		r.Front[0].Entry.ID != "lor_p90" || r.Front[1].Entry.ID != "lor_p50" || r.Front[2].Entry.ID != "lor_p10" {
		t.Fatalf("注入顺序应为优先级降序: %+v", r.Front)
	}
}

func TestPresentNPCNames(t *testing.T) {
	// 无 Location 信息 -> 全部在场（旧行为等价）
	ws := NewWorldState("w", ModeTRPG)
	ws.Characters["甲"] = &CharacterState{Name: "甲", Alive: true}
	ws.Characters["乙"] = &CharacterState{Name: "乙", Alive: false}
	if got := PresentNPCNames(ws); len(got) != 2 {
		t.Fatalf("无地点信息时应返回全部 NPC: %v", got)
	}
	// 有 Location 信息 -> 按场景名过滤
	ws2 := NewWorldState("w2", ModeSimRPG)
	ws2.Scene.NodeName = "寒鸦堡"
	ws2.Characters["在场者"] = &CharacterState{Name: "在场者", Alive: true, Location: "寒鸦堡"}
	ws2.Characters["远方者"] = &CharacterState{Name: "远方者", Alive: true, Location: "南海"}
	ws2.Characters["死者"] = &CharacterState{Name: "死者", Alive: false, Location: "寒鸦堡"}
	got := PresentNPCNames(ws2)
	if len(got) != 1 || got[0] != "在场者" {
		t.Fatalf("按地点过滤在场 NPC 错误: %v", got)
	}
}

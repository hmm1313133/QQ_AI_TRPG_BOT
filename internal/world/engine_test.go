package world

import "testing"

func newTestWorldState() *WorldState {
	ws := NewWorldState("test-world", ModeTRPG)
	ws.Characters["老陈"] = &CharacterState{
		Name: "老陈", Kind: "npc", Alive: true, Disposition: "neutral",
	}
	ws.HiddenInfo = []HiddenItem{
		{ID: "node_1_clue_0", Description: "书架上的日记本，记录着仪式的细节"},
	}
	ws.EventQueue = []ScheduledEvent{
		{ID: "node_1_trigger_0", Description: "玩家触碰祭坛时触发守卫出现", Type: "trigger"},
	}
	ws.Quests = []QuestState{
		{Description: "调查书房找到仪式线索"},
	}
	return ws
}

func TestApplyEvent_HiddenByID(t *testing.T) {
	e := NewEngine(nil)
	ws := newTestWorldState()
	ok, err := e.ApplyEvent(ws, WorldEvent{Type: "hidden_discovered", Target: "node_1_clue_0"})
	if !ok || err != nil {
		t.Fatalf("按 ID 匹配应命中: ok=%v err=%v", ok, err)
	}
	if !ws.HiddenInfo[0].Discovered {
		t.Fatal("线索应被标记为已发现")
	}
	// 命中的事件应记入 EventLog
	if len(ws.EventLog) != 1 || ws.EventLog[0].Type != "hidden_discovered" {
		t.Fatalf("EventLog 应记录该事件: %+v", ws.EventLog)
	}
}

func TestApplyEvent_HiddenByDescriptionFragment(t *testing.T) {
	e := NewEngine(nil)
	ws := newTestWorldState()
	ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "hidden_discovered", Target: "书架上的日记本"})
	if !ok {
		t.Fatal("描述片段应命中")
	}
}

func TestApplyEvent_EventByDescriptionFragment(t *testing.T) {
	e := NewEngine(nil)
	ws := newTestWorldState()
	ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "event_triggered", Target: "触碰祭坛"})
	if !ok {
		t.Fatal("事件描述片段应命中")
	}
	if !ws.EventQueue[0].Triggered {
		t.Fatal("事件应被标记为已触发")
	}
}

func TestApplyEvent_NPCDispositionFuzzyName(t *testing.T) {
	e := NewEngine(nil)
	ws := newTestWorldState()
	ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "npc_disposition", Target: " 老陈 ", Value: "hostile"})
	if !ok {
		t.Fatal("带空格的 NPC 名称应模糊命中")
	}
	if ws.Characters["老陈"].Disposition != "hostile" {
		t.Fatal("NPC 态度应被更新为 hostile")
	}
}

func TestApplyEvent_ObjectiveCompleted(t *testing.T) {
	e := NewEngine(nil)
	ws := newTestWorldState()
	ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "objective_completed", Target: "调查书房"})
	if !ok {
		t.Fatal("目标描述片段应命中")
	}
	if !ws.Quests[0].Completed {
		t.Fatal("目标应被标记为已完成")
	}
}

func TestApplyEvent_Unmatched(t *testing.T) {
	e := NewEngine(nil)
	ws := newTestWorldState()
	ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "hidden_discovered", Target: "不存在的线索物品"})
	if ok {
		t.Fatal("不存在的目标不应命中")
	}
	ok, _ = e.ApplyEvent(ws, WorldEvent{Type: "hidden_discovered", Target: "日记"})
	if ok {
		t.Fatal("过短 target 不应通过子串匹配命中")
	}
	_, err := e.ApplyEvent(ws, WorldEvent{Type: "unknown_type", Target: "x"})
	if err == nil {
		t.Fatal("未知类型应返回错误")
	}
}

func TestApplyEvent_DeathLock(t *testing.T) {
	e := NewEngine(nil)
	ws := newTestWorldState()

	// NPC 死亡 → 自动加锁
	ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "npc_disposition", Target: "老陈", Value: "dead"})
	if !ok {
		t.Fatal("死亡变更应命中")
	}
	if ws.Characters["老陈"].Alive {
		t.Fatal("NPC 应被标记为死亡")
	}
	if !ws.IsLocked("npc:老陈:dead") {
		t.Fatal("死亡应产生状态锁定")
	}

	// 已锁定死亡 NPC 不能再变更态度（防复活幻觉）
	ok, _ = e.ApplyEvent(ws, WorldEvent{Type: "npc_disposition", Target: "老陈", Value: "friendly"})
	if ok {
		t.Fatal("已死 NPC 的态度变更应被拒绝")
	}
	if ws.Characters["老陈"].Disposition != "dead" {
		t.Fatal("已死 NPC 的态度不应被改变")
	}
}

func TestApplyEvent_MoodAndRelation(t *testing.T) {
	e := NewEngine(nil)
	ws := newTestWorldState()

	ok, _ := e.ApplyEvent(ws, WorldEvent{
		Type: "mood_change", Target: "老陈",
		Value: "valence=+30,arousal=+10,tag=感激",
	})
	if !ok {
		t.Fatal("情绪变更应命中")
	}
	mood := ws.Characters["老陈"].Mood
	if mood.Valence != 30 || mood.Arousal != 10 || len(mood.Tags) != 1 {
		t.Fatalf("情绪状态不正确: %+v", mood)
	}

	ok, _ = e.ApplyEvent(ws, WorldEvent{
		Type: "relation_change", Target: "player",
		Value: "to=老陈,trust=-50,fear=+30",
	})
	if !ok {
		t.Fatal("关系变更应命中")
	}
	rel := ws.GetRelation("player", "老陈")
	if rel.Trust != -50 || rel.Fear != 30 {
		t.Fatalf("关系边不正确: %+v", rel)
	}
}

func TestApplyEvent_FactBiTemporal(t *testing.T) {
	e := NewEngine(nil)
	ws := newTestWorldState()
	ws.Clock.WorldTime = 100

	// 添加事实：门开着
	e.ApplyEvent(ws, WorldEvent{Type: "fact_add", Target: "酒馆大门", Value: "predicate=状态,object=open"})
	// 时间推进后：门锁了（旧事实应失效而非覆盖）
	ws.Clock.WorldTime = 200
	e.ApplyEvent(ws, WorldEvent{Type: "fact_add", Target: "酒馆大门", Value: "predicate=状态,object=locked"})

	if len(ws.Facts) != 2 {
		t.Fatalf("应有 2 条事实: %d", len(ws.Facts))
	}
	if ws.Facts[0].InvalidAt != 200 {
		t.Fatalf("旧事实应在 t=200 失效: %+v", ws.Facts[0])
	}
	if ws.Facts[1].InvalidAt != 0 {
		t.Fatalf("新事实应仍有效: %+v", ws.Facts[1])
	}
}

func TestApplyEvents_Count(t *testing.T) {
	e := NewEngine(nil)
	ws := newTestWorldState()
	applied := e.ApplyEvents(ws, []WorldEvent{
		{Type: "hidden_discovered", Target: "node_1_clue_0"},     // 命中
		{Type: "event_triggered", Target: "不存在的事件描述文本"}, // 未命中
		{Type: "objective_completed", Target: "调查书房"},         // 命中
	})
	if applied != 2 {
		t.Fatalf("应命中 2 条，实际 %d 条", applied)
	}
}

func TestMatchTarget(t *testing.T) {
	cases := []struct {
		target, id, desc string
		want             bool
	}{
		{"node_1_clue_0", "node_1_clue_0", "描述", true},
		{"书架上的日记本", "id1", "书架上的日记本，记录着仪式的细节", true},
		{"线索：书架上的日记本，记录着仪式的细节（已发现）", "id1", "书架上的日记本，记录着仪式的细节", true},
		{"日记", "id1", "书架上的日记本", false}, // 过短不模糊匹配
		{"", "id1", "desc", false},
		{"完全不相关的内容", "id1", "书架上的日记本", false},
	}
	for _, c := range cases {
		if got := MatchTarget(c.target, c.id, c.desc); got != c.want {
			t.Errorf("MatchTarget(%q, %q, %q) = %v, want %v", c.target, c.id, c.desc, got, c.want)
		}
	}
}

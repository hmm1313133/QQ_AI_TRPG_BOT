package agent

import "testing"

func newTestGameState() *GameState {
	return &GameState{
		NPCStates: map[string]NPCState{
			"老陈": {Name: "老陈", Disposition: "neutral"},
		},
		HiddenInfo: []HiddenItem{
			{ID: "node_1_clue_0", Description: "书架上的日记本，记录着仪式的细节"},
		},
		PendingEvents: []PendingEvent{
			{ID: "node_1_trigger_0", Description: "玩家触碰祭坛时触发守卫出现", Type: "trigger"},
		},
		Objectives: []ObjectiveState{
			{Description: "调查书房找到仪式线索"},
		},
	}
}

func TestApplyUpdate_HiddenByID(t *testing.T) {
	gs := newTestGameState()
	if !gs.ApplyUpdate(StateUpdate{Type: "hidden_discovered", Target: "node_1_clue_0"}) {
		t.Fatal("按 ID 匹配应命中")
	}
	if !gs.HiddenInfo[0].Discovered {
		t.Fatal("线索应被标记为已发现")
	}
}

func TestApplyUpdate_HiddenByDescriptionFragment(t *testing.T) {
	gs := newTestGameState()
	// Narrator 工具通常只能给出描述片段而非内部 ID
	if !gs.ApplyUpdate(StateUpdate{Type: "hidden_discovered", Target: "书架上的日记本"}) {
		t.Fatal("描述片段应命中")
	}
	if !gs.HiddenInfo[0].Discovered {
		t.Fatal("线索应被标记为已发现")
	}
}

func TestApplyUpdate_HiddenByLongTargetContainingDescription(t *testing.T) {
	gs := newTestGameState()
	// LLM 给出带前后缀的长文本，包含完整描述
	if !gs.ApplyUpdate(StateUpdate{Type: "hidden_discovered", Target: "线索：书架上的日记本，记录着仪式的细节（已发现）"}) {
		t.Fatal("包含完整描述的长 target 应命中")
	}
}

func TestApplyUpdate_EventByDescriptionFragment(t *testing.T) {
	gs := newTestGameState()
	if !gs.ApplyUpdate(StateUpdate{Type: "event_triggered", Target: "触碰祭坛"}) {
		t.Fatal("事件描述片段应命中")
	}
	if !gs.PendingEvents[0].Triggered {
		t.Fatal("事件应被标记为已触发")
	}
}

func TestApplyUpdate_NPCDispositionFuzzyName(t *testing.T) {
	gs := newTestGameState()
	if !gs.ApplyUpdate(StateUpdate{Type: "npc_disposition", Target: " 老陈 ", Value: "hostile"}) {
		t.Fatal("带空格的 NPC 名称应模糊命中")
	}
	if gs.NPCStates["老陈"].Disposition != "hostile" {
		t.Fatal("NPC 态度应被更新为 hostile")
	}
}

func TestApplyUpdate_ObjectiveCompleted(t *testing.T) {
	gs := newTestGameState()
	if !gs.ApplyUpdate(StateUpdate{Type: "objective_completed", Target: "调查书房"}) {
		t.Fatal("目标描述片段应命中")
	}
	if !gs.Objectives[0].Completed {
		t.Fatal("目标应被标记为已完成")
	}
}

func TestApplyUpdate_Unmatched(t *testing.T) {
	gs := newTestGameState()
	if gs.ApplyUpdate(StateUpdate{Type: "hidden_discovered", Target: "不存在的线索物品"}) {
		t.Fatal("不存在的目标不应命中")
	}
	if gs.ApplyUpdate(StateUpdate{Type: "unknown_type", Target: "node_1_clue_0"}) {
		t.Fatal("未知类型不应命中")
	}
}

func TestApplyUpdate_ShortTargetNoSubstringMatch(t *testing.T) {
	gs := newTestGameState()
	// 过短 target（<4 字）不允许子串匹配，防止误命中
	if gs.ApplyUpdate(StateUpdate{Type: "hidden_discovered", Target: "日记"}) {
		t.Fatal("过短 target 不应通过子串匹配命中")
	}
}

func TestApplyUpdates_Count(t *testing.T) {
	gs := newTestGameState()
	applied := gs.ApplyUpdates([]StateUpdate{
		{Type: "hidden_discovered", Target: "node_1_clue_0"},   // 命中
		{Type: "event_triggered", Target: "不存在的事件描述文本"},   // 未命中
		{Type: "objective_completed", Target: "调查书房"},       // 命中
	})
	if applied != 2 {
		t.Fatalf("应命中 2 条，实际 %d 条", applied)
	}
}

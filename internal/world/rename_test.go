package world

import "testing"

func TestDisplayNameFallback(t *testing.T) {
	ws := &WorldState{WorldID: "w1"}
	if got := ws.DisplayName(); got != "" {
		t.Errorf("无名称时应返回空串，got %q", got)
	}
	ws.ScriptName = "旧剧本名"
	if got := ws.DisplayName(); got != "旧剧本名" {
		t.Errorf("应回退 ScriptName，got %q", got)
	}
	ws.Name = "新世界名"
	if got := ws.DisplayName(); got != "新世界名" {
		t.Errorf("Name 应优先于 ScriptName，got %q", got)
	}
}

func TestApplyEventRename(t *testing.T) {
	e := NewEngine(nil)
	ws := NewWorldState("w1", ModeSimRPG)

	ok, err := e.ApplyEvent(ws, WorldEvent{Type: "rename", Actor: "player", Target: "w1", Value: "  雾都之夜  "})
	if err != nil || !ok {
		t.Fatalf("rename 应命中: ok=%v err=%v", ok, err)
	}
	if ws.Name != "雾都之夜" {
		t.Errorf("名称应去空白，got %q", ws.Name)
	}

	// 空名不命中
	if ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "rename", Target: "w1", Value: "  "}); ok {
		t.Error("空名不应命中")
	}
	// 与现名相同不命中
	if ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "rename", Target: "w1", Value: "雾都之夜"}); ok {
		t.Error("同名不应命中")
	}
	// 超长截断到 50 字
	long := make([]rune, 60)
	for i := range long {
		long[i] = '界'
	}
	if ok, _ := e.ApplyEvent(ws, WorldEvent{Type: "rename", Target: "w1", Value: string(long)}); !ok {
		t.Fatal("超长名应命中（截断）")
	}
	if got := len([]rune(ws.Name)); got != 50 {
		t.Errorf("名称应截断到 50 字，got %d", got)
	}
}

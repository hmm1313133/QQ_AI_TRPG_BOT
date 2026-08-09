// 回合快照（SaveTurnSnapshot/RestoreTurnSnapshot）与 RecentTurns 叙事同步测试。
package world

import "testing"

func TestTurnSnapshot_SaveRestoreRoundtrip(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	e := NewEngine(repo)

	// 无快照时 ok=false
	if _, ok, err := e.RestoreTurnSnapshot("w-snap"); err != nil || ok {
		t.Fatalf("无快照应 ok=false: (%v,%v)", ok, err)
	}

	// 回合前状态写快照
	ws := NewWorldState("w-snap", ModeSimRPG)
	ws.Background = "边境小镇"
	ws.RoundCount = 5
	ws.AppendTurn("我四处看看", "你看到一扇门")
	if err := e.Save(ws); err != nil {
		t.Fatal(err)
	}
	if err := e.SaveTurnSnapshot("w-snap", ws, "我推开门"); err != nil {
		t.Fatalf("SaveTurnSnapshot: %v", err)
	}

	// 模拟回合落账（状态漂移）
	mutated := e.LoadOrNil("w-snap")
	mutated.RoundCount = 6
	mutated.AppendTurn("我推开门", "门后是一条走廊")
	if err := e.Save(mutated); err != nil {
		t.Fatal(err)
	}

	// 恢复快照：世界回到 RoundCount=5，返回当时的用户输入
	userText, ok, err := e.RestoreTurnSnapshot("w-snap")
	if err != nil || !ok || userText != "我推开门" {
		t.Fatalf("RestoreTurnSnapshot: (%q,%v,%v)", userText, ok, err)
	}
	got := e.LoadOrNil("w-snap")
	if got.RoundCount != 5 || len(got.RecentTurns) != 1 || got.RecentTurns[0].Narrator != "你看到一扇门" {
		t.Fatalf("世界应回滚到快照: %+v", got)
	}

	// 再次恢复仍是同一基点（幂等）
	if _, ok, err := e.RestoreTurnSnapshot("w-snap"); err != nil || !ok {
		t.Fatalf("重复恢复应仍命中: (%v,%v)", ok, err)
	}
	if got := e.LoadOrNil("w-snap"); got.RoundCount != 5 {
		t.Fatalf("重复恢复后应仍是快照状态: round=%d", got.RoundCount)
	}
}

func TestEngine_UpdateRecentNarrator(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	e := NewEngine(repo)

	ws := NewWorldState("w-narr", ModeSimRPG)
	ws.AppendTurn("第一句", "回复A")
	ws.AppendTurn("第二句", "回复B")
	ws.AppendTurn("第三句", "回复A") // 与第一条同文本：应替换最后一处
	if err := e.Save(ws); err != nil {
		t.Fatal(err)
	}

	if err := e.UpdateRecentNarrator("w-narr", "回复A", "回复A（改）"); err != nil {
		t.Fatalf("UpdateRecentNarrator: %v", err)
	}
	got := e.LoadOrNil("w-narr")
	if got.RecentTurns[0].Narrator != "回复A" || got.RecentTurns[2].Narrator != "回复A（改）" {
		t.Fatalf("应替换窗口内最后一处匹配: %+v", got.RecentTurns)
	}

	// 窗口内未命中：静默成功
	if err := e.UpdateRecentNarrator("w-narr", "不存在的文本", "x"); err != nil {
		t.Fatalf("未命中应静默成功: %v", err)
	}
	// 世界不存在：静默成功
	if err := e.UpdateRecentNarrator("no-such", "a", "b"); err != nil {
		t.Fatalf("世界不存在应静默成功: %v", err)
	}
	// old == new：no-op
	if err := e.UpdateRecentNarrator("w-narr", "回复B", "回复B"); err != nil {
		t.Fatalf("old==new 应 no-op: %v", err)
	}
}

// 非 SQLite 存储（JSON 文件库）不支持快照：静默跳过。
func TestTurnSnapshot_NonSQLiteRepo(t *testing.T) {
	dir := t.TempDir()
	repo, err := NewJSONRepository(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := NewEngine(repo)
	ws := NewWorldState("w-json", ModeSimRPG)
	if err := e.SaveTurnSnapshot("w-json", ws, "输入"); err != nil {
		t.Fatalf("JSON 库存快照应静默跳过: %v", err)
	}
	if _, ok, err := e.RestoreTurnSnapshot("w-json"); err != nil || ok {
		t.Fatalf("JSON 库恢复快照应 ok=false: (%v,%v)", ok, err)
	}
}

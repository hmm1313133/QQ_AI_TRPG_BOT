package world

import "testing"

func TestSaveIfRevisionRejectsStaleState(t *testing.T) {
	repo, err := NewJSONRepository(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	state := NewWorldState("revision", ModeSimRPG)
	if err := repo.Save(state); err != nil {
		t.Fatal(err)
	}
	base := state.Revision
	state.RoundCount = 1
	if err := repo.SaveIfRevision(state, base); err != nil {
		t.Fatal(err)
	}
	stale := *state
	stale.RoundCount = 99
	if err := repo.SaveIfRevision(&stale, base); err == nil {
		t.Fatal("过期版本提交必须失败")
	}
	got, err := repo.Load("revision")
	if err != nil || got.RoundCount != 1 {
		t.Fatalf("过期提交不应覆盖当前状态: %+v, %v", got, err)
	}
}

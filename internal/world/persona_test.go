package world

import (
	"path/filepath"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
)

// EffectivePersona 优先级：本世界覆盖 > 全局默认 > 无。
func TestEffectivePersona_Priority(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	store, err := persona.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Set("u1", persona.Profile{Name: "全局名", Description: "全局描述"}); err != nil {
		t.Fatal(err)
	}

	ws := NewWorldState("w", ModeSimRPG)

	// 无覆盖 -> 全局默认
	if p := ws.EffectivePersona("u1", store); p == nil || p.Name != "全局名" {
		t.Fatalf("无覆盖应回退全局默认: %+v", p)
	}

	// 有覆盖 -> 本世界覆盖优先
	ws.Personas["u1"] = &persona.Profile{Name: "世界名", Description: "世界描述"}
	if p := ws.EffectivePersona("u1", store); p == nil || p.Name != "世界名" {
		t.Fatalf("覆盖应优先于全局默认: %+v", p)
	}

	// 覆盖为空条目 -> 视为无覆盖，回退全局
	ws.Personas["u1"] = &persona.Profile{}
	if p := ws.EffectivePersona("u1", store); p == nil || p.Name != "全局名" {
		t.Fatalf("空覆盖应回退全局默认: %+v", p)
	}

	// 全局也无 -> nil；global 为 nil 安全
	delete(ws.Personas, "u1")
	if p := ws.EffectivePersona("u2", store); p != nil {
		t.Fatalf("均无设置应返回 nil: %+v", p)
	}
	if p := ws.EffectivePersona("u1", nil); p != nil {
		t.Fatalf("global 为 nil 且无覆盖应返回 nil: %+v", p)
	}
}

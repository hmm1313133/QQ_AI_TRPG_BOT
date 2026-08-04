package world

import (
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
)

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	repo, err := NewJSONRepository(t.TempDir())
	if err != nil {
		t.Fatalf("创建测试仓库失败: %v", err)
	}
	return NewEngine(repo)
}

func TestSeedWorld_SimRPG(t *testing.T) {
	e := newTestEngine(t)
	state, err := e.SeedWorld("w-sim", SeedSpec{
		Mode:       ModeSimRPG,
		Background: "剑与魔法的边境小镇",
		Scene:      "雨夜的酒馆",
		NPCs: []NPCSeed{
			{Name: "酒馆老板", Personality: "精明健谈，想打探冒险者的来历"},
		},
		Locations: []string{"酒馆", "集市"},
	}, nil)
	if err != nil {
		t.Fatalf("simrpg 播种失败: %v", err)
	}
	if state.Mode != ModeSimRPG || state.Background != "剑与魔法的边境小镇" {
		t.Fatalf("世界基本信息不正确: %+v", state)
	}
	if state.Scene.Description != "雨夜的酒馆" {
		t.Fatalf("初始场景未填充: %+v", state.Scene)
	}
	npc := state.Characters["酒馆老板"]
	if npc == nil || !npc.Alive || npc.Disposition != "neutral" || npc.Kind != "npc" {
		t.Fatalf("NPC 默认值不正确: %+v", npc)
	}
	if len(npc.Goals) != 1 || npc.Goals[0].Description != "精明健谈，想打探冒险者的来历" {
		t.Fatalf("Personality 应映射为 Goals: %+v", npc.Goals)
	}
	if len(state.Locations) != 2 || state.Locations["loc_0"].Name != "酒馆" {
		t.Fatalf("地点未填充: %+v", state.Locations)
	}

	// 已落库可加载
	loaded, err := e.Load("w-sim")
	if err != nil || loaded.Mode != ModeSimRPG {
		t.Fatalf("播种后的世界应可加载: err=%v", err)
	}
}

func TestSeedWorld_RoleplayRequiresNPC(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.SeedWorld("w-rp", SeedSpec{Mode: ModeRoleplay, Background: "x"}, nil); err == nil {
		t.Fatal("roleplay 无 NPC 应报错")
	}
	state, err := e.SeedWorld("w-rp", SeedSpec{
		Mode: ModeRoleplay,
		NPCs: []NPCSeed{{Name: "搭档", Kind: "npc", Disposition: "friendly"}},
	}, nil)
	if err != nil {
		t.Fatalf("roleplay 播种失败: %v", err)
	}
	if state.Characters["搭档"].Disposition != "friendly" {
		t.Fatalf("Disposition 应保留传入值: %+v", state.Characters["搭档"])
	}
}

func TestSeedWorld_TRPGDelegatesToScript(t *testing.T) {
	e := newTestEngine(t)
	scr := &script.Script{
		ID: "s1", Name: "测试剧本", Title: "测试剧本", System: "coc7",
		Timeline: []script.TimelineNode{{ID: "node_1", Name: "开场", Order: 1}},
	}
	if _, err := e.SeedWorld("w-trpg", SeedSpec{Mode: ModeTRPG, ScriptID: "s1"}, nil); err == nil {
		t.Fatal("trpg 缺剧本应报错")
	}
	state, err := e.SeedWorld("w-trpg", SeedSpec{Mode: ModeTRPG, ScriptID: "s1"}, scr)
	if err != nil {
		t.Fatalf("trpg 播种失败: %v", err)
	}
	if state.ScriptID != "s1" || state.Scene.NodeID != "node_1" {
		t.Fatalf("trpg 应复用 InitFromScript: %+v", state)
	}
}

func TestSeedWorld_Validation(t *testing.T) {
	e := newTestEngine(t)
	if _, err := e.SeedWorld("", SeedSpec{Mode: ModeSimRPG}, nil); err == nil {
		t.Fatal("空世界 ID 应报错")
	}
	if _, err := e.SeedWorld("w-bad", SeedSpec{Mode: "unknown"}, nil); err == nil {
		t.Fatal("未知模式应报错")
	}
	if _, err := e.SeedWorld("w-npc", SeedSpec{Mode: ModeSimRPG, NPCs: []NPCSeed{{Name: ""}}}, nil); err == nil {
		t.Fatal("空 NPC 名称应报错")
	}

	// 重复 ID 应报错，且不能覆盖已有世界
	if _, err := e.SeedWorld("w-dup", SeedSpec{Mode: ModeSimRPG, Background: "旧设定"}, nil); err != nil {
		t.Fatalf("首次播种失败: %v", err)
	}
	if _, err := e.SeedWorld("w-dup", SeedSpec{Mode: ModeSimRPG, Background: "新设定"}, nil); err == nil {
		t.Fatal("重复世界 ID 应报错")
	}
	loaded, _ := e.Load("w-dup")
	if loaded.Background != "旧设定" {
		t.Fatal("重复播种不应覆盖已有世界")
	}
}

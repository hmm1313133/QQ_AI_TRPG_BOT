// 剧本 → 世界实体派生映射测试（设计 §11.2）。
package world

import (
	"path/filepath"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
)

func scriptFixture() *script.Script {
	return &script.Script{
		ID:    "scr_test",
		Name:  "活神之手",
		Title: "活神之手（完整版）",
		Background: script.StoryBackground{
			Setting:          "1920 年代新英格兰",
			Synopsis:         "调查失踪案",
			Backstory:        "阿卡姆城的阴霾……",
			KeyOrganizations: []string{"银暮会", "阿卡姆警局"},
			KeyThemes:        []string{"疯狂"},
		},
		Timeline: []script.TimelineNode{
			{ID: "node_1", Name: "第一幕 降临", Type: "act", Order: 1, Description: "开端"},
			{ID: "node_2", Name: "图书馆调查", Type: "scene", Order: 2, IsKeyNode: true, Description: "查资料"},
			{ID: "node_3", Name: "第二幕 觉醒", Type: "act", Order: 3, Description: "高潮"},
		},
		Characters: []script.ScriptCharacter{
			{ID: "char_1", Name: "亨利探长", Role: "npc", Personality: "严谨", Background: "老警察", Appearance: "灰胡子"},
		},
		Scenes: []script.ScriptScene{
			{ID: "scene_1", Name: "阿卡姆图书馆", Description: "藏书丰富", Atmosphere: "安静", DangerLevel: "安全", InvestigationPoints: []string{"禁书区"}},
			{ID: "scene_2", Name: "废弃教堂", Description: "坍塌一半"},
		},
	}
}

func TestStorylineFromScript(t *testing.T) {
	scr := scriptFixture()
	sl := StorylineFromScript(scr)
	if sl == nil {
		t.Fatal("应派生出主线")
	}
	if sl.Title != "活神之手（完整版）" {
		t.Fatalf("主线标题应取剧本标题: %s", sl.Title)
	}
	if sl.Premise != "调查失踪案" {
		t.Fatalf("主线前提应取 synopsis: %s", sl.Premise)
	}
	// 优先 act 节点
	if len(sl.Acts) != 2 || sl.Acts[0].Title != "第一幕 降临" || sl.Acts[1].Title != "第二幕 觉醒" {
		t.Fatalf("应优先取 act 节点: %+v", sl.Acts)
	}
	if sl.Acts[0].Status != "active" || sl.Acts[1].Status != "pending" {
		t.Fatalf("首幕 active 其余 pending: %+v", sl.Acts)
	}

	// 无 act 节点 → 回退关键节点
	scr2 := &script.Script{Name: "s2", Timeline: []script.TimelineNode{
		{ID: "n1", Name: "普通", Order: 1},
		{ID: "n2", Name: "关键", Order: 2, IsKeyNode: true},
	}}
	sl2 := StorylineFromScript(scr2)
	if len(sl2.Acts) != 1 || sl2.Acts[0].Title != "关键" {
		t.Fatalf("无 act 时应取关键节点: %+v", sl2.Acts)
	}
	if sl2.Title != "s2" {
		t.Fatalf("无标题时回退 Name: %s", sl2.Title)
	}

	// 全普通节点 → 全部
	scr3 := &script.Script{Name: "s3", Timeline: []script.TimelineNode{
		{ID: "n1", Name: "甲", Order: 1}, {ID: "n2", Name: "乙", Order: 2},
	}}
	if sl3 := StorylineFromScript(scr3); len(sl3.Acts) != 2 {
		t.Fatalf("无 act/关键节点时应取全部: %+v", sl3.Acts)
	}

	// 空时间轴 → nil
	if StorylineFromScript(&script.Script{Name: "empty"}) != nil {
		t.Fatal("空时间轴不应派生主线")
	}
}

func TestLocationsAndFactionsFromScript(t *testing.T) {
	scr := scriptFixture()
	locs := LocationsFromScript(scr)
	if len(locs) != 2 {
		t.Fatalf("应有 2 个地点: %d", len(locs))
	}
	lib := locs[0]
	if lib.Name != "阿卡姆图书馆" || lib.Atmosphere != "安静" || lib.Danger != "安全" || len(lib.Points) != 1 {
		t.Fatalf("地点字段映射错误: %+v", lib)
	}

	facs := FactionsFromScript(scr)
	if len(facs) != 2 || facs[0].Name != "银暮会" {
		t.Fatalf("势力派生错误: %+v", facs)
	}

	wv := WorldviewFromScript(scr)
	if wv.Setting != "1920 年代新英格兰" || wv.Backstory == "" || len(wv.Themes) != 1 {
		t.Fatalf("世界观派生错误: %+v", wv)
	}
}

// InitFromScript 端到端：创建 trpg 世界即自动带入主线/地点/势力/角色创作字段。
func TestInitFromScriptDerivesAssets(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	defer db.Close()
	repo, err := NewSQLiteRepository(db)
	if err != nil {
		t.Fatalf("创建仓库失败: %v", err)
	}
	engine := NewEngine(repo)

	ws, err := engine.InitFromScript("w_trpg", scriptFixture())
	if err != nil {
		t.Fatalf("InitFromScript 失败: %v", err)
	}
	if ws.Storyline == nil || len(ws.Storyline.Acts) != 2 {
		t.Fatalf("应派生主线: %+v", ws.Storyline)
	}
	if len(ws.Locations) != 2 {
		t.Fatalf("应派生 2 个地点: %d", len(ws.Locations))
	}
	if ws.Locations["阿卡姆图书馆"].Danger != "安全" {
		t.Fatalf("地点字段应带入: %+v", ws.Locations["阿卡姆图书馆"])
	}
	if len(ws.Factions) != 2 {
		t.Fatalf("应派生 2 个势力: %d", len(ws.Factions))
	}
	ch := ws.Characters["亨利探长"]
	if ch == nil || ch.Personality != "严谨" || ch.Backstory != "老警察" || ch.Appearance != "灰胡子" {
		t.Fatalf("角色创作字段应带入: %+v", ch)
	}
}

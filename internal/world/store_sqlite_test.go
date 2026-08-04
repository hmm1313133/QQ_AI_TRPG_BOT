package world

import (
	"path/filepath"
	"testing"
)

func newTestSQLiteRepo(t *testing.T) *SQLiteRepository {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	repo, err := NewSQLiteRepository(db)
	if err != nil {
		t.Fatalf("NewSQLiteRepository: %v", err)
	}
	return repo
}

func TestSQLiteRepository_SaveLoadList(t *testing.T) {
	repo := newTestSQLiteRepo(t)

	ws := NewWorldState("w1", ModeSimRPG)
	ws.Scene.NodeName = "边境小镇"
	ws.RoundCount = 7
	ws.Items["秘银短剑"] = &Item{ID: "item_0", Name: "秘银短剑", Type: "weapon", Owner: "玩家"}
	ws.Storyline = &Storyline{Title: "北境风云", Acts: []StoryAct{{ID: "act_0", Title: "序章", Status: "active"}}}
	if err := repo.Save(ws); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.Load("w1")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Scene.NodeName != "边境小镇" || got.RoundCount != 7 {
		t.Fatalf("round-trip 字段丢失: %+v", got.Scene)
	}
	if got.Items["秘银短剑"] == nil || got.Items["秘银短剑"].Owner != "玩家" {
		t.Fatal("items 未持久化")
	}
	if got.Storyline == nil || got.Storyline.Acts[0].Status != "active" {
		t.Fatal("storyline 未持久化")
	}

	ids, err := repo.List()
	if err != nil || len(ids) != 1 || ids[0] != "w1" {
		t.Fatalf("List: %v %v", ids, err)
	}

	// 覆盖写（upsert）
	ws.RoundCount = 8
	if err := repo.Save(ws); err != nil {
		t.Fatalf("Save 覆盖: %v", err)
	}
	got2, _ := repo.Load("w1")
	if got2.RoundCount != 8 {
		t.Fatal("upsert 未生效")
	}

	// 删除连同存档
	if _, err := repo.CreateSave(ws, "档1", "", false); err != nil {
		t.Fatalf("CreateSave: %v", err)
	}
	if err := repo.Delete("w1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Load("w1"); err == nil {
		t.Fatal("删除后不应能 Load")
	}
	saves, _ := repo.ListSaves("w1")
	if len(saves) != 0 {
		t.Fatal("删除世界应级联删除存档")
	}
}

func TestSQLiteRepository_Saves(t *testing.T) {
	repo := newTestSQLiteRepo(t)
	ws := NewWorldState("w2", ModeSimRPG)
	ws.RoundCount = 3
	if err := repo.Save(ws); err != nil {
		t.Fatal(err)
	}

	info, err := repo.CreateSave(ws, "boss 战前", "备注", false)
	if err != nil {
		t.Fatalf("CreateSave: %v", err)
	}
	if info.ID == 0 || info.RoundCount != 3 || info.Auto {
		t.Fatalf("SaveInfo 元数据错误: %+v", info)
	}

	// 推进后恢复
	ws.RoundCount = 10
	_ = repo.Save(ws)
	_, snap, err := repo.LoadSave(info.ID)
	if err != nil {
		t.Fatalf("LoadSave: %v", err)
	}
	if snap.RoundCount != 3 {
		t.Fatalf("快照应为 RoundCount=3，实际 %d", snap.RoundCount)
	}

	listed, err := repo.ListSaves("w2")
	if err != nil || len(listed) != 1 || listed[0].Name != "boss 战前" {
		t.Fatalf("ListSaves: %+v %v", listed, err)
	}

	// 自动档滚动保留
	for i := 0; i < autoSaveKeep+3; i++ {
		if _, err := repo.CreateSave(ws, "自动备份", "", true); err != nil {
			t.Fatal(err)
		}
	}
	listed, _ = repo.ListSaves("w2")
	autoCount := 0
	for _, s := range listed {
		if s.Auto {
			autoCount++
		}
	}
	if autoCount != autoSaveKeep {
		t.Fatalf("自动档应滚动保留 %d 条，实际 %d", autoSaveKeep, autoCount)
	}

	if err := repo.DeleteSave(info.ID); err != nil {
		t.Fatalf("DeleteSave: %v", err)
	}
	if err := repo.DeleteSave(info.ID); err == nil {
		t.Fatal("重复删除应报错")
	}
}

func TestAssetStore_CRUD(t *testing.T) {
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store, err := NewAssetStore(db)
	if err != nil {
		t.Fatal(err)
	}

	a := &Asset{
		Kind: "character", Name: "老练的佣兵", Tags: []string{"战斗", "中立"},
		Summary: "可复用的佣兵模板", Payload: []byte(`{"name":"老练的佣兵"}`),
	}
	if err := store.Create(a); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if a.ID == "" {
		t.Fatal("应生成 ID")
	}

	got, err := store.Get(a.ID)
	if err != nil || string(got.Payload) != `{"name":"老练的佣兵"}` {
		t.Fatalf("Get: %+v %v", got, err)
	}

	// 过滤：kind / q / tag
	list, _ := store.List("character", "", "")
	if len(list) != 1 || list[0].Payload != nil {
		t.Fatalf("List 不应含 payload: %+v", list)
	}
	if l, _ := store.List("item", "", ""); len(l) != 0 {
		t.Fatal("kind 过滤失效")
	}
	if l, _ := store.List("", "佣兵", ""); len(l) != 1 {
		t.Fatal("名称模糊过滤失效")
	}
	if l, _ := store.List("", "", "战斗"); len(l) != 1 {
		t.Fatal("标签过滤失效")
	}
	if l, _ := store.List("", "", "魔法"); len(l) != 0 {
		t.Fatal("标签过滤失效")
	}

	got.Summary = "更新后的摘要"
	if err := store.Update(got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := store.Delete(a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Get(a.ID); err == nil {
		t.Fatal("删除后不应能 Get")
	}
}

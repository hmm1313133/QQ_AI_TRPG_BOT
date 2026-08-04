// 素材联动与游玩存档 API 测试（《世界编辑器与素材联动设计.md》§四/§9.4）。
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/character"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// newAssetTestServer 启动 SQLite 存储 + 素材库的管理后台测试服务。
func newAssetTestServer(t *testing.T) (*httptest.Server, AdminDeps) {
	t.Helper()
	db, err := world.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	repo, err := world.NewSQLiteRepository(db)
	if err != nil {
		t.Fatalf("创建世界仓库失败: %v", err)
	}
	assetStore, err := world.NewAssetStore(db)
	if err != nil {
		t.Fatalf("创建素材库失败: %v", err)
	}
	charMgr, err := character.NewSQLiteManager(db)
	if err != nil {
		t.Fatalf("创建角色卡管理器失败: %v", err)
	}
	archive, err := script.NewArchive(t.TempDir())
	if err != nil {
		t.Fatalf("创建剧本存档失败: %v", err)
	}

	sessions := core.NewSessionManager()
	router := core.NewRouter(core.NewPluginManager(), sessions, nil)
	srv := NewServer(Config{UploadDir: t.TempDir()}, router, sessions)
	deps := AdminDeps{
		Sessions:    sessions,
		WorldEngine: world.NewEngine(repo),
		Archive:     archive,
		CharMgr:     charMgr,
		AssetStore:  assetStore,
		StartTime:   time.Now(),
	}
	srv.SetAdmin(deps, "tok")
	return httptest.NewServer(srv.Handler()), deps
}

func readJSON(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
}

// 素材库 CRUD → 收藏 → 导入 全链路。
func TestAdmin_AssetLibraryFlow(t *testing.T) {
	ts, deps := newAssetTestServer(t)
	defer ts.Close()

	// 手动创建素材
	resp := adminReq(t, "POST", ts.URL+"/api/admin/assets/library",
		`{"kind":"location","name":"寒鸦堡","tags":["城堡"],"summary":"北境要塞","payload":{"id":"loc_x","name":"寒鸦堡","description":"阴森的石堡","atmosphere":"肃杀","danger":"高危","points":["瞭望塔"]}}`)
	var asset world.Asset
	readJSON(t, resp, &asset)
	if asset.ID == "" {
		t.Fatal("创建素材应返回 ID")
	}

	// 非法类型 → 400
	resp = adminReq(t, "POST", ts.URL+"/api/admin/assets/library", `{"kind":"dragon","name":"x","payload":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 kind 应 400: %d", resp.StatusCode)
	}

	// 创建目标世界（simrpg）
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds",
		`{"world_id":"w_imp","mode":"simrpg","background":"测试世界"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("创建世界失败: %d", resp.StatusCode)
	}

	// 从素材库导入地点
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w_imp/assets/import",
		`{"library":["`+asset.ID+`"]}`)
	var result map[string]any
	readJSON(t, resp, &result)
	if int(result["imported"].(float64)) != 1 {
		t.Fatalf("应导入 1 项: %+v", result)
	}

	// 重复导入 → 冲突跳过
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w_imp/assets/import",
		`{"library":["`+asset.ID+`"]}`)
	readJSON(t, resp, &result)
	if len(result["conflicts"].([]any)) != 1 {
		t.Fatalf("重复导入应报冲突: %+v", result)
	}

	// overwrite 强制覆盖
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w_imp/assets/import",
		`{"library":["`+asset.ID+`"],"on_conflict":"overwrite"}`)
	readJSON(t, resp, &result)
	if int(result["imported"].(float64)) != 1 {
		t.Fatalf("overwrite 应导入: %+v", result)
	}

	// 校验落库
	ws := deps.WorldEngine.LoadOrNil("w_imp")
	if loc := ws.Locations["寒鸦堡"]; loc == nil || loc.Atmosphere != "肃杀" {
		t.Fatalf("导入的地点未落库: %+v", ws.Locations)
	}

	// 收藏回素材库（世界实体 → 素材）
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w_imp/assets/collect",
		`{"kind":"location","name":"寒鸦堡","tags":["收藏"]}`)
	var collected world.Asset
	readJSON(t, resp, &collected)
	if collected.Source == "" || collected.Name != "寒鸦堡" {
		t.Fatalf("收藏素材元数据错误: %+v", collected)
	}

	// 人物卡关联导入
	if err := deps.CharMgr.Create(&character.Card{
		Name: "佣兵雷恩", Player: "p1", System: "coc7",
		Skills: map[string]int{"剑术": 70}, Backstory: "退役老兵", Personality: "沉默寡言",
	}); err != nil {
		t.Fatal(err)
	}
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w_imp/assets/import",
		`{"cards":["p1:佣兵雷恩"]}`)
	readJSON(t, resp, &result)
	if int(result["imported"].(float64)) != 1 {
		t.Fatalf("人物卡导入失败: %+v", result)
	}
	ws = deps.WorldEngine.LoadOrNil("w_imp")
	npc := ws.Characters["佣兵雷恩"]
	if npc == nil || npc.CardRef != "p1:佣兵雷恩" || npc.Backstory != "退役老兵" || len(npc.Skills) != 1 {
		t.Fatalf("人物卡关联映射错误: %+v", npc)
	}

	// 素材目录聚合
	resp = adminReq(t, "GET", ts.URL+"/api/admin/assets", "")
	var catalog map[string]any
	readJSON(t, resp, &catalog)
	if len(catalog["library"].([]any)) != 2 || len(catalog["cards"].([]any)) != 1 || len(catalog["worlds"].([]any)) != 1 {
		t.Fatalf("素材目录聚合错误: %+v", catalog)
	}
}

// 存档：新建 → 推进后恢复 → 自动备份。
func TestAdmin_SavesFlow(t *testing.T) {
	ts, deps := newAssetTestServer(t)
	defer ts.Close()

	resp := adminReq(t, "POST", ts.URL+"/api/admin/worlds",
		`{"world_id":"w_save","mode":"simrpg","background":"测试世界"}`)
	resp.Body.Close()

	// 新建存档
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w_save/saves", `{"name":"开局","note":"初始状态"}`)
	var save world.SaveInfo
	readJSON(t, resp, &save)
	if save.ID == 0 {
		t.Fatal("应返回存档 ID")
	}

	// 改状态（轮次 +5）
	ws := deps.WorldEngine.LoadOrNil("w_save")
	ws.RoundCount = 5
	if err := deps.WorldEngine.Save(ws); err != nil {
		t.Fatal(err)
	}

	// 恢复
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w_save/saves/1/restore", "")
	var msg map[string]string
	readJSON(t, resp, &msg)
	ws = deps.WorldEngine.LoadOrNil("w_save")
	if ws.RoundCount != 0 {
		t.Fatalf("恢复后应为轮次 0，实际 %d", ws.RoundCount)
	}

	// 恢复前自动备份已生成
	resp = adminReq(t, "GET", ts.URL+"/api/admin/worlds/w_save/saves", "")
	var saves []world.SaveInfo
	readJSON(t, resp, &saves)
	autoFound := false
	for _, s := range saves {
		if s.Auto {
			autoFound = true
			if s.RoundCount != 5 {
				t.Fatalf("自动备份应记录轮次 5，实际 %d", s.RoundCount)
			}
		}
	}
	if !autoFound {
		t.Fatal("恢复前应自动备份当前进度")
	}

	// 恢复事件留痕
	noteFound := false
	for _, ev := range ws.EventLog {
		if ev.Type == "note" && ev.Target == "save:restore" {
			noteFound = true
		}
	}
	if !noteFound {
		t.Fatal("恢复应记审计事件")
	}

	// 删除存档
	resp = adminReq(t, "DELETE", ts.URL+"/api/admin/worlds/w_save/saves/1", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("删除存档失败: %d", resp.StatusCode)
	}
}

// 管理后台扩展 P1：人物卡/剧本/世界手动管理端点测试（见《管理后台扩展设计.md》2.3/2.4/2.5）。
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/script"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/character"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// newMgmtTestServer 启动带真实 CharMgr/Archive/Engine（均用 t.TempDir()）的管理后台测试服务。
func newMgmtTestServer(t *testing.T) (*httptest.Server, AdminDeps) {
	t.Helper()
	charMgr, err := character.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("创建角色卡管理器失败: %v", err)
	}
	archive, err := script.NewArchive(t.TempDir())
	if err != nil {
		t.Fatalf("创建剧本存档失败: %v", err)
	}
	repo, err := world.NewJSONRepository(t.TempDir())
	if err != nil {
		t.Fatalf("创建世界仓库失败: %v", err)
	}

	sessions := core.NewSessionManager()
	router := core.NewRouter(core.NewPluginManager(), sessions, nil)
	srv := NewServer(Config{UploadDir: t.TempDir()}, router, sessions)
	deps := AdminDeps{
		Sessions:    sessions,
		WorldEngine: world.NewEngine(repo),
		Archive:     archive,
		CharMgr:     charMgr,
		StartTime:   time.Now(),
	}
	srv.SetAdmin(deps, "tok")
	return httptest.NewServer(srv.Handler()), deps
}

// ============================================================
// 人物卡
// ============================================================

func TestAdmin_CharacterCreate(t *testing.T) {
	ts, deps := newMgmtTestServer(t)
	defer ts.Close()

	// 缺 system → 400
	resp := adminReq(t, "POST", ts.URL+"/api/admin/characters", `{"name":"Alice","player":"p1"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺 system 应 400: %d", resp.StatusCode)
	}
	// 非法 system → 400
	resp = adminReq(t, "POST", ts.URL+"/api/admin/characters", `{"name":"Alice","player":"p1","system":"gurps"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 system 应 400: %d", resp.StatusCode)
	}
	// 缺 name → 400
	resp = adminReq(t, "POST", ts.URL+"/api/admin/characters", `{"player":"p1","system":"coc7"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺 name 应 400: %d", resp.StatusCode)
	}

	// 正常创建 → 200
	resp = adminReq(t, "POST", ts.URL+"/api/admin/characters",
		`{"name":"Alice","player":"p1","system":"coc7","attrs":{"str":50},"backstory":"调查员"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("创建应 200: %d", resp.StatusCode)
	}
	card, err := deps.CharMgr.Get("p1:Alice")
	if err != nil {
		t.Fatalf("角色卡应已创建: %v", err)
	}
	if card.System != "coc7" || card.Attrs["str"] != 50 || card.Backstory != "调查员" {
		t.Fatalf("角色卡字段不正确: %+v", card)
	}
	if card.Skills == nil || card.Status == nil {
		t.Fatal("缺省 map 应初始化为空而非 nil")
	}

	// 重复创建 → 409
	resp2 := adminReq(t, "POST", ts.URL+"/api/admin/characters",
		`{"name":"Alice","player":"p1","system":"coc7"}`)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("重复创建应 409: %d", resp2.StatusCode)
	}
}

func TestAdmin_CharacterUpdate(t *testing.T) {
	ts, deps := newMgmtTestServer(t)
	defer ts.Close()

	resp := adminReq(t, "POST", ts.URL+"/api/admin/characters",
		`{"name":"Alice","player":"p1","system":"coc7","attrs":{"str":50}}`)
	resp.Body.Close()

	// 改 name/system/backstory + map 整体替换 → 200
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/characters/p1:Alice",
		`{"name":"Alice II","system":"dnd5e","backstory":"圣武士","attrs":{"dex":40},"skills":{"侦查":60}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("更新应 200: %d", resp.StatusCode)
	}
	card, _ := deps.CharMgr.Get("p1:Alice") // ID 不可变
	if card.Name != "Alice II" || card.System != "dnd5e" || card.Backstory != "圣武士" {
		t.Fatalf("基本字段未更新: %+v", card)
	}
	// 传了的 map 整体替换（str 被移除），未传的 status 不动
	if _, ok := card.Attrs["str"]; ok || card.Attrs["dex"] != 40 || card.Skills["侦查"] != 60 {
		t.Fatalf("map 应整体替换: %+v", card)
	}

	// 非法 system → 400
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/characters/p1:Alice", `{"system":"gurps"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 system 应 400: %d", resp.StatusCode)
	}
	// 不存在 → 404
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/characters/p1:nobody", `{"attrs":{"str":1}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("不存在的卡应 404: %d", resp.StatusCode)
	}
}

func TestAdmin_CharacterDelete(t *testing.T) {
	ts, deps := newMgmtTestServer(t)
	defer ts.Close()

	resp := adminReq(t, "POST", ts.URL+"/api/admin/characters",
		`{"name":"Alice","player":"p1","system":"coc7"}`)
	resp.Body.Close()

	resp = adminReq(t, "DELETE", ts.URL+"/api/admin/characters/p1:Alice", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("删除应 200: %d", resp.StatusCode)
	}
	if _, err := deps.CharMgr.Get("p1:Alice"); err == nil {
		t.Fatal("角色卡应已删除")
	}
	// 再删 → 404
	resp = adminReq(t, "DELETE", ts.URL+"/api/admin/characters/p1:Alice", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("重复删除应 404: %d", resp.StatusCode)
	}
}

// ============================================================
// 剧本
// ============================================================

func TestAdmin_ScriptCreate(t *testing.T) {
	ts, deps := newMgmtTestServer(t)
	defer ts.Close()

	// 缺 title → 400
	resp := adminReq(t, "POST", ts.URL+"/api/admin/scripts",
		`{"name":"my script","system":"coc7","timeline":[{"id":"n1","name":"开场"}]}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺 title 应 400: %d", resp.StatusCode)
	}
	// 空 timeline → 400
	resp = adminReq(t, "POST", ts.URL+"/api/admin/scripts",
		`{"name":"my script","title":"我的剧本","system":"coc7"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("空 timeline 应 400: %d", resp.StatusCode)
	}
	// 节点 ID 重复 → 400
	resp = adminReq(t, "POST", ts.URL+"/api/admin/scripts",
		`{"name":"my script","title":"我的剧本","system":"coc7","timeline":[{"id":"n1"},{"id":"n1"}]}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("节点 ID 重复应 400: %d", resp.StatusCode)
	}

	// 成功：传入 ID 被忽略，按名称生成；Order 为 0 自动补齐
	resp = adminReq(t, "POST", ts.URL+"/api/admin/scripts",
		`{"id":"hacker-id","name":"my script","title":"我的剧本","system":"coc7","timeline":[{"id":"n1","name":"开场"},{"id":"n2","name":"高潮","order":5},{"id":"n3","name":"结局"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("创建应 200: %d", resp.StatusCode)
	}
	var created script.Script
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if created.ID != "my_script" {
		t.Fatalf("ID 应按名称生成（忽略传入 id）: %s", created.ID)
	}
	if created.CreatedAt == "" {
		t.Fatal("应补 CreatedAt")
	}
	if created.Timeline[0].Order != 1 || created.Timeline[1].Order != 5 || created.Timeline[2].Order != 6 {
		t.Fatalf("Order 应自动补齐: %+v", created.Timeline)
	}
	if _, err := deps.Archive.Get("my_script"); err != nil {
		t.Fatalf("剧本应已入库: %v", err)
	}

	// 同名再建 → 409
	resp2 := adminReq(t, "POST", ts.URL+"/api/admin/scripts",
		`{"name":"my script","title":"我的剧本","system":"coc7","timeline":[{"id":"n1"}]}`)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Fatalf("ID 冲突应 409: %d", resp2.StatusCode)
	}
}

func TestAdmin_ScriptReplace(t *testing.T) {
	ts, deps := newMgmtTestServer(t)
	defer ts.Close()

	resp := adminReq(t, "POST", ts.URL+"/api/admin/scripts",
		`{"name":"my script","title":"我的剧本","system":"coc7","timeline":[{"id":"n1","name":"开场"}]}`)
	resp.Body.Close()

	// 不存在 → 404
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/scripts/no_such",
		`{"name":"no such","title":"x","system":"coc7","timeline":[{"id":"n1"}]}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("不存在应 404: %d", resp.StatusCode)
	}
	// URL id 与 body 名称生成的 ID 不一致 → 400
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/scripts/my_script",
		`{"name":"other name","title":"x","system":"coc7","timeline":[{"id":"n1"}]}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("ID 不一致应 400: %d", resp.StatusCode)
	}
	// 校验失败（非法 system）→ 400
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/scripts/my_script",
		`{"name":"my script","title":"x","system":"gurps","timeline":[{"id":"n1"}]}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 system 应 400: %d", resp.StatusCode)
	}

	// 整体替换 → 200
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/scripts/my_script",
		`{"name":"my script","title":"我的剧本·改","system":"dnd5e","timeline":[{"id":"n1","name":"新开场"},{"id":"n2","name":"新结局"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("替换应 200: %d", resp.StatusCode)
	}
	got, err := deps.Archive.Get("my_script")
	if err != nil {
		t.Fatalf("剧本应存在: %v", err)
	}
	if got.Title != "我的剧本·改" || got.System != "dnd5e" || len(got.Timeline) != 2 {
		t.Fatalf("整体替换未生效: %+v", got)
	}
}

// ============================================================
// 世界
// ============================================================

func TestAdmin_WorldCreate(t *testing.T) {
	ts, deps := newMgmtTestServer(t)
	defer ts.Close()

	// 缺 mode → 400
	resp := adminReq(t, "POST", ts.URL+"/api/admin/worlds", `{"world_id":"w0"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺 mode 应 400: %d", resp.StatusCode)
	}
	// roleplay 缺 NPC → 400
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds", `{"world_id":"w1","mode":"roleplay","background":"x"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("roleplay 缺 NPC 应 400: %d", resp.StatusCode)
	}
	// trpg 缺 script_id → 400
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds", `{"world_id":"w2","mode":"trpg"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("trpg 缺 script_id 应 400: %d", resp.StatusCode)
	}
	// trpg script_id 取不到 → 400
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds", `{"world_id":"w2","mode":"trpg","script_id":"no_such"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("剧本取不到应 400: %d", resp.StatusCode)
	}

	// trpg 成功：先入库剧本
	scr := &script.Script{
		ID: "s1", Name: "测试剧本", Title: "测试剧本", System: "coc7",
		Timeline: []script.TimelineNode{{ID: "node_1", Name: "开场", Order: 1}},
	}
	if err := deps.Archive.Save(scr); err != nil {
		t.Fatalf("剧本入库失败: %v", err)
	}
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds", `{"world_id":"w2","mode":"trpg","script_id":"s1"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("trpg 创建应 200: %d", resp.StatusCode)
	}
	ws := deps.WorldEngine.LoadOrNil("w2")
	if ws == nil || ws.ScriptID != "s1" || ws.Scene.NodeID != "node_1" {
		t.Fatalf("trpg 世界应复用 InitFromScript: %+v", ws)
	}

	// 重复 world_id → 409
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds", `{"world_id":"w2","mode":"simrpg"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("重复世界应 409: %d", resp.StatusCode)
	}

	// roleplay 成功 + world_id 缺省自动生成
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds",
		`{"mode":"roleplay","background":"赛博都市","npcs":[{"name":"搭档","disposition":"friendly"}]}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("roleplay 创建应 200: %d", resp.StatusCode)
	}
	var state world.WorldState
	if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if state.WorldID == "" || state.Characters["搭档"] == nil {
		t.Fatalf("应自动生成 world_id 并播种 NPC: %+v", state)
	}
}

func TestAdmin_WorldDelete(t *testing.T) {
	ts, deps := newMgmtTestServer(t)
	defer ts.Close()

	if _, err := deps.WorldEngine.SeedWorld("w-free", world.SeedSpec{Mode: world.ModeSimRPG}, nil); err != nil {
		t.Fatalf("播种失败: %v", err)
	}
	if _, err := deps.WorldEngine.SeedWorld("w-busy", world.SeedSpec{Mode: world.ModeSimRPG}, nil); err != nil {
		t.Fatalf("播种失败: %v", err)
	}
	deps.Sessions.GetSession("w-busy") // 会话 ID == 世界 ID，视为占用

	// 被占用 → 409
	resp := adminReq(t, "DELETE", ts.URL+"/api/admin/worlds/w-busy", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("被占用世界应 409: %d", resp.StatusCode)
	}
	if deps.WorldEngine.LoadOrNil("w-busy") == nil {
		t.Fatal("被占用世界不应被删除")
	}

	// 空闲 → 200
	resp = adminReq(t, "DELETE", ts.URL+"/api/admin/worlds/w-free", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("删除应 200: %d", resp.StatusCode)
	}
	if deps.WorldEngine.LoadOrNil("w-free") != nil {
		t.Fatal("世界应已删除")
	}

	// 不存在 → 404
	resp = adminReq(t, "DELETE", ts.URL+"/api/admin/worlds/w-free", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("不存在应 404: %d", resp.StatusCode)
	}
}

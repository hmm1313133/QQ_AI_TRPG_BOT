// 管理后台：世界设定库（lore）与分区编辑 API 测试（设计文档 §4.4/§4.6 P3）。
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// createLoreTestWorld 建一个 simrpg 测试世界。
func createLoreTestWorld(t *testing.T, ts *httptest.Server, id string) {
	t.Helper()
	resp := adminReq(t, "POST", ts.URL+"/api/admin/worlds", `{"world_id":"`+id+`","mode":"simrpg"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("创建世界失败: %d", resp.StatusCode)
	}
}

// lastEvent 取世界 EventLog 最后一条事件。
func lastEvent(t *testing.T, deps AdminDeps, worldID string) world.WorldEvent {
	t.Helper()
	ws := deps.WorldEngine.LoadOrNil(worldID)
	if ws == nil || len(ws.EventLog) == 0 {
		t.Fatalf("世界 %s 无事件日志", worldID)
	}
	return ws.EventLog[len(ws.EventLog)-1]
}

func TestAdmin_LoreCRUD(t *testing.T) {
	ts, deps := newMgmtTestServer(t)
	defer ts.Close()
	createLoreTestWorld(t, ts, "w1")

	// 缺 title → 400
	resp := adminReq(t, "POST", ts.URL+"/api/admin/worlds/w1/lore", `{"content":"正文"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺 title 应 400: %d", resp.StatusCode)
	}
	// 缺 content → 400
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w1/lore", `{"title":"标题"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺 content 应 400: %d", resp.StatusCode)
	}

	// 正常新增 → 200，ID lor_ 前缀，默认值生效
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w1/lore",
		`{"title":"寒鸦堡","content":"北境的边境要塞","keys":["寒鸦堡","要塞"]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("新增应 200: %d", resp.StatusCode)
	}
	var entry world.LoreEntry
	json.NewDecoder(resp.Body).Decode(&entry)
	resp.Body.Close()
	if !strings.HasPrefix(entry.ID, "lor_") {
		t.Fatalf("ID 应有 lor_ 前缀: %s", entry.ID)
	}
	if entry.Category != "background" || entry.Position != "front" || entry.Priority != 50 || !entry.Enabled {
		t.Fatalf("默认值不正确: %+v", entry)
	}
	if entry.Source != "manual" {
		t.Fatalf("Source 应为 manual: %s", entry.Source)
	}
	// 审计 note 事件
	if ev := lastEvent(t, deps, "w1"); ev.Type != "note" || ev.Actor != "admin" || ev.Target != "lore:"+entry.ID {
		t.Fatalf("应写入 note 审计事件: %+v", ev)
	}

	// 列表 + category/enabled 过滤
	resp = adminReq(t, "GET", ts.URL+"/api/admin/worlds/w1/lore", "")
	var list []world.LoreEntry
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 1 {
		t.Fatalf("列表应 1 条: %d", len(list))
	}
	resp = adminReq(t, "GET", ts.URL+"/api/admin/worlds/w1/lore?category=geo", "")
	list = nil
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 0 {
		t.Fatalf("category=geo 应过滤为空: %d", len(list))
	}
	resp = adminReq(t, "GET", ts.URL+"/api/admin/worlds/w1/lore?enabled=false", "")
	list = nil
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list) != 0 {
		t.Fatalf("enabled=false 应过滤为空: %d", len(list))
	}

	// 更新：改标题/停用/优先级；ID 与 Source 不可变
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/worlds/w1/lore/"+entry.ID,
		`{"title":"寒鸦堡（改）","content":"新正文","enabled":false,"priority":80,"source":"script"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("更新应 200: %d", resp.StatusCode)
	}
	var updated world.LoreEntry
	json.NewDecoder(resp.Body).Decode(&updated)
	resp.Body.Close()
	if updated.ID != entry.ID || updated.Title != "寒鸦堡（改）" || updated.Enabled || updated.Priority != 80 {
		t.Fatalf("更新结果不正确: %+v", updated)
	}
	if updated.Source != "manual" {
		t.Fatalf("Source 不应被修改: %s", updated.Source)
	}
	// 更新不存在的条目 → 404
	resp = adminReq(t, "PUT", ts.URL+"/api/admin/worlds/w1/lore/lor_none",
		`{"title":"x","content":"y"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("更新不存在条目应 404: %d", resp.StatusCode)
	}

	// 删除
	resp = adminReq(t, "DELETE", ts.URL+"/api/admin/worlds/w1/lore/"+entry.ID, "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("删除应 200: %d", resp.StatusCode)
	}
	ws := deps.WorldEngine.LoadOrNil("w1")
	if len(ws.Lore) != 0 {
		t.Fatalf("删除后应为空: %d", len(ws.Lore))
	}
	if ev := lastEvent(t, deps, "w1"); ev.Type != "note" || !strings.Contains(ev.Value, "删除") {
		t.Fatalf("删除应有 note 审计事件: %+v", ev)
	}
	// 删除不存在 → 404
	resp = adminReq(t, "DELETE", ts.URL+"/api/admin/worlds/w1/lore/lor_none", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("删除不存在条目应 404: %d", resp.StatusCode)
	}
}

func TestAdmin_LoreTest(t *testing.T) {
	ts, _ := newMgmtTestServer(t)
	defer ts.Close()
	createLoreTestWorld(t, ts, "w1")

	// 一条恒定 + 一条关键词条目
	resp := adminReq(t, "POST", ts.URL+"/api/admin/worlds/w1/lore",
		`{"title":"主线","content":"恒定的世界主线","constant":true}`)
	resp.Body.Close()
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w1/lore",
		`{"title":"寒鸦堡","content":"北境的边境要塞","keys":["寒鸦堡"],"priority":80}`)
	resp.Body.Close()

	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w1/lore/test", `{"text":"我前往寒鸦堡"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("命中测试应 200: %d", resp.StatusCode)
	}
	var result struct {
		Budget int             `json:"budget"`
		Front  []world.LoreHit `json:"front"`
		Tail   []world.LoreHit `json:"tail"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	if result.Budget != world.DefaultLoreBudget {
		t.Fatalf("默认预算应为 %d: %d", world.DefaultLoreBudget, result.Budget)
	}
	if len(result.Front) != 2 {
		t.Fatalf("应命中 2 条: %+v", result.Front)
	}
	// Priority 降序：寒鸦堡(80) 在前，恒定主线(50) 在后
	if result.Front[0].Entry.Title != "寒鸦堡" || !strings.Contains(result.Front[0].Reason, "寒鸦堡") {
		t.Fatalf("首条应为关键词命中的寒鸦堡: %+v", result.Front[0])
	}
	if result.Front[1].Reason != "恒定" {
		t.Fatalf("次条应为恒定命中: %+v", result.Front[1])
	}
	if result.Front[0].Chars != len(result.Front[0].Entry.Content) {
		t.Fatalf("预算记账应为正文字符数: %+v", result.Front[0])
	}

	// 不存在的世界 → 404
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/none/lore/test", `{"text":"x"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("世界不存在应 404: %d", resp.StatusCode)
	}
}

func TestAdmin_LoreImportText(t *testing.T) {
	ts, deps := newMgmtTestServer(t)
	defer ts.Close()
	createLoreTestWorld(t, ts, "w1")

	resp := adminReq(t, "POST", ts.URL+"/api/admin/worlds/w1/lore/import",
		`{"text":"寒鸦堡\n北境的边境要塞。\n\n\n\n白狼团\n活跃于雪原的佣兵团。"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("文本导入应 200: %d", resp.StatusCode)
	}
	var drafts []world.LoreEntry
	json.NewDecoder(resp.Body).Decode(&drafts)
	resp.Body.Close()
	if len(drafts) != 2 {
		t.Fatalf("应拆成 2 条: %d", len(drafts))
	}
	if drafts[0].Title != "寒鸦堡" || drafts[0].Content != "寒鸦堡\n北境的边境要塞。" {
		t.Fatalf("首条标题/正文不正确: %+v", drafts[0])
	}
	if drafts[0].Source != "manual" || !drafts[0].Enabled || !strings.HasPrefix(drafts[0].ID, "lor_") {
		t.Fatalf("草稿默认值不正确: %+v", drafts[0])
	}
	if len(deps.WorldEngine.LoadOrNil("w1").Lore) != 2 {
		t.Fatal("条目应已写入世界")
	}
	if ev := lastEvent(t, deps, "w1"); ev.Type != "note" {
		t.Fatalf("导入应有 note 审计事件: %+v", ev)
	}

	// 空 text 且无 entries → 400
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w1/lore/import", `{"text":"  "}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("空导入应 400: %d", resp.StatusCode)
	}
}

func TestAdmin_LoreImportSTLorebook(t *testing.T) {
	ts, _ := newMgmtTestServer(t)
	defer ts.Close()
	createLoreTestWorld(t, ts, "w1")

	// 对象形态（ST 官方：uid -> entry）
	resp := adminReq(t, "POST", ts.URL+"/api/admin/worlds/w1/lore/import", `{"entries":{
		"0":{"name":"北境","keys":["北境","雪原"],"secondary_keys":["狼"],"constant":true,
			"insertion_order":80,"content":"北境终年积雪","enabled":true},
		"1":{"comment":"白狼团","keys":["白狼团"],"insertion_order":60,"content":"雪原佣兵团"},
		"2":{"name":"空条目","content":"  "}
	}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ST 导入应 200: %d", resp.StatusCode)
	}
	var drafts []world.LoreEntry
	json.NewDecoder(resp.Body).Decode(&drafts)
	resp.Body.Close()
	if len(drafts) != 2 { // 空正文条目跳过
		t.Fatalf("应导入 2 条: %+v", drafts)
	}
	var north *world.LoreEntry
	for i := range drafts {
		if drafts[i].Title == "北境" {
			north = &drafts[i]
		}
	}
	if north == nil {
		t.Fatalf("缺少北境条目: %+v", drafts)
	}
	if !north.Constant || north.Priority != 80 || len(north.Keys) != 2 ||
		len(north.SecondaryKeys) != 1 || north.Position != "front" {
		t.Fatalf("ST 字段映射不正确: %+v", north)
	}

	// 数组形态
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w1/lore/import",
		`{"entries":[{"name":"南境","keys":["南境"],"content":"温暖的南境","enabled":false}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("数组形态导入应 200: %d", resp.StatusCode)
	}
	drafts = nil
	json.NewDecoder(resp.Body).Decode(&drafts)
	resp.Body.Close()
	if len(drafts) != 1 || drafts[0].Title != "南境" || drafts[0].Enabled {
		t.Fatalf("数组形态映射不正确: %+v", drafts)
	}

	// entries 非法 JSON → 400
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds/w1/lore/import", `{"entries":"not-an-object"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 entries 应 400: %d", resp.StatusCode)
	}
}

func TestAdmin_LoreInjections(t *testing.T) {
	// 无 TurnEngine → 503
	ts, _ := newMgmtTestServer(t)
	defer ts.Close()
	createLoreTestWorld(t, ts, "w1")
	resp := adminReq(t, "GET", ts.URL+"/api/admin/worlds/w1/lore/injections", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("无 TurnEngine 应 503: %d", resp.StatusCode)
	}

	// 有 TurnEngine → 200（无回合记录时返回空数组）
	sessions := core.NewSessionManager()
	router := core.NewRouter(core.NewPluginManager(), sessions, nil)
	srv := NewServer(Config{UploadDir: t.TempDir()}, router, sessions)
	repo, err := world.NewJSONRepository(t.TempDir())
	if err != nil {
		t.Fatalf("创建世界仓库失败: %v", err)
	}
	engine := world.NewEngine(repo)
	srv.SetAdmin(AdminDeps{
		Sessions:    sessions,
		WorldEngine: engine,
		TurnEngine:  agent.NewTurnEngine(nil, nil, nil, engine, nil, 0),
		StartTime:   time.Now(),
	}, "tok")
	ts2 := httptest.NewServer(srv.Handler())
	defer ts2.Close()

	resp = adminReq(t, "GET", ts2.URL+"/api/admin/worlds/w1/lore/injections", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("有 TurnEngine 应 200: %d", resp.StatusCode)
	}
	var records []world.LoreResult
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if records == nil {
		t.Fatal("无记录时应返回空数组而非 null")
	}
}

func TestAdmin_Section(t *testing.T) {
	ts, deps := newMgmtTestServer(t)
	defer ts.Close()
	createLoreTestWorld(t, ts, "w1")

	// GET scene 分区
	resp := adminReq(t, "GET", ts.URL+"/api/admin/worlds/w1/section?part=scene", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET scene 应 200: %d", resp.StatusCode)
	}
	var scene world.SceneState
	json.NewDecoder(resp.Body).Decode(&scene)
	resp.Body.Close()

	// GET 未知分区 → 400
	resp = adminReq(t, "GET", ts.URL+"/api/admin/worlds/w1/section?part=bad", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("未知分区应 400: %d", resp.StatusCode)
	}

	// PATCH scene
	resp = adminReq(t, "PATCH", ts.URL+"/api/admin/worlds/w1/section",
		`{"part":"scene","data":{"node_id":"n1","node_name":"寒鸦堡大厅","description":"阴冷的大厅"}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH scene 应 200: %d", resp.StatusCode)
	}
	ws := deps.WorldEngine.LoadOrNil("w1")
	if ws.Scene.NodeName != "寒鸦堡大厅" {
		t.Fatalf("scene 未生效: %+v", ws.Scene)
	}
	if ev := lastEvent(t, deps, "w1"); ev.Type != "note" || ev.Target != "section:scene" {
		t.Fatalf("PATCH 应有 note 审计事件: %+v", ev)
	}

	// PATCH scene 空名称 → 400
	resp = adminReq(t, "PATCH", ts.URL+"/api/admin/worlds/w1/section",
		`{"part":"scene","data":{"node_name":"  "}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("空 scene 名称应 400: %d", resp.StatusCode)
	}

	// PATCH characters：map 键回填 Name
	resp = adminReq(t, "PATCH", ts.URL+"/api/admin/worlds/w1/section",
		`{"part":"characters","data":{"老王":{"kind":"npc","alive":true,"disposition":"friendly"}}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH characters 应 200: %d", resp.StatusCode)
	}
	ws = deps.WorldEngine.LoadOrNil("w1")
	if ws.Characters["老王"] == nil || ws.Characters["老王"].Name != "老王" {
		t.Fatalf("characters 未生效: %+v", ws.Characters)
	}

	// PATCH 未知分区 → 400
	resp = adminReq(t, "PATCH", ts.URL+"/api/admin/worlds/w1/section",
		`{"part":"bad","data":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("未知分区应 400: %d", resp.StatusCode)
	}

	// GET characters 分区能读回
	resp = adminReq(t, "GET", ts.URL+"/api/admin/worlds/w1/section?part=characters", "")
	var chars map[string]*world.CharacterState
	json.NewDecoder(resp.Body).Decode(&chars)
	resp.Body.Close()
	if len(chars) != 1 || chars["老王"].Disposition != "friendly" {
		t.Fatalf("GET characters 不正确: %+v", chars)
	}
}

func TestAdmin_WorldCreateWithLore(t *testing.T) {
	ts, deps := newMgmtTestServer(t)
	defer ts.Close()

	// 随世界创建写入 lore
	resp := adminReq(t, "POST", ts.URL+"/api/admin/worlds",
		`{"world_id":"w1","mode":"simrpg","lore":[{"title":"寒鸦堡","content":"北境要塞","keys":["寒鸦堡"],"priority":70}]}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("创建应 200: %d", resp.StatusCode)
	}
	resp.Body.Close()
	ws := deps.WorldEngine.LoadOrNil("w1")
	if len(ws.Lore) != 1 {
		t.Fatalf("应写入 1 条 lore: %d", len(ws.Lore))
	}
	e := ws.Lore[0]
	if !strings.HasPrefix(e.ID, "lor_") || e.Source != "manual" || e.Priority != 70 || !e.Enabled {
		t.Fatalf("创建向导 lore 不正确: %+v", e)
	}
	if ev := lastEvent(t, deps, "w1"); ev.Type != "note" {
		t.Fatalf("应有 note 审计事件: %+v", ev)
	}

	// lore 校验失败 → 400 且世界不应创建
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds",
		`{"world_id":"w2","mode":"simrpg","lore":[{"content":"无标题"}]}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("lore 校验失败应 400: %d", resp.StatusCode)
	}
	if deps.WorldEngine.LoadOrNil("w2") != nil {
		t.Fatal("校验失败不应创建世界")
	}
}

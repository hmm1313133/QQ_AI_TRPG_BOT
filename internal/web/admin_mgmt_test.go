// 管理后台：世界手动管理端点测试（见《管理后台扩展设计.md》2.5）。
package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// newMgmtTestServer 启动带真实世界引擎（t.TempDir()）的管理后台测试服务。
func newMgmtTestServer(t *testing.T) (*httptest.Server, AdminDeps) {
	t.Helper()
	repo, err := world.NewJSONRepository(t.TempDir())
	if err != nil {
		t.Fatalf("创建世界仓库失败: %v", err)
	}

	sessions := core.NewSessionManager()
	router := core.NewRouter(core.NewPluginManager(), sessions)
	srv := NewServer(Config{}, router, sessions)
	deps := AdminDeps{
		Sessions:    sessions,
		WorldEngine: world.NewEngine(repo),
		StartTime:   time.Now(),
	}
	srv.SetAdmin(deps, "tok")
	return httptest.NewServer(srv.Handler()), deps
}

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
	// 未知模式 → 400
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds", `{"world_id":"w2","mode":"trpg"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("未知模式应 400: %d", resp.StatusCode)
	}

	// simrpg 成功
	resp = adminReq(t, "POST", ts.URL+"/api/admin/worlds",
		`{"world_id":"w2","mode":"simrpg","background":"剑与魔法"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("simrpg 创建应 200: %d", resp.StatusCode)
	}
	if ws := deps.WorldEngine.LoadOrNil("w2"); ws == nil || ws.Mode != world.ModeSimRPG {
		t.Fatalf("simrpg 世界应已创建: %+v", ws)
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

	if _, err := deps.WorldEngine.SeedWorld("w-free", world.SeedSpec{Mode: world.ModeSimRPG}); err != nil {
		t.Fatalf("播种失败: %v", err)
	}
	if _, err := deps.WorldEngine.SeedWorld("w-busy", world.SeedSpec{Mode: world.ModeSimRPG}); err != nil {
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

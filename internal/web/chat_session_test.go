// 玩家侧会话状态 API 测试（/api/chat/state、/api/chat/persona、/api/chat/world/*）。
package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/persona"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// newChatSessionTestServer 启动带世界引擎 + 人设存储的玩家侧测试服务。
func newChatSessionTestServer(t *testing.T) (*httptest.Server, AdminDeps, *core.SessionManager) {
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
	personaStore, err := persona.NewStore(db)
	if err != nil {
		t.Fatalf("创建人设存储失败: %v", err)
	}

	sessions := core.NewSessionManager()
	router := core.NewRouter(core.NewPluginManager(), sessions)
	srv := NewServer(Config{}, router, sessions)
	deps := AdminDeps{
		Sessions:     sessions,
		WorldEngine:  world.NewEngine(repo),
		PersonaStore: personaStore,
		StartTime:    time.Now(),
	}
	srv.SetAdmin(deps, "tok")
	return httptest.NewServer(srv.Handler()), deps, sessions
}

// chatReq 玩家侧请求（无鉴权 token 配置时仅需会话 token query 参数）。
func chatReq(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	return resp
}

func TestChatState_GetPut(t *testing.T) {
	ts, _, sessions := newChatSessionTestServer(t)
	defer ts.Close()

	// 默认状态
	resp := chatReq(t, "GET", ts.URL+"/api/chat/state?token=t1", "")
	var st chatStateResp
	readJSON(t, resp, &st)
	if st.Mode != "normal" || st.ReplyLength != "standard" || st.WorldID != "" || st.Persona != "" {
		t.Fatalf("默认状态不正确: %+v", st)
	}

	// 更新 mode + reply_length
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/state?token=t1", `{"mode":"freechat","reply_length":"short"}`)
	readJSON(t, resp, &st)
	if st.Mode != "freechat" || st.ReplyLength != "short" {
		t.Fatalf("更新未生效: %+v", st)
	}
	if sessions.GetMode("web:t1") != core.ModeFreeChat {
		t.Fatal("会话模式应已切换")
	}

	// 非法 mode → 400（JSON 错误体）
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/state?token=t1", `{"mode":"trpg"}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 mode 应 400: %d", resp.StatusCode)
	}
	var errBody map[string]string
	readJSON(t, resp, &errBody)
	if errBody["error"] == "" {
		t.Fatal("错误体应含 error 字段")
	}

	// 非法 reply_length → 400
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/state?token=t1", `{"reply_length":"huge"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法档位应 400: %d", resp.StatusCode)
	}

	// 缺 token → 401
	resp = chatReq(t, "GET", ts.URL+"/api/chat/state", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("缺 token 应 401: %d", resp.StatusCode)
	}
}

func TestChatPersona_GlobalAndWorld(t *testing.T) {
	ts, deps, _ := newChatSessionTestServer(t)
	defer ts.Close()

	// 全局：默认空
	resp := chatReq(t, "GET", ts.URL+"/api/chat/persona?token=t1", "")
	var p chatPersonaResp
	readJSON(t, resp, &p)
	if p.Name != "" || p.Description != "" {
		t.Fatalf("默认应为空: %+v", p)
	}

	// 设全局人设
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/persona?token=t1",
		`{"world":"","name":"林月","description":"冷静果断的侦探"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("设全局人设应 200: %d", resp.StatusCode)
	}
	resp = chatReq(t, "GET", ts.URL+"/api/chat/persona?token=t1", "")
	readJSON(t, resp, &p)
	if p.Name != "林月" || p.Description != "冷静果断的侦探" {
		t.Fatalf("全局人设读取错误: %+v", p)
	}

	// 世界层覆盖
	if _, err := deps.WorldEngine.SeedWorld("w1", world.SeedSpec{Mode: world.ModeSimRPG, Background: "北境"}); err != nil {
		t.Fatalf("播种失败: %v", err)
	}
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/persona?token=t1",
		`{"world":"w1","name":"林月","description":"伪装成商人"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("设世界人设应 200: %d", resp.StatusCode)
	}
	resp = chatReq(t, "GET", ts.URL+"/api/chat/persona?token=t1&world=w1", "")
	readJSON(t, resp, &p)
	if p.Description != "伪装成商人" {
		t.Fatalf("世界人设读取错误: %+v", p)
	}
	// 世界层不影响全局层
	resp = chatReq(t, "GET", ts.URL+"/api/chat/persona?token=t1", "")
	readJSON(t, resp, &p)
	if p.Description != "冷静果断的侦探" {
		t.Fatalf("全局人设不应被世界层覆盖: %+v", p)
	}

	// 清空世界层（description 空串）
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/persona?token=t1", `{"world":"w1","description":""}`)
	resp.Body.Close()
	resp = chatReq(t, "GET", ts.URL+"/api/chat/persona?token=t1&world=w1", "")
	readJSON(t, resp, &p)
	if p.Description != "" {
		t.Fatalf("世界层应已清除: %+v", p)
	}

	// 不存在的世界 → 404
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/persona?token=t1", `{"world":"no_such","description":"x"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("不存在的世界应 404: %d", resp.StatusCode)
	}
}

func TestChatWorld_EnterLeave(t *testing.T) {
	ts, deps, sessions := newChatSessionTestServer(t)
	defer ts.Close()

	if _, err := deps.WorldEngine.SeedWorld("tpl", world.SeedSpec{
		Mode: world.ModeSimRPG, Background: "剑与魔法的边境小镇",
	}); err != nil {
		t.Fatalf("播种失败: %v", err)
	}

	// 进入不存在的世界 → 404 JSON 错误
	resp := chatReq(t, "POST", ts.URL+"/api/chat/world/enter?token=t1", `{"world_id":"no_such"}`)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("不存在应 404: %d", resp.StatusCode)
	}
	var errBody map[string]string
	readJSON(t, resp, &errBody)
	if errBody["error"] == "" {
		t.Fatal("错误体应含 error 字段")
	}

	// 正常进入 → ok + world_name，自动切 freechat
	resp = chatReq(t, "POST", ts.URL+"/api/chat/world/enter?token=t1", `{"world_id":"tpl"}`)
	var enterResp struct {
		OK        bool   `json:"ok"`
		WorldName string `json:"world_name"`
	}
	readJSON(t, resp, &enterResp)
	if !enterResp.OK || enterResp.WorldName == "" {
		t.Fatalf("进入响应错误: %+v", enterResp)
	}
	if sessions.GetMode("web:t1") != core.ModeFreeChat {
		t.Fatal("进入世界后应切到 freechat")
	}
	// 会话世界为源世界的快照，源世界不受影响
	ws := deps.WorldEngine.LoadOrNil("web:t1")
	if ws == nil || ws.Background != "剑与魔法的边境小镇" {
		t.Fatalf("会话世界应为源世界快照: %+v", ws)
	}
	if deps.WorldEngine.LoadOrNil("tpl") == nil {
		t.Fatal("源世界不应受影响")
	}

	// GET state 反映已进入的世界
	resp = chatReq(t, "GET", ts.URL+"/api/chat/state?token=t1", "")
	var st chatStateResp
	readJSON(t, resp, &st)
	if st.WorldID != "tpl" || st.WorldName == "" || st.Mode != "freechat" {
		t.Fatalf("state 应反映已进入世界: %+v", st)
	}

	// 重复进入 → 409
	resp = chatReq(t, "POST", ts.URL+"/api/chat/world/enter?token=t1", `{"world_id":"tpl"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("已有世界时应 409: %d", resp.StatusCode)
	}

	// 离开 → ok，世界删除，切回 normal
	resp = chatReq(t, "POST", ts.URL+"/api/chat/world/leave?token=t1", "")
	var leaveResp map[string]bool
	readJSON(t, resp, &leaveResp)
	if !leaveResp["ok"] {
		t.Fatalf("离开响应错误: %+v", leaveResp)
	}
	if deps.WorldEngine.LoadOrNil("web:t1") != nil {
		t.Fatal("会话世界应已删除")
	}
	if sessions.GetMode("web:t1") != core.ModeNormal {
		t.Fatal("离开后应切回 normal")
	}
	if deps.WorldEngine.LoadOrNil("tpl") == nil {
		t.Fatal("源世界模板不应被删除")
	}

	// 幂等：再离开仍 ok
	resp = chatReq(t, "POST", ts.URL+"/api/chat/world/leave?token=t1", "")
	readJSON(t, resp, &leaveResp)
	if !leaveResp["ok"] {
		t.Fatal("重复离开应幂等返回 ok")
	}
}

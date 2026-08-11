// 玩家侧世界状态查看/编辑 API 测试（/api/chat/world/detail、/api/chat/world/section）。
package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// enterChatWorld 播种模板世界并让会话 token 进入（会话世界 = web:<token> 快照）。
func enterChatWorld(t *testing.T, ts *httptest.Server, deps AdminDeps, token string) {
	t.Helper()
	if _, err := deps.WorldEngine.SeedWorld("tpl", world.SeedSpec{
		Mode:       world.ModeSimRPG,
		Background: "剑与魔法的边境小镇",
		Scene:      "镇口广场",
		NPCs:       []world.NPCSeed{{Name: "老铁匠"}, {Name: "吟游诗人"}},
	}); err != nil {
		t.Fatalf("播种失败: %v", err)
	}
	resp := chatReq(t, "POST", ts.URL+"/api/chat/world/enter?token="+token, `{"world_id":"tpl"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("进入世界失败: %d", resp.StatusCode)
	}
}

func TestChatWorldEdit_DetailAndSection(t *testing.T) {
	ts, deps, _ := newChatSessionTestServer(t)
	defer ts.Close()
	enterChatWorld(t, ts, deps, "t1")

	// detail：完整 WorldState（形状同 admin 世界 detail）
	resp := chatReq(t, "GET", ts.URL+"/api/chat/world/detail?token=t1", "")
	var ws world.WorldState
	readJSON(t, resp, &ws)
	if ws.WorldID != "web:t1" || ws.Background != "剑与魔法的边境小镇" {
		t.Fatalf("detail 返回错误: %+v", ws)
	}

	// section GET：scene
	resp = chatReq(t, "GET", ts.URL+"/api/chat/world/section?token=t1&part=scene", "")
	var scene world.SceneState
	readJSON(t, resp, &scene)
	if scene.NodeName == "" {
		t.Fatalf("scene 分区应有节点: %+v", scene)
	}

	// section PATCH：characters 全量替换 → 再 GET 往返确认
	resp = chatReq(t, "PATCH", ts.URL+"/api/chat/world/section?token=t1",
		`{"part":"characters","data":{"莉娅":{"role":"法师","personality":"冷静"}}}`)
	var patchResp map[string]string
	readJSON(t, resp, &patchResp)
	if patchResp["message"] != "已应用" {
		t.Fatalf("PATCH 响应错误: %+v", patchResp)
	}
	resp = chatReq(t, "GET", ts.URL+"/api/chat/world/section?token=t1&part=characters", "")
	var chars map[string]*world.CharacterState
	readJSON(t, resp, &chars)
	if len(chars) != 1 || chars["莉娅"] == nil {
		t.Fatalf("characters 往返错误: %+v", chars)
	}
	if chars["莉娅"].Name != "莉娅" {
		t.Fatalf("map 键应回填为角色名: %+v", chars["莉娅"])
	}
	// detail 应同步反映修改
	resp = chatReq(t, "GET", ts.URL+"/api/chat/world/detail?token=t1", "")
	readJSON(t, resp, &ws)
	if ws.Characters["莉娅"] == nil {
		t.Fatal("detail 应反映 PATCH 后的角色")
	}

	// 非法 part → 400（GET 与 PATCH 都是）
	resp = chatReq(t, "GET", ts.URL+"/api/chat/world/section?token=t1&part=nope", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 part GET 应 400: %d", resp.StatusCode)
	}
	resp = chatReq(t, "PATCH", ts.URL+"/api/chat/world/section?token=t1",
		`{"part":"nope","data":{}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 part PATCH 应 400: %d", resp.StatusCode)
	}

	// PATCH 缺 data → 400
	resp = chatReq(t, "PATCH", ts.URL+"/api/chat/world/section?token=t1", `{"part":"scene"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("缺 data 应 400: %d", resp.StatusCode)
	}

	// PATCH 数据校验失败（scene.node_name 空）→ 400
	resp = chatReq(t, "PATCH", ts.URL+"/api/chat/world/section?token=t1",
		`{"part":"scene","data":{"node_name":"  "}}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 scene 应 400: %d", resp.StatusCode)
	}
}

func TestChatWorldEdit_NotInWorld(t *testing.T) {
	ts, _, _ := newChatSessionTestServer(t)
	defer ts.Close()

	// 未进入世界：detail / section GET / section PATCH 一律 404 JSON 错误
	for _, tc := range []struct {
		method, path, body string
	}{
		{"GET", "/api/chat/world/detail?token=t1", ""},
		{"GET", "/api/chat/world/section?token=t1&part=scene", ""},
		{"PATCH", "/api/chat/world/section?token=t1", `{"part":"scene","data":{"node_name":"x"}}`},
	} {
		resp := chatReq(t, tc.method, ts.URL+tc.path, tc.body)
		if resp.StatusCode != http.StatusNotFound {
			resp.Body.Close()
			t.Fatalf("%s %s 未进入世界应 404: %d", tc.method, tc.path, resp.StatusCode)
		}
		var errBody map[string]string
		readJSON(t, resp, &errBody)
		if !strings.Contains(errBody["error"], "当前会话还没有进行中的世界") {
			t.Fatalf("%s %s 错误体不符: %+v", tc.method, tc.path, errBody)
		}
	}
}

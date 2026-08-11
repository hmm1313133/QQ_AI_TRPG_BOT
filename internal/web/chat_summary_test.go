// 玩家侧剧情摘要查看/编辑 API 测试（/api/chat/summary）。
package web

import (
	"net/http"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

func TestChatSummary_GetAndPut(t *testing.T) {
	ts, deps, _ := newChatSessionTestServer(t)
	defer ts.Close()
	enterChatWorld(t, ts, deps, "t1")

	// 初始：空摘要、无待总结轮
	resp := chatReq(t, "GET", ts.URL+"/api/chat/summary?token=t1", "")
	var got struct {
		Summary      string `json:"summary"`
		PendingTurns int    `json:"pending_turns"`
	}
	readJSON(t, resp, &got)
	if got.Summary != "" || got.PendingTurns != 0 {
		t.Fatalf("初始应为空摘要: %+v", got)
	}

	// PUT 写回摘要 → GET 往返确认
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/summary?token=t1", `{"summary":"  玩家来到边境小镇。 "}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT 应 200: %d", resp.StatusCode)
	}
	resp = chatReq(t, "GET", ts.URL+"/api/chat/summary?token=t1", "")
	readJSON(t, resp, &got)
	if got.Summary != "玩家来到边境小镇。" {
		t.Fatalf("摘要应 TrimSpace 后写回: %q", got.Summary)
	}

	// 待总结轮数透传
	ws := deps.WorldEngine.LoadOrNil("web:t1")
	ws.PendingSummaryTurns = []world.DialogueTurn{{Round: 1, Player: "看", Narrator: "墙"}}
	if err := deps.WorldEngine.Save(ws); err != nil {
		t.Fatal(err)
	}
	resp = chatReq(t, "GET", ts.URL+"/api/chat/summary?token=t1", "")
	readJSON(t, resp, &got)
	if got.PendingTurns != 1 {
		t.Fatalf("pending_turns 应为 1: %+v", got)
	}
}

func TestChatSummary_NotInWorld(t *testing.T) {
	ts, _, _ := newChatSessionTestServer(t)
	defer ts.Close()

	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/chat/summary?token=t1", ""},
		{"PUT", "/api/chat/summary?token=t1", `{"summary":"x"}`},
	} {
		resp := chatReq(t, tc.method, ts.URL+tc.path, tc.body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s 未进入世界应 404: %d", tc.method, tc.path, resp.StatusCode)
		}
	}
}

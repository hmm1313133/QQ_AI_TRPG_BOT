// 玩家侧剧情摘要多条目 API 测试（/api/chat/summary*）。
package web

import (
	"net/http"
	"testing"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// summaryEntry 测试侧条目视图。
type summaryEntry struct {
	ID        int    `json:"id"`
	RoundFrom int    `json:"round_from"`
	RoundTo   int    `json:"round_to"`
	Text      string `json:"text"`
}

func getSummaryEntries(t *testing.T, baseURL string) ([]summaryEntry, int) {
	t.Helper()
	resp := chatReq(t, "GET", baseURL+"/api/chat/summary?token=t1", "")
	var got struct {
		Entries      []summaryEntry `json:"entries"`
		PendingTurns int            `json:"pending_turns"`
	}
	readJSON(t, resp, &got)
	return got.Entries, got.PendingTurns
}

func TestChatSummary_EntryCRUD(t *testing.T) {
	ts, deps, _ := newChatSessionTestServer(t)
	defer ts.Close()
	enterChatWorld(t, ts, deps, "t1")

	// 初始：空列表、无待总结轮
	entries, pending := getSummaryEntries(t, ts.URL)
	if len(entries) != 0 || pending != 0 {
		t.Fatalf("初始应为空列表: %+v", entries)
	}

	// 手动补录两条
	for _, text := range []string{"玩家来到边境小镇。", "玩家接下铁匠的委托。"} {
		resp := chatReq(t, "POST", ts.URL+"/api/chat/summary/entries?token=t1", `{"text":"`+text+`"}`)
		var addResp struct {
			OK bool `json:"ok"`
			ID int  `json:"id"`
		}
		readJSON(t, resp, &addResp)
		if !addResp.OK || addResp.ID == 0 {
			t.Fatalf("补录失败: %+v", addResp)
		}
	}
	entries, _ = getSummaryEntries(t, ts.URL)
	if len(entries) != 2 || entries[0].Text != "玩家来到边境小镇。" || entries[1].ID != 2 {
		t.Fatalf("条目列表错误: %+v", entries)
	}

	// 编辑第二条
	resp := chatReq(t, "PUT", ts.URL+"/api/chat/summary/entries/2?token=t1", `{"text":" 玩家拒绝了委托。 "}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("编辑应 200: %d", resp.StatusCode)
	}
	entries, _ = getSummaryEntries(t, ts.URL)
	if entries[1].Text != "玩家拒绝了委托。" {
		t.Fatalf("编辑应 TrimSpace 后写回: %+v", entries[1])
	}

	// 删除第一条，剩余条目 ID 不变
	resp = chatReq(t, "DELETE", ts.URL+"/api/chat/summary/entries/1?token=t1", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("删除应 200: %d", resp.StatusCode)
	}
	entries, _ = getSummaryEntries(t, ts.URL)
	if len(entries) != 1 || entries[0].ID != 2 {
		t.Fatalf("删除后应剩条目 2: %+v", entries)
	}

	// 编辑/删除不存在的条目 → 404；空文本 → 400
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/summary/entries/99?token=t1", `{"text":"x"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("编辑不存在条目应 404: %d", resp.StatusCode)
	}
	resp = chatReq(t, "DELETE", ts.URL+"/api/chat/summary/entries/99?token=t1", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("删除不存在条目应 404: %d", resp.StatusCode)
	}
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/summary/entries/2?token=t1", `{"text":"  "}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("空文本应 400: %d", resp.StatusCode)
	}
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/summary/entries/abc?token=t1", `{"text":"x"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 id 应 400: %d", resp.StatusCode)
	}

	// 待总结轮数透传
	ws := deps.WorldEngine.LoadOrNil("web:t1")
	ws.PendingSummaryTurns = []world.DialogueTurn{{Round: 1, Player: "看", Narrator: "墙"}}
	if err := deps.WorldEngine.Save(ws); err != nil {
		t.Fatal(err)
	}
	_, pending = getSummaryEntries(t, ts.URL)
	if pending != 1 {
		t.Fatalf("pending_turns 应为 1: %d", pending)
	}
}

// 旧数据（仅 CampaignSummary）经 GET 返回懒迁移视图
func TestChatSummary_LegacyView(t *testing.T) {
	ts, deps, _ := newChatSessionTestServer(t)
	defer ts.Close()
	enterChatWorld(t, ts, deps, "t1")

	ws := deps.WorldEngine.LoadOrNil("web:t1")
	ws.CampaignSummary = "旧滚动摘要。"
	if err := deps.WorldEngine.Save(ws); err != nil {
		t.Fatal(err)
	}

	entries, _ := getSummaryEntries(t, ts.URL)
	if len(entries) != 1 || entries[0].Text != "旧滚动摘要。" {
		t.Fatalf("旧摘要应以单条目视图返回: %+v", entries)
	}
}

func TestChatSummary_NotInWorld(t *testing.T) {
	ts, _, _ := newChatSessionTestServer(t)
	defer ts.Close()

	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/chat/summary?token=t1", ""},
		{"POST", "/api/chat/summary/entries?token=t1", `{"text":"x"}`},
		{"PUT", "/api/chat/summary/entries/1?token=t1", `{"text":"x"}`},
		{"DELETE", "/api/chat/summary/entries/1?token=t1", ""},
	} {
		resp := chatReq(t, tc.method, ts.URL+tc.path, tc.body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %s 未进入世界应 404: %d", tc.method, tc.path, resp.StatusCode)
		}
	}
}

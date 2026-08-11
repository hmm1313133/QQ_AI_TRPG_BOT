// 玩家侧消息编辑 / 多版本切换 API 测试（/api/chat/messages/*）。
package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"
)

// newChatMessageTestServer 启动带世界引擎 + 聊天记录存储的玩家侧测试服务。
func newChatMessageTestServer(t *testing.T) (*httptest.Server, AdminDeps, *ChatHistoryStore) {
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
	history, err := NewChatHistoryStore(db)
	if err != nil {
		t.Fatalf("创建聊天记录存储失败: %v", err)
	}

	sessions := core.NewSessionManager()
	router := core.NewRouter(core.NewPluginManager(), sessions)
	srv := NewServer(Config{}, router, sessions)
	srv.SetChatHistory(history)
	deps := AdminDeps{
		Sessions:    sessions,
		WorldEngine: world.NewEngine(repo),
		StartTime:   time.Now(),
	}
	srv.SetAdmin(deps, "tok")
	return httptest.NewServer(srv.Handler()), deps, history
}

func TestChatMessage_Edit(t *testing.T) {
	ts, deps, history := newChatMessageTestServer(t)
	defer ts.Close()

	history.Add("web:t1", "user", "我推开门")
	replyID := history.Add("web:t1", "reply", "门后是一条走廊")

	// 编辑（无版本消息）：ok + version 字段为 0
	resp := chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(replyID)+"?token=t1", `{"text":"门后是一堵墙"}`)
	var editResp struct {
		OK           bool  `json:"ok"`
		ID           int64 `json:"id"`
		VersionNo    int   `json:"version_no"`
		VersionCount int   `json:"version_count"`
	}
	readJSON(t, resp, &editResp)
	if !editResp.OK || editResp.ID != replyID || editResp.VersionNo != 0 || editResp.VersionCount != 0 {
		t.Fatalf("编辑响应错误: %+v", editResp)
	}
	msg, _ := history.GetMessage("web:t1", replyID)
	if msg.Text != "门后是一堵墙" {
		t.Fatalf("文本未更新: %+v", msg)
	}

	// 有版本后编辑：改的是当前激活版本
	if _, _, err := history.AddReplyVersion(replyID, "门后是一座大厅"); err != nil {
		t.Fatal(err)
	}
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(replyID)+"?token=t1", `{"text":"门后是一座宏伟的大厅"}`)
	readJSON(t, resp, &editResp)
	if !editResp.OK || editResp.VersionNo != 2 || editResp.VersionCount != 2 {
		t.Fatalf("有版本时编辑响应错误: %+v", editResp)
	}
	text, _, err := history.SetActiveVersion(replyID, 1)
	if err != nil || text != "门后是一堵墙" {
		t.Fatalf("v1 不应被编辑影响: (%q,%v)", text, err)
	}

	// 编辑后同步世界 RecentTurns（freechat 会话世界存在时）
	ws := world.NewWorldState("web:t1", world.ModeSimRPG)
	ws.AppendTurn("我推开门", "门后是一堵墙")
	if err := deps.WorldEngine.Save(ws); err != nil {
		t.Fatal(err)
	}
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(replyID)+"?token=t1", `{"text":"门后是一堵高墙"}`)
	readJSON(t, resp, &editResp)
	if !editResp.OK {
		t.Fatalf("编辑应成功: %+v", editResp)
	}
	got := deps.WorldEngine.LoadOrNil("web:t1")
	if got.RecentTurns[0].Narrator != "门后是一堵高墙" {
		t.Fatalf("RecentTurns 应同步为编辑后文本: %+v", got.RecentTurns)
	}

	// 空文本 → 400
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(replyID)+"?token=t1", `{"text":"  "}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("空文本应 400: %d", resp.StatusCode)
	}
}

func TestChatMessage_VersionSwitch(t *testing.T) {
	ts, deps, history := newChatMessageTestServer(t)
	defer ts.Close()

	history.Add("web:t1", "user", "我推开门")
	replyID := history.Add("web:t1", "reply", "门后是一条走廊")
	if _, _, err := history.AddReplyVersion(replyID, "门后是一堵墙"); err != nil {
		t.Fatal(err)
	}

	// 切到 v1：返回激活文本
	resp := chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(replyID)+"/version?token=t1", `{"version_no":1}`)
	var swResp struct {
		OK           bool   `json:"ok"`
		Text         string `json:"text"`
		VersionNo    int    `json:"version_no"`
		VersionCount int    `json:"version_count"`
	}
	readJSON(t, resp, &swResp)
	if !swResp.OK || swResp.Text != "门后是一条走廊" || swResp.VersionNo != 1 || swResp.VersionCount != 2 {
		t.Fatalf("版本切换响应错误: %+v", swResp)
	}
	msg, _ := history.GetMessage("web:t1", replyID)
	if msg.Text != "门后是一条走廊" || msg.VersionNo != 1 {
		t.Fatalf("镜像未切到 v1: %+v", msg)
	}

	// 版本切换同步世界 RecentTurns
	ws := world.NewWorldState("web:t1", world.ModeSimRPG)
	ws.AppendTurn("我推开门", "门后是一条走廊")
	if err := deps.WorldEngine.Save(ws); err != nil {
		t.Fatal(err)
	}
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(replyID)+"/version?token=t1", `{"version_no":2}`)
	readJSON(t, resp, &swResp)
	if !swResp.OK || swResp.Text != "门后是一堵墙" {
		t.Fatalf("切到 v2 响应错误: %+v", swResp)
	}
	got := deps.WorldEngine.LoadOrNil("web:t1")
	if got.RecentTurns[0].Narrator != "门后是一堵墙" {
		t.Fatalf("RecentTurns 应同步为 v2 文本: %+v", got.RecentTurns)
	}

	// 不存在的版本 → 400
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(replyID)+"/version?token=t1", `{"version_no":9}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("不存在的版本应 400: %d", resp.StatusCode)
	}
	// 非法 version_no → 400
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(replyID)+"/version?token=t1", `{"version_no":0}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("version_no=0 应 400: %d", resp.StatusCode)
	}
}

func TestChatMessage_Errors(t *testing.T) {
	ts, _, history := newChatMessageTestServer(t)
	defer ts.Close()

	history.Add("web:t1", "user", "我推开门")
	replyID := history.Add("web:t1", "reply", "门后是一条走廊")
	pushID := history.Add("web:t1", "push", "系统通知")

	// 越权（消息属于其他会话）→ 404
	resp := chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(replyID)+"?token=other", `{"text":"x"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("越权编辑应 404: %d", resp.StatusCode)
	}
	// push 消息不可编辑 → 400
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(pushID)+"?token=t1", `{"text":"x"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("编辑 push 消息应 400: %d", resp.StatusCode)
	}
	// 不存在的 id → 404
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/99999?token=t1", `{"text":"x"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("不存在的消息应 404: %d", resp.StatusCode)
	}
	// 非法 id → 400
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/abc?token=t1", `{"text":"x"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("非法 id 应 400: %d", resp.StatusCode)
	}
	// 缺 token → 401
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(replyID), `{"text":"x"}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("缺 token 应 401: %d", resp.StatusCode)
	}
	// 版本切换的越权 → 404
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(replyID)+"/version?token=other", `{"version_no":1}`)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("越权切版本应 404: %d", resp.StatusCode)
	}
	// 错误体应为 {"error":...}
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/99999?token=t1", `{"text":"x"}`)
	var errBody map[string]string
	readJSON(t, resp, &errBody)
	if errBody["error"] == "" {
		t.Fatal("错误体应含 error 字段")
	}
}

func TestChatMessage_EditUser(t *testing.T) {
	ts, deps, history := newChatMessageTestServer(t)
	defer ts.Close()

	userID := history.Add("web:t1", "user", "我推开门")
	history.Add("web:t1", "reply", "门后是一条走廊")

	// 编辑玩家发言
	resp := chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(userID)+"?token=t1", `{"text":"我踹开门"}`)
	var editResp struct {
		OK bool `json:"ok"`
	}
	readJSON(t, resp, &editResp)
	if !editResp.OK {
		t.Fatalf("编辑玩家发言应成功: %+v", editResp)
	}
	msg, _ := history.GetMessage("web:t1", userID)
	if msg.Text != "我踹开门" {
		t.Fatalf("文本未更新: %+v", msg)
	}

	// 同步世界 RecentTurns 的 Player 字段
	ws := world.NewWorldState("web:t1", world.ModeSimRPG)
	ws.AppendTurn("我踹开门", "门后是一条走廊")
	if err := deps.WorldEngine.Save(ws); err != nil {
		t.Fatal(err)
	}
	resp = chatReq(t, "PUT", ts.URL+"/api/chat/messages/"+itoa(userID)+"?token=t1", `{"text":"我轻轻推开门"}`)
	readJSON(t, resp, &editResp)
	if !editResp.OK {
		t.Fatalf("编辑应成功: %+v", editResp)
	}
	got := deps.WorldEngine.LoadOrNil("web:t1")
	if got.RecentTurns[0].Player != "我轻轻推开门" {
		t.Fatalf("RecentTurns Player 应同步为编辑后文本: %+v", got.RecentTurns)
	}
}

func TestChatMessage_Delete(t *testing.T) {
	ts, deps, history := newChatMessageTestServer(t)
	defer ts.Close()

	userID := history.Add("web:t1", "user", "我推开门")
	replyID := history.Add("web:t1", "reply", "门后是一条走廊")
	if _, _, err := history.AddReplyVersion(replyID, "门后是一堵墙"); err != nil {
		t.Fatal(err)
	}
	pushID := history.Add("web:t1", "push", "系统通知")

	// 世界 RecentTurns 里有一轮对应对话
	ws := world.NewWorldState("web:t1", world.ModeSimRPG)
	ws.AppendTurn("我推开门", "门后是一堵墙")
	ws.AppendTurn("四处看看", "走廊尽头有光")
	if err := deps.WorldEngine.Save(ws); err != nil {
		t.Fatal(err)
	}

	// 删除 AI 回复：行与版本行都没了，RecentTurns 对应 Narrator 被清空（轮次保留，因 Player 还在）
	resp := chatReq(t, "DELETE", ts.URL+"/api/chat/messages/"+itoa(replyID)+"?token=t1", "")
	var delResp struct {
		OK bool `json:"ok"`
	}
	readJSON(t, resp, &delResp)
	if !delResp.OK {
		t.Fatalf("删除应成功: %+v", delResp)
	}
	if _, err := history.GetMessage("web:t1", replyID); err == nil {
		t.Fatal("删除后消息应不存在")
	}
	if _, _, err := history.SetActiveVersion(replyID, 1); err == nil {
		t.Fatal("删除后版本行应不存在")
	}
	got := deps.WorldEngine.LoadOrNil("web:t1")
	if len(got.RecentTurns) != 2 || got.RecentTurns[0].Narrator != "" || got.RecentTurns[0].Player != "我推开门" {
		t.Fatalf("RecentTurns 应清空对应 Narrator: %+v", got.RecentTurns)
	}

	// 再删玩家发言：该轮两个字段都空了，整轮移除
	resp = chatReq(t, "DELETE", ts.URL+"/api/chat/messages/"+itoa(userID)+"?token=t1", "")
	readJSON(t, resp, &delResp)
	if !delResp.OK {
		t.Fatalf("删除应成功: %+v", delResp)
	}
	got = deps.WorldEngine.LoadOrNil("web:t1")
	if len(got.RecentTurns) != 1 || got.RecentTurns[0].Player != "四处看看" {
		t.Fatalf("空轮次应整轮移除: %+v", got.RecentTurns)
	}

	// push 消息可删（不影响世界状态）
	resp = chatReq(t, "DELETE", ts.URL+"/api/chat/messages/"+itoa(pushID)+"?token=t1", "")
	readJSON(t, resp, &delResp)
	if !delResp.OK {
		t.Fatalf("删除 push 应成功: %+v", delResp)
	}

	// 越权删除 → 404；重复删除 → 404
	resp = chatReq(t, "DELETE", ts.URL+"/api/chat/messages/"+itoa(userID)+"?token=other", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("越权删除应 404: %d", resp.StatusCode)
	}
	resp = chatReq(t, "DELETE", ts.URL+"/api/chat/messages/"+itoa(userID)+"?token=t1", "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("重复删除应 404: %d", resp.StatusCode)
	}
	// 缺 token → 401
	resp = chatReq(t, "DELETE", ts.URL+"/api/chat/messages/"+itoa(userID), "")
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("缺 token 应 401: %d", resp.StatusCode)
	}
}

// itoa int64 → 字符串（测试辅助）。
func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

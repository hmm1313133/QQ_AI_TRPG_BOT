// WS「AI 回复重试（regen）」链路测试。
package web

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/world"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// newRegenTestServer 启动带聊天记录存储（可选世界引擎）的 WS 测试服务。
func newRegenTestServer(t *testing.T, withWorld bool) (*httptest.Server, *ChatHistoryStore, *world.Engine) {
	t.Helper()
	db, err := world.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	history, err := NewChatHistoryStore(db)
	if err != nil {
		t.Fatalf("创建聊天记录存储失败: %v", err)
	}

	sessions := core.NewSessionManager()
	router := core.NewRouter(core.NewPluginManager(), sessions)
	srv := NewServer(Config{}, router, sessions)
	srv.SetChatHistory(history)

	var engine *world.Engine
	if withWorld {
		repo, err := world.NewSQLiteRepository(db)
		if err != nil {
			t.Fatalf("创建世界仓库失败: %v", err)
		}
		engine = world.NewEngine(repo)
		srv.SetAdmin(AdminDeps{Sessions: sessions, WorldEngine: engine, StartTime: time.Now()}, "tok")
	}
	return httptest.NewServer(srv.Handler()), history, engine
}

// dialChat 建立 WS 连接并读完 session 欢迎帧。
func dialChat(t *testing.T, baseURL, token string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, wsURL(baseURL)+"/ws/chat?token="+token, nil)
	if err != nil {
		t.Fatalf("WS 连接失败: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	var welcome wsMessage
	if err := wsjson.Read(ctx, conn, &welcome); err != nil || welcome.Type != "session" {
		t.Fatalf("应收到 session 欢迎帧: %v, %+v", err, welcome)
	}
	return conn
}

// readUntil 读帧直到命中目标类型。
func readUntil(t *testing.T, conn *websocket.Conn, typ string) wsMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		var msg wsMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			t.Fatalf("读取 %s 帧失败: %v", typ, err)
		}
		if msg.Type == typ {
			return msg
		}
	}
}

// sendWS 发送一帧。
func sendWS(t *testing.T, conn *websocket.Conn, msg wsMessage) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, msg); err != nil {
		t.Fatalf("发送帧失败: %v", err)
	}
}

// 成功路径：无世界快照（normal 模式）时取前一条 user 消息直接重跑；
// 重试回复落为新版本，不新增 chat_messages 行。
func TestWebChannel_RegenRoundtrip(t *testing.T) {
	ts, history, _ := newRegenTestServer(t, false)
	defer ts.Close()

	token := getToken(t, ts.URL)
	conn := dialChat(t, ts.URL, token)

	// 普通对话：reply 帧带新行 id
	sendWS(t, conn, wsMessage{Type: "chat", Text: "你好"})
	reply := readUntil(t, conn, "reply")
	if reply.ID <= 0 || reply.Text == "" {
		t.Fatalf("普通 reply 帧应带 id 与文本: %+v", reply)
	}

	// 第一次重试：落 v1+v2，回复帧带 ref_id/version_no=2/version_count=2
	sendWS(t, conn, wsMessage{Type: "regen", RefID: reply.ID})
	regen := readUntil(t, conn, "reply")
	if regen.RefID != reply.ID || regen.VersionNo != 2 || regen.VersionCount != 2 || regen.Text == "" {
		t.Fatalf("重试 reply 帧错误: %+v", regen)
	}
	if regen.ID != 0 {
		t.Fatalf("重试 reply 帧不应带新行 id: %+v", regen)
	}

	// 第二次重试：v3
	sendWS(t, conn, wsMessage{Type: "regen", RefID: reply.ID})
	regen = readUntil(t, conn, "reply")
	if regen.RefID != reply.ID || regen.VersionNo != 3 || regen.VersionCount != 3 {
		t.Fatalf("第二次重试 reply 帧错误: %+v", regen)
	}

	// 不新增 chat_messages 行（仍只有 user + reply 两条）
	list, _ := history.List("web:"+token, 0)
	if len(list) != 2 {
		t.Fatalf("重试不应新增消息行: %+v", list)
	}
	if list[1].VersionNo != 3 || list[1].VersionCount != 3 {
		t.Fatalf("历史聚合应反映 3 个版本: %+v", list[1])
	}
}

// 校验失败路径：非 reply / 非最新 reply / 不存在的消息都回 error 帧。
func TestWebChannel_RegenValidation(t *testing.T) {
	ts, history, _ := newRegenTestServer(t, false)
	defer ts.Close()

	token := getToken(t, ts.URL)
	conn := dialChat(t, ts.URL, token)
	sessionID := "web:" + token

	// 不存在的消息 → error
	sendWS(t, conn, wsMessage{Type: "regen", RefID: 99999})
	if msg := readUntil(t, conn, "error"); msg.Text == "" {
		t.Fatal("不存在的消息应回 error 帧")
	}

	// user 消息 → error
	userID := history.Add(sessionID, "user", "我自己说的")
	sendWS(t, conn, wsMessage{Type: "regen", RefID: userID})
	if msg := readUntil(t, conn, "error"); msg.Text == "" {
		t.Fatal("user 消息应回 error 帧")
	}

	// 旧 reply（其后已有更新的 user/reply）→ error
	oldReply := history.Add(sessionID, "reply", "旧回复")
	history.Add(sessionID, "user", "新话题")
	history.Add(sessionID, "reply", "新回复")
	sendWS(t, conn, wsMessage{Type: "regen", RefID: oldReply})
	if msg := readUntil(t, conn, "error"); msg.Text == "" {
		t.Fatal("非最新 reply 应回 error 帧")
	}
}

// freechat 路径：有回合快照时重试先回滚世界状态。
func TestWebChannel_RegenRestoresSnapshot(t *testing.T) {
	ts, history, engine := newRegenTestServer(t, true)
	defer ts.Close()

	token := getToken(t, ts.URL)
	sessionID := "web:" + token

	// 会话世界：回合前状态（RoundCount=5）写快照，随后模拟回合落账（RoundCount=6）
	ws := world.NewWorldState(sessionID, world.ModeSimRPG)
	ws.RoundCount = 5
	if err := engine.Save(ws); err != nil {
		t.Fatal(err)
	}
	if err := engine.SaveTurnSnapshot(sessionID, ws, "冲进去"); err != nil {
		t.Fatal(err)
	}
	mutated := engine.LoadOrNil(sessionID)
	mutated.RoundCount = 6
	mutated.AppendTurn("冲进去", "旧回复")
	if err := engine.Save(mutated); err != nil {
		t.Fatal(err)
	}

	// 历史：user + reply（重试校验目标）
	history.Add(sessionID, "user", "冲进去")
	replyID := history.Add(sessionID, "reply", "旧回复")

	conn := dialChat(t, ts.URL, token)
	sendWS(t, conn, wsMessage{Type: "regen", RefID: replyID})
	regen := readUntil(t, conn, "reply")
	if regen.RefID != replyID || regen.VersionNo != 2 || regen.VersionCount != 2 {
		t.Fatalf("重试 reply 帧错误: %+v", regen)
	}

	// 世界状态应回滚到快照（RoundCount=5，无回合落账的 RecentTurns）
	got := engine.LoadOrNil(sessionID)
	if got == nil || got.RoundCount != 5 || len(got.RecentTurns) != 0 {
		t.Fatalf("世界状态应回滚到快照: %+v", got)
	}
}

package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/handler"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	plugins := core.NewPluginManager()
	plugins.RegisterHandler(handler.NewHelpHandler())
	sessions := core.NewSessionManager()
	router := core.NewRouter(plugins, sessions, nil)

	srv := NewServer(Config{UploadDir: t.TempDir()}, router, sessions)
	return httptest.NewServer(srv.Handler())
}

func getToken(t *testing.T, baseURL string) string {
	t.Helper()
	resp, err := http.Get(baseURL + "/api/session")
	if err != nil {
		t.Fatalf("获取会话令牌失败: %v", err)
	}
	defer resp.Body.Close()
	var data struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.Token == "" {
		t.Fatalf("令牌响应异常: %v", err)
	}
	return data.Token
}

func wsURL(httpURL string) string {
	return "ws" + strings.TrimPrefix(httpURL, "http")
}

func TestWebChannel_HelpCommand(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	token := getToken(t, ts.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, wsURL(ts.URL)+"/ws/chat?token="+token, nil)
	if err != nil {
		t.Fatalf("WS 连接失败: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// 读取欢迎帧
	var welcome wsMessage
	if err := wsjson.Read(ctx, conn, &welcome); err != nil || welcome.Type != "session" {
		t.Fatalf("应收到 session 欢迎帧: %v, %+v", err, welcome)
	}

	// 发送 .help 指令
	if err := wsjson.Write(ctx, conn, wsMessage{Type: "chat", Text: ".help"}); err != nil {
		t.Fatalf("发送消息失败: %v", err)
	}

	// 期待回复帧
	var reply wsMessage
	if err := wsjson.Read(ctx, conn, &reply); err != nil {
		t.Fatalf("读取回复失败: %v", err)
	}
	if reply.Type != "reply" || reply.Text == "" {
		t.Fatalf("应收到非空 reply 帧: %+v", reply)
	}
	t.Logf("收到回复（前 60 字符）: %.60s", reply.Text)
}

func TestWebChannel_MissingToken(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, resp, err := websocket.Dial(ctx, wsURL(ts.URL)+"/ws/chat", nil)
	if err == nil {
		t.Fatal("缺少 token 应拒绝连接")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("应返回 401: %+v", resp)
	}
}

func TestWebChannel_ChatPageServed(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	// SPA history fallback：前端路由路径应返回 index.html
	resp, err := http.Get(ts.URL + "/chat")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("聊天页应可访问: %v, %+v", err, resp)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "<div id=\"app\">") && !strings.Contains(string(body), "id=\"app\"") {
		t.Fatalf("/chat 应返回 SPA index.html，实际: %.120s", body)
	}

	// 根路径应重定向到 /chat
	resp2, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("根路径访问失败: %v", err)
	}
	resp2.Body.Close()
	if resp2.Request.URL.Path != "/chat" {
		t.Fatalf("根路径应重定向到 /chat: %s", resp2.Request.URL.Path)
	}
}

package web

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/handler"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// TestTrpcHTTPSpike 验证 trpc-go 泛 HTTP 托管（StartTrpc，trpc_go.yaml 注入）下三条链路可用：
// REST API、SPA 静态回退、WebSocket 升级。这是 main.go 正式接入的回归保障。
func TestTrpcHTTPSpike(t *testing.T) {
	// 构造测试用 trpc_go.yaml（标准 server.service 配置）
	confPath := filepath.Join(t.TempDir(), "trpc_go.yaml")
	confYAML := `server:
  app: test
  server: test
  service:
    - name: trpc.trpg.web.Admin
      ip: 127.0.0.1
      port: 18099
      network: tcp
      protocol: http
      idletime: -1
`
	if err := os.WriteFile(confPath, []byte(confYAML), 0644); err != nil {
		t.Fatalf("写入测试配置失败: %v", err)
	}

	plugins := core.NewPluginManager()
	plugins.RegisterHandler(handler.NewHelpHandler())
	sessions := core.NewSessionManager()
	router := core.NewRouter(plugins, sessions, nil)
	srv := NewServer(Config{UploadDir: t.TempDir()}, router, sessions)

	if err := srv.StartTrpc(confPath); err != nil {
		t.Fatalf("StartTrpc 失败: %v", err)
	}
	defer srv.Stop()

	// 等待端口就绪
	base := "http://127.0.0.1:18099"
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", "127.0.0.1:18099", 200*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("trpc HTTP 服务未在 5s 内就绪: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 1. REST：GET /api/session 返回 token
	resp, err := http.Get(base + "/api/session")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("REST /api/session 失败: %v, %+v", err, resp)
	}
	var data struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil || data.Token == "" {
		resp.Body.Close()
		t.Fatalf("REST 响应异常: %v", err)
	}
	resp.Body.Close()

	// 2. SPA：GET /chat 回退到 index.html
	resp2, err := http.Get(base + "/chat")
	if err != nil || resp2.StatusCode != http.StatusOK {
		t.Fatalf("SPA /chat 失败: %v, %+v", err, resp2)
	}
	body, _ := io.ReadAll(resp2.Body)
	resp2.Body.Close()
	if !strings.Contains(string(body), `id="app"`) {
		t.Fatalf("SPA 回退未返回 index.html: %.120s", body)
	}

	// 3. WebSocket：升级 + 收发一回合
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws://127.0.0.1:18099/ws/chat?token="+data.Token, nil)
	if err != nil {
		t.Fatalf("WS 升级失败: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	var welcome wsMessage
	if err := wsjson.Read(ctx, conn, &welcome); err != nil || welcome.Type != "session" {
		t.Fatalf("WS 欢迎帧异常: %v, %+v", err, welcome)
	}
	if err := wsjson.Write(ctx, conn, wsMessage{Type: "chat", Text: ".help"}); err != nil {
		t.Fatalf("WS 发送失败: %v", err)
	}
	var reply wsMessage
	if err := wsjson.Read(ctx, conn, &reply); err != nil || reply.Type != "reply" {
		t.Fatalf("WS 回复帧异常: %v, %+v", err, reply)
	}
}

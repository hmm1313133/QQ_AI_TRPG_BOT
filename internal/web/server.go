// Package web 实现 Web 渠道：HTTP 静态服务 + WebSocket 聊天 + 管理后台 API。
//
// 设计见《多渠道改造计划.md》。与 QQ 渠道共用 core.Router，
// 指令与聊天同通道（.script 等指令在 Web 端零成本可用）。
package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

//go:embed static
var staticFS embed.FS

// Config Web 渠道配置。
type Config struct {
	Addr      string // 监听地址，如 :8080
	ChatToken string // 可选访问令牌（为空则开放）
	UploadDir string // 上传文件目录
}

// Server 是 Web 渠道服务器。
type Server struct {
	cfg    Config
	router *core.Router
	sess   *core.SessionManager

	httpServer *http.Server

	adminDeps  AdminDeps
	adminToken string
	adminReady bool

	mu    sync.Mutex
	conns map[string]*wsConn // sessionToken -> 连接
}

// wsConn 一个 WebSocket 连接（写操作需持锁）。
type wsConn struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

// wsMessage 客户端/服务端消息帧。
type wsMessage struct {
	Type  string `json:"type"`            // chat / reply / status / push / progress / error / session
	Text  string `json:"text,omitempty"`
	State string `json:"state,omitempty"` // status: thinking / idle
	Round int    `json:"round,omitempty"`
	Token string `json:"token,omitempty"` // session 帧
}

// NewServer 创建 Web 渠道服务器。
func NewServer(cfg Config, router *core.Router, sessions *core.SessionManager) *Server {
	if cfg.Addr == "" {
		cfg.Addr = ":8080"
	}
	if cfg.UploadDir == "" {
		cfg.UploadDir = "./data/uploads"
	}
	return &Server{
		cfg:    cfg,
		router: router,
		sess:   sessions,
		conns:  make(map[string]*wsConn),
	}
}

// ID 实现 core.Channel。
func (s *Server) ID() string { return "web" }

// SetAdmin 配置管理后台（Start 前调用）。
func (s *Server) SetAdmin(deps AdminDeps, adminToken string) {
	s.adminDeps = deps
	s.adminToken = adminToken
	s.adminReady = true
}

// buildMux 构建路由表。
func (s *Server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	// SPA：静态资源走 /assets/，其余 GET 路径一律回退到 index.html（history 路由）
	if dist, err := fs.Sub(staticFS, "static/dist"); err == nil {
		if indexHTML, err := fs.ReadFile(dist, "index.html"); err == nil {
			mux.Handle("GET /assets/", http.FileServerFS(dist))
			mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "/chat", http.StatusFound)
			})
			mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write(indexHTML)
			})
		}
	}
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("POST /api/upload", s.handleUpload)
	mux.HandleFunc("GET /ws/chat", s.handleChatWS)
	if s.adminReady {
		s.registerAdmin(mux, s.adminDeps, s.adminToken)
	}
	return mux
}

// Handler 返回 HTTP 处理器（供测试与内嵌使用）。
func (s *Server) Handler() http.Handler {
	return s.buildMux()
}

// Start 启动 HTTP 服务（非阻塞）。
func (s *Server) Start() error {
	if err := os.MkdirAll(s.cfg.UploadDir, 0755); err != nil {
		return err
	}

	s.httpServer = &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.buildMux(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("[Web] 聊天与管理服务已启动: http://localhost%s", s.cfg.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Web] HTTP 服务出错: %v", err)
		}
	}()
	return nil
}

// Stop 停止服务。
func (s *Server) Stop() error {
	if s.httpServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

// ============================================================
// 会话令牌
// ============================================================

// handleSession 签发会话令牌（客户端存 localStorage，WS 连接时携带）。
func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ChatToken != "" && r.URL.Query().Get("auth") != s.cfg.ChatToken {
		http.Error(w, "未授权", http.StatusUnauthorized)
		return
	}
	token := newToken()
	writeJSON(w, map[string]string{"token": token})
}

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ============================================================
// 文件上传（剧本等）
// ============================================================

// handleUpload 接收 multipart 文件，保存后返回路径（供 .script upload 使用）。
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ChatToken != "" && r.URL.Query().Get("auth") != s.cfg.ChatToken {
		http.Error(w, "未授权", http.StatusUnauthorized)
		return
	}
	if err := r.ParseMultipartForm(50 << 20); err != nil { // 50MB
		http.Error(w, "解析上传失败: "+err.Error(), http.StatusBadRequest)
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "缺少文件: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer file.Close()

	// 文件名安全化
	name := filepath.Base(header.Filename)
	name = strings.Map(func(c rune) rune {
		if c == '/' || c == '\\' || c == ':' {
			return '_'
		}
		return c
	}, name)
	dst := filepath.Join(s.cfg.UploadDir, time.Now().Format("20060102150405")+"_"+name)

	out, err := os.Create(dst)
	if err != nil {
		http.Error(w, "保存失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		http.Error(w, "写入失败: "+err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("[Web] 文件上传: %s -> %s", header.Filename, dst)
	writeJSON(w, map[string]string{"path": dst, "filename": header.Filename})
}

// ============================================================
// WebSocket 聊天
// ============================================================

// handleChatWS 处理聊天 WebSocket 连接。
func (s *Server) handleChatWS(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "缺少 token", http.StatusUnauthorized)
		return
	}
	if s.cfg.ChatToken != "" && r.URL.Query().Get("auth") != s.cfg.ChatToken {
		http.Error(w, "未授权", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // 自托管场景，允许任意来源
	})
	if err != nil {
		log.Printf("[Web] WS 握手失败: %v", err)
		return
	}

	wc := &wsConn{conn: conn}
	s.mu.Lock()
	s.conns[token] = wc
	s.mu.Unlock()
	log.Printf("[Web] WS 连接建立: token=%s...", token[:8])

	defer func() {
		s.mu.Lock()
		delete(s.conns, token)
		s.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "")
		log.Printf("[Web] WS 连接关闭: token=%s...", token[:8])
	}()

	sessionID := "web:" + token

	// 连接即发送欢迎与当前模式
	s.send(wc, wsMessage{Type: "session", Token: token,
		Text: "已连接。发送 .help 查看指令，.mode trpg 进入跑团模式。"})

	for {
		var msg wsMessage
		err := wsjson.Read(r.Context(), conn, &msg)
		if err != nil {
			return // 连接关闭
		}
		if msg.Type != "chat" || strings.TrimSpace(msg.Text) == "" {
			continue
		}

		// 构建统一消息上下文，走共享路由
		mc := &core.MessageContext{
			Ctx:       context.Background(),
			Source:    core.SourceWeb,
			SessionID: sessionID,
			UserID:    sessionID,
			OpenID:    token,
			MsgID:     newToken()[:16],
			Content:   strings.TrimSpace(msg.Text),
			IsGroup:   false,
			Extra:     make(map[string]interface{}),
		}

		// 非指令消息先反馈"思考中"
		if !strings.HasPrefix(mc.Content, ".") {
			s.send(wc, wsMessage{Type: "status", State: "thinking"})
		}

		go s.router.Route(mc, s.makeReplyFunc(wc))
	}
}

// makeReplyFunc 创建 Web 渠道的回复函数（WS 推送）。
func (s *Server) makeReplyFunc(wc *wsConn) core.ReplyFunc {
	return func(ctx context.Context, openid, msgID, text string, isGroup bool) error {
		s.send(wc, wsMessage{Type: "reply", Text: text})
		s.send(wc, wsMessage{Type: "status", State: "idle"})
		return nil
	}
}

// send 向连接写入消息（持锁保证写安全）。
func (s *Server) send(wc *wsConn, msg wsMessage) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, wc.conn, msg); err != nil {
		log.Printf("[Web] WS 写入失败: %v", err)
	}
}

// PushToSession 主动向指定会话推送消息（TimelineEngine 提醒等）。
// sessionID 形如 "web:<token>"；无连接时静默忽略。
func (s *Server) PushToSession(sessionID, text string) {
	if !strings.HasPrefix(sessionID, "web:") {
		return
	}
	token := strings.TrimPrefix(sessionID, "web:")
	s.mu.Lock()
	wc, ok := s.conns[token]
	s.mu.Unlock()
	if !ok {
		return
	}
	s.send(wc, wsMessage{Type: "push", Text: text})
}

// writeJSON 输出 JSON 响应。
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

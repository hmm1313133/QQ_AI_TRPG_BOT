// Package web 实现 Web 渠道：HTTP 静态服务 + WebSocket 聊天 + 管理后台 API。
//
// 设计见《多渠道改造计划.md》。消息统一经 core.Router 转给 AI Agent，
// 会话状态（模式/长度/人设/进出世界）走 /api/chat/* REST API。
package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"

	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
	trpcserver "trpc.group/trpc-go/trpc-go/server"
)

//go:embed static
var staticFS embed.FS

// Config Web 渠道配置。
// 监听地址/端口由 trpc_go.yaml 的 server.service 段配置，不在此处。
type Config struct {
	ChatToken string // 可选访问令牌（为空则开放）
}

// Server 是 Web 渠道服务器。
type Server struct {
	cfg    Config
	router *core.Router
	sess   *core.SessionManager

	trpcServer *trpcserver.Server // trpc-go 泛 HTTP 托管（StartTrpc 后非空）

	adminDeps  AdminDeps
	adminToken string
	adminReady bool

	history ChatLogger // 聊天记录存储（可为 nil，nil 时 /api/chat/history 返回空）

	mu    sync.Mutex
	conns map[string]*wsConn // sessionToken -> 连接

	// turnLocks 串行化同一 Web 会话的 chat、regen 和恢复操作。
	turnLocksMu sync.Mutex
	turnLocks   map[string]*sync.Mutex
}

// wsConn 一个 WebSocket 连接（写操作需持锁）。
type wsConn struct {
	mu   sync.Mutex
	conn *websocket.Conn
}

// wsMessage 客户端/服务端消息帧。
type wsMessage struct {
	Type  string `json:"type"` // chat / regen / reply / status / push / progress / error / session / ack
	Text  string `json:"text,omitempty"`
	State string `json:"state,omitempty"` // status: thinking / idle
	Round int    `json:"round,omitempty"`
	Token string `json:"token,omitempty"` // session 帧
	// ID 普通 reply 帧的新行 id（chat_messages.id）。
	ID int64 `json:"id,omitempty"`
	// RefID regen 请求帧 / 重试 reply 帧引用的原消息 id（版本组所在行）。
	RefID        int64 `json:"ref_id,omitempty"`
	VersionNo    int   `json:"version_no,omitempty"`    // 重试 reply 帧：激活版本号
	VersionCount int   `json:"version_count,omitempty"` // 重试 reply 帧：总版本数
}

// NewServer 创建 Web 渠道服务器。
func NewServer(cfg Config, router *core.Router, sessions *core.SessionManager) *Server {
	return &Server{
		cfg:       cfg,
		router:    router,
		sess:      sessions,
		conns:     make(map[string]*wsConn),
		turnLocks: make(map[string]*sync.Mutex),
	}
}

// ID 实现 core.Channel。
func (s *Server) ID() string { return "web" }

func (s *Server) lockTurn(sessionID string) func() {
	s.turnLocksMu.Lock()
	lock := s.turnLocks[sessionID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.turnLocks[sessionID] = lock
	}
	s.turnLocksMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

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
	mux.HandleFunc("GET /ws/chat", s.handleChatWS)
	mux.HandleFunc("GET /api/chat/history", s.handleChatHistory)
	s.registerChatSaveRoutes(mux)      // 玩家侧存档（设计 §9.4）
	s.registerChatWorldRoutes(mux)     // 玩家侧进入世界选择器
	s.registerChatSessionRoutes(mux)   // 玩家侧会话状态（模式/长度/人设/进出世界）
	s.registerChatMessageRoutes(mux)   // 玩家侧消息编辑/多版本切换
	s.registerChatWorldEditRoutes(mux) // 玩家侧世界状态查看/编辑
	s.registerChatSummaryRoutes(mux)   // 玩家侧剧情摘要查看/编辑
	s.registerPromptPresetRoutes(mux)  // 玩家侧主提示词预设列表
	if s.adminReady {
		s.registerAdmin(mux, s.adminDeps, s.adminToken)
	}
	return mux
}

// Handler 返回 HTTP 处理器（供测试与内嵌使用）。
func (s *Server) Handler() http.Handler {
	return s.buildMux()
}

// Stop 停止服务。
func (s *Server) Stop() error {
	return s.stopTrpc()
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
		Text: "已连接。直接发送消息开始对话；左侧可进入世界、设置人设与回复长度。"})

	for {
		var msg wsMessage
		err := wsjson.Read(r.Context(), conn, &msg)
		if err != nil {
			return // 连接关闭
		}
		if msg.Type == "regen" {
			go s.handleRegen(wc, sessionID, token, msg.RefID)
			continue
		}
		if msg.Type != "chat" || strings.TrimSpace(msg.Text) == "" {
			continue
		}

		// 同一会话的回合请求串行化，避免 chat 与 regen 交错提交世界状态。
		unlockTurn := s.lockTurn(sessionID)
		// 记录玩家消息（历史恢复用），并回执行 id 让前端气泡可编辑/删除
		userMsgID := s.recordChat(sessionID, "user", strings.TrimSpace(msg.Text))
		s.send(wc, wsMessage{Type: "ack", ID: userMsgID, Text: strings.TrimSpace(msg.Text)})

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

		// 先反馈"思考中"
		s.send(wc, wsMessage{Type: "status", State: "thinking"})

		go func() {
			defer unlockTurn()
			s.router.Route(mc, s.makeReplyFunc(wc, sessionID))
		}()
	}
}

// handleRegen 处理"AI 回复重试"请求：校验目标 → 回滚世界状态（有快照时）→ 重跑用户输入。
// 重放输入优先取聊天历史里目标 reply 前最近一条 user 消息的当前文本（玩家可能编辑过
// 自己的发言，编辑后重试应按新文本生成）；历史缺失（如发言已删）才退回快照原文。
// 校验失败回 error 帧；重试回复不新增 chat_messages 行，而是作为 ref_id 消息的新版本。
func (s *Server) handleRegen(wc *wsConn, sessionID, token string, refID int64) {
	unlockTurn := s.lockTurn(sessionID)
	defer unlockTurn()
	fail := func(text string) {
		s.send(wc, wsMessage{Type: "error", Text: text})
	}
	if s.history == nil {
		fail("聊天记录存储不可用")
		return
	}
	if refID <= 0 {
		fail("缺少目标消息 id")
		return
	}
	// 校验：目标存在、属于本会话、type=reply、是该会话最新一条 reply 且其后无更新的 user 消息
	// （仅允许重试最新一条 AI 回复；更早的消息只能编辑或切版本）。
	target, err := s.history.GetMessage(sessionID, refID)
	if err != nil {
		fail("消息不存在或不属于本会话")
		return
	}
	if target.Type != "reply" {
		fail("只能重试 AI 回复")
		return
	}
	last, err := s.history.LastReplyOf(sessionID)
	if err != nil || last == nil || last.ID != refID {
		fail("只能重试最新一条回复")
		return
	}
	if newer, err := s.history.HasNewerUserAfter(sessionID, refID); err != nil || newer {
		fail("该回复之后已有新消息，无法重试")
		return
	}

	// 世界状态回滚：freechat 且有快照则恢复（快照原文留作兜底输入）；
	// 无快照（normal 模式或老数据）直接重跑（无状态/接受漂移）。
	var snapshotText string
	if s.adminReady && s.adminDeps.WorldEngine != nil {
		ut, has, rerr := s.adminDeps.WorldEngine.RestoreTurnSnapshot(sessionID)
		if rerr != nil {
			log.Printf("[Web] 回合快照恢复失败（直接重跑）: %v", rerr)
		} else if has {
			snapshotText = ut
		}
	}
	// 重放输入：聊天历史里的当前文本优先（反映玩家编辑），快照原文兜底。
	userText, ok := s.history.UserTextBefore(sessionID, refID)
	if !ok || strings.TrimSpace(userText) == "" {
		userText = snapshotText
	}
	if strings.TrimSpace(userText) == "" {
		fail("找不到可重放的用户输入")
		return
	}

	// 重试路径不重复记录 user 消息；Extra 标记让 TurnEngine 跳过回合快照写入。
	mc := &core.MessageContext{
		Ctx:       context.Background(),
		Source:    core.SourceWeb,
		SessionID: sessionID,
		UserID:    sessionID,
		OpenID:    token,
		MsgID:     newToken()[:16],
		Content:   userText,
		IsGroup:   false,
		Extra:     map[string]interface{}{agent.ExtraKeyRegenRetry: true},
	}

	s.send(wc, wsMessage{Type: "status", State: "thinking"})
	// handleRegen 本身运行在独立 goroutine，保持会话锁直到路由和状态提交完成。
	s.router.Route(mc, s.makeRegenReplyFunc(wc, refID))
}

// makeReplyFunc 创建 Web 渠道的回复函数（WS 推送 + 历史记录）。
// reply 帧携带落库后的新行 id（历史存储未配置时为 0，前端按缺省处理）。
func (s *Server) makeReplyFunc(wc *wsConn, sessionID string) core.ReplyFunc {
	return func(ctx context.Context, openid, msgID, text string, isGroup bool) error {
		var id int64
		if s.history != nil {
			id = s.history.Add(sessionID, "reply", text)
		}
		s.send(wc, wsMessage{Type: "reply", ID: id, Text: text})
		s.send(wc, wsMessage{Type: "status", State: "idle"})
		return nil
	}
}

// makeRegenReplyFunc 创建"重试"回复函数：回复文本落为 refID 消息的新版本
// （不新增 chat_messages 行），reply 帧携带 ref_id / version_no / version_count。
func (s *Server) makeRegenReplyFunc(wc *wsConn, refID int64) core.ReplyFunc {
	return func(ctx context.Context, openid, msgID, text string, isGroup bool) error {
		versionNo, count, err := s.history.AddReplyVersion(refID, text)
		if err != nil {
			log.Printf("[Web] 重试版本落库失败: %v", err)
			s.send(wc, wsMessage{Type: "error", Text: "重试结果保存失败: " + err.Error()})
			s.send(wc, wsMessage{Type: "status", State: "idle"})
			return nil
		}
		s.send(wc, wsMessage{Type: "reply", RefID: refID, Text: text, VersionNo: versionNo, VersionCount: count})
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

// PushToSession 主动向指定会话推送消息。
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
	s.recordChat(sessionID, "push", text)
	s.send(wc, wsMessage{Type: "push", Text: text})
}

// writeJSON 输出 JSON 响应。
func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

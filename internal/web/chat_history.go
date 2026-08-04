// Package web - Web 聊天消息记录（SQLite 持久化）。
//
// 解决 /chat 页刷新/往返后台后对话历史丢失的问题：
// Web 渠道收发的消息落 chat_messages 表，页面加载时经
// GET /api/chat/history 拉取最近记录恢复视图。
// 仅记录 Web 渠道（QQ 渠道有各自客户端的历史，无需服务端代存）。
package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"time"
)

// 每会话保留的最大消息条数（超出滚动删除最旧的）。
const chatHistoryKeep = 500

// 历史接口默认返回条数。
const chatHistoryLimit = 200

// ChatLogger 聊天记录存储接口。
//
// 自己实现而不用 trpc-agent-go 的 session.Service 的原因（设计权衡，见
// 《AI多层架构分析.md》问题 6）：框架 session 存的是 runner 的 LLM 事件流
// （含工具调用中间帧），持久化实现只有 Redis；而 Narrator 刻意无状态调用，
// 这里要存的是"玩家可见消息"（指令回复、推送也在内），语义不同。
// 接口保留两个方向的最小能力，后期可换实现（如按渠道分表、加检索）。
type ChatLogger interface {
	// Add 记录一条消息（实现方负责保留策略；失败应静默，不影响聊天主流程）。
	Add(sessionID, typ, text string)
	// List 取会话最近 limit 条消息（按时间升序；limit <= 0 用默认值）。
	List(sessionID string, limit int) ([]ChatMessage, error)
}

// ChatMessage 一条聊天记录。
type ChatMessage struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"` // user / reply / push
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
}

// ChatHistoryStore 聊天记录 SQLite 存储（ChatLogger 的默认实现）。
type ChatHistoryStore struct {
	db *sql.DB
}

// NewChatHistoryStore 创建聊天记录存储（建表 chat_messages）。
func NewChatHistoryStore(db *sql.DB) (*ChatHistoryStore, error) {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS chat_messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			type TEXT NOT NULL,
			text TEXT NOT NULL,
			created_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chat_messages_session ON chat_messages(session_id, id)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, fmt.Errorf("建聊天记录表失败: %w", err)
		}
	}
	return &ChatHistoryStore{db: db}, nil
}

// Add 记录一条消息，并滚动清理该会话超出保留上限的旧消息。
func (s *ChatHistoryStore) Add(sessionID, typ, text string) {
	if text == "" {
		return
	}
	if _, err := s.db.Exec(
		`INSERT INTO chat_messages (session_id, type, text, created_at) VALUES (?, ?, ?, ?)`,
		sessionID, typ, text, time.Now().Format("2006-01-02 15:04:05")); err != nil {
		return // 记录失败不影响聊天主流程
	}
	// 滚动保留最近 N 条
	_, _ = s.db.Exec(
		`DELETE FROM chat_messages WHERE session_id = ? AND id NOT IN (
			SELECT id FROM chat_messages WHERE session_id = ? ORDER BY id DESC LIMIT ?
		)`, sessionID, sessionID, chatHistoryKeep)
}

// List 取会话最近 limit 条消息（按时间升序）。
func (s *ChatHistoryStore) List(sessionID string, limit int) ([]ChatMessage, error) {
	if limit <= 0 {
		limit = chatHistoryLimit
	}
	rows, err := s.db.Query(
		`SELECT id, type, text, created_at FROM (
			SELECT id, type, text, created_at FROM chat_messages
			WHERE session_id = ? ORDER BY id DESC LIMIT ?
		) ORDER BY id ASC`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		var createdAt sql.NullString
		if err := rows.Scan(&m.ID, &m.Type, &m.Text, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt = createdAt.String
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetChatHistory 配置聊天记录存储（Start 前调用；nil 则历史功能关闭）。
func (s *Server) SetChatHistory(st ChatLogger) {
	s.history = st
}

// recordChat 记录一条聊天消息（存储未配置或失败时静默跳过）。
func (s *Server) recordChat(sessionID, typ, text string) {
	if s.history != nil {
		s.history.Add(sessionID, typ, text)
	}
}

// handleChatHistory 返回会话最近聊天记录（鉴权与玩家侧存档一致：auth + token）。
func (s *Server) handleChatHistory(w http.ResponseWriter, r *http.Request) {
	if s.cfg.ChatToken != "" && r.URL.Query().Get("auth") != s.cfg.ChatToken {
		http.Error(w, "未授权", http.StatusUnauthorized)
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "缺少 token", http.StatusUnauthorized)
		return
	}
	if s.history == nil {
		writeJSON(w, []ChatMessage{})
		return
	}
	list, err := s.history.List("web:"+token, 0)
	if err != nil {
		http.Error(w, "查询失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, list)
}

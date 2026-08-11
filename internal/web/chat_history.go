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
	// Add 记录一条消息，返回新行 id（失败应静默返回 0，不影响聊天主流程）。
	Add(sessionID, typ, text string) int64
	// List 取会话最近 limit 条消息（按时间升序；limit <= 0 用默认值）。
	List(sessionID string, limit int) ([]ChatMessage, error)
	// Replace 用给定消息整体覆盖会话聊天记录（存档恢复回放用；保留原 type/text/created_at 顺序）。
	Replace(sessionID string, msgs []ChatMessage) error

	// ---- AI 回复多版本（重试/编辑，见 chat_messages.go）----

	// AddReplyVersion 为 reply 消息追加一个新版本并激活（首次调用先把现有文本落为 v1）。
	// 返回新版本号与总版本数。
	AddReplyVersion(messageID int64, text string) (versionNo, count int, err error)
	// SetActiveVersion 切换激活版本，返回激活文本与总版本数。
	SetActiveVersion(messageID int64, versionNo int) (text string, count int, err error)
	// UpdateActiveText 编辑消息当前文本（有版本记录时同步更新激活版本行）。
	UpdateActiveText(messageID int64, text string) error
	// GetMessage 取单条消息（校验属于该会话；含版本聚合）。
	GetMessage(sessionID string, messageID int64) (*ChatMessage, error)
	// LastReplyOf 取会话最新一条 reply 消息（无则 nil；重试校验用）。
	LastReplyOf(sessionID string) (*ChatMessage, error)
	// HasNewerUserAfter 检查指定消息之后是否存在更新的 user 消息（重试校验用）。
	HasNewerUserAfter(sessionID string, messageID int64) (bool, error)
	// UserTextBefore 取指定消息之前最近一条 user 消息文本（无快照重试的兜底输入）。
	UserTextBefore(sessionID string, messageID int64) (string, bool)
	// DeleteMessage 删除一条消息（级联删版本行；校验属于该会话）。
	DeleteMessage(sessionID string, messageID int64) error
}

// ChatMessage 一条聊天记录。
type ChatMessage struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"` // user / reply / push
	Text      string `json:"text"`
	CreatedAt string `json:"created_at"`
	// VersionNo / VersionCount 仅对有多版本的 reply 消息 >0（无版本时为 0），
	// 前端据此判断是否显示版本切换器。
	VersionNo    int `json:"version_no"`
	VersionCount int `json:"version_count"`
}

// ChatHistoryStore 聊天记录 SQLite 存储（ChatLogger 的默认实现）。
type ChatHistoryStore struct {
	db *sql.DB
}

// NewChatHistoryStore 创建聊天记录存储（建表 chat_messages / chat_reply_versions）。
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
		// AI 回复多版本：承载全部版本，chat_messages.text 始终镜像当前激活版本
		// （不改 chat_messages 表结构、无排序陷阱）。
		`CREATE TABLE IF NOT EXISTS chat_reply_versions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			message_id INTEGER NOT NULL,
			version_no INTEGER NOT NULL,
			text TEXT NOT NULL,
			active INTEGER NOT NULL DEFAULT 0,
			created_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reply_versions_msg ON chat_reply_versions(message_id, version_no)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, fmt.Errorf("建聊天记录表失败: %w", err)
		}
	}
	return &ChatHistoryStore{db: db}, nil
}

// Add 记录一条消息（返回新行 id，失败返回 0），并滚动清理该会话超出保留上限的旧消息。
func (s *ChatHistoryStore) Add(sessionID, typ, text string) int64 {
	if text == "" {
		return 0
	}
	res, err := s.db.Exec(
		`INSERT INTO chat_messages (session_id, type, text, created_at) VALUES (?, ?, ?, ?)`,
		sessionID, typ, text, time.Now().Format("2006-01-02 15:04:05"))
	if err != nil {
		return 0 // 记录失败不影响聊天主流程
	}
	id, _ := res.LastInsertId()
	// 滚动保留最近 N 条（级联清理被清消息的版本行）
	_, _ = s.db.Exec(
		`DELETE FROM chat_reply_versions WHERE message_id IN (
			SELECT id FROM chat_messages WHERE session_id = ? AND id NOT IN (
				SELECT id FROM chat_messages WHERE session_id = ? ORDER BY id DESC LIMIT ?
			))`, sessionID, sessionID, chatHistoryKeep)
	_, _ = s.db.Exec(
		`DELETE FROM chat_messages WHERE session_id = ? AND id NOT IN (
			SELECT id FROM chat_messages WHERE session_id = ? ORDER BY id DESC LIMIT ?
		)`, sessionID, sessionID, chatHistoryKeep)
	return id
}

// List 取会话最近 limit 条消息（按时间升序；reply 消息附带版本聚合）。
func (s *ChatHistoryStore) List(sessionID string, limit int) ([]ChatMessage, error) {
	if limit <= 0 {
		limit = chatHistoryLimit
	}
	rows, err := s.db.Query(
		`SELECT m.id, m.type, m.text, m.created_at,
			(SELECT COUNT(*) FROM chat_reply_versions v WHERE v.message_id = m.id),
			COALESCE((SELECT v.version_no FROM chat_reply_versions v
				WHERE v.message_id = m.id AND v.active = 1), 0)
		FROM (
			SELECT id, type, text, created_at FROM chat_messages
			WHERE session_id = ? ORDER BY id DESC LIMIT ?
		) m ORDER BY m.id ASC`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		var createdAt sql.NullString
		if err := rows.Scan(&m.ID, &m.Type, &m.Text, &createdAt, &m.VersionCount, &m.VersionNo); err != nil {
			return nil, err
		}
		m.CreatedAt = createdAt.String
		out = append(out, m)
	}
	return out, rows.Err()
}

// Replace 用给定消息整体覆盖会话聊天记录（删该会话全部行 + 事务内批量插入）。
// 级联清空该会话全部版本行（存档快照不含版本信息，恢复后版本历史丢弃，已知取舍）。
func (s *ChatHistoryStore) Replace(sessionID string, msgs []ChatMessage) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM chat_reply_versions WHERE message_id IN (
		SELECT id FROM chat_messages WHERE session_id = ?)`, sessionID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM chat_messages WHERE session_id = ?`, sessionID); err != nil {
		return err
	}
	stmt, err := tx.Prepare(`INSERT INTO chat_messages (session_id, type, text, created_at) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, m := range msgs {
		createdAt := m.CreatedAt
		if createdAt == "" {
			createdAt = time.Now().Format("2006-01-02 15:04:05")
		}
		if _, err := stmt.Exec(sessionID, m.Type, m.Text, createdAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ============================================================
// AI 回复多版本（重试 / 编辑）
// ============================================================

// now 时间戳格式（与 chat_messages.created_at 一致）。
func nowText() string { return time.Now().Format("2006-01-02 15:04:05") }

// versionCount 取消息版本数（事务内用）。
func versionCount(tx *sql.Tx, messageID int64) (int, error) {
	var n int
	err := tx.QueryRow(`SELECT COUNT(*) FROM chat_reply_versions WHERE message_id = ?`, messageID).Scan(&n)
	return n, err
}

// AddReplyVersion 为 reply 消息追加新版本并置为激活：
// 首次调用时先把 chat_messages 现有文本落为 v1（active=0），再插新文本为最新版本；
// chat_messages.text 始终镜像激活版本。全程事务。
func (s *ChatHistoryStore) AddReplyVersion(messageID int64, text string) (int, int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	var current string
	if err := tx.QueryRow(`SELECT text FROM chat_messages WHERE id = ?`, messageID).Scan(&current); err != nil {
		return 0, 0, fmt.Errorf("消息不存在: %w", err)
	}
	count, err := versionCount(tx, messageID)
	if err != nil {
		return 0, 0, err
	}
	versionNo := count + 1
	if count == 0 {
		// 首次重试：现有文本落为 v1（非激活），新回复为 v2
		if _, err := tx.Exec(
			`INSERT INTO chat_reply_versions (message_id, version_no, text, active, created_at) VALUES (?, 1, ?, 0, ?)`,
			messageID, current, nowText()); err != nil {
			return 0, 0, err
		}
		versionNo = 2
		count = 1
	}
	if _, err := tx.Exec(`UPDATE chat_reply_versions SET active = 0 WHERE message_id = ?`, messageID); err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(
		`INSERT INTO chat_reply_versions (message_id, version_no, text, active, created_at) VALUES (?, ?, ?, 1, ?)`,
		messageID, versionNo, text, nowText()); err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(`UPDATE chat_messages SET text = ? WHERE id = ?`, text, messageID); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return versionNo, count + 1, nil
}

// SetActiveVersion 切换激活版本（镜像 text 到 chat_messages），返回激活文本与总版本数。
func (s *ChatHistoryStore) SetActiveVersion(messageID int64, versionNo int) (string, int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()

	var text string
	if err := tx.QueryRow(
		`SELECT text FROM chat_reply_versions WHERE message_id = ? AND version_no = ?`,
		messageID, versionNo).Scan(&text); err != nil {
		return "", 0, fmt.Errorf("版本不存在: %w", err)
	}
	count, err := versionCount(tx, messageID)
	if err != nil {
		return "", 0, err
	}
	if _, err := tx.Exec(`UPDATE chat_reply_versions SET active = CASE WHEN version_no = ? THEN 1 ELSE 0 END
		WHERE message_id = ?`, versionNo, messageID); err != nil {
		return "", 0, err
	}
	if _, err := tx.Exec(`UPDATE chat_messages SET text = ? WHERE id = ?`, text, messageID); err != nil {
		return "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	return text, count, nil
}

// UpdateActiveText 编辑消息文本：更新 chat_messages.text；有版本记录时同步更新激活版本行。
func (s *ChatHistoryStore) UpdateActiveText(messageID int64, text string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE chat_messages SET text = ? WHERE id = ?`, text, messageID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("消息不存在")
	}
	if _, err := tx.Exec(`UPDATE chat_reply_versions SET text = ? WHERE message_id = ? AND active = 1`,
		text, messageID); err != nil {
		return err
	}
	return tx.Commit()
}

// GetMessage 取单条消息并校验属于该会话（含版本聚合；不存在或不属于该会话返回错误）。
func (s *ChatHistoryStore) GetMessage(sessionID string, messageID int64) (*ChatMessage, error) {
	var m ChatMessage
	var createdAt sql.NullString
	err := s.db.QueryRow(
		`SELECT m.id, m.type, m.text, m.created_at,
			(SELECT COUNT(*) FROM chat_reply_versions v WHERE v.message_id = m.id),
			COALESCE((SELECT v.version_no FROM chat_reply_versions v
				WHERE v.message_id = m.id AND v.active = 1), 0)
		FROM chat_messages m WHERE m.id = ? AND m.session_id = ?`, messageID, sessionID).
		Scan(&m.ID, &m.Type, &m.Text, &createdAt, &m.VersionCount, &m.VersionNo)
	if err != nil {
		return nil, fmt.Errorf("消息不存在或不属于本会话: %w", err)
	}
	m.CreatedAt = createdAt.String
	return &m, nil
}

// LastReplyOf 取会话最新一条 reply 消息（无则返回 nil, nil）。
func (s *ChatHistoryStore) LastReplyOf(sessionID string) (*ChatMessage, error) {
	var m ChatMessage
	var createdAt sql.NullString
	err := s.db.QueryRow(
		`SELECT id, type, text, created_at FROM chat_messages
		WHERE session_id = ? AND type = 'reply' ORDER BY id DESC LIMIT 1`, sessionID).
		Scan(&m.ID, &m.Type, &m.Text, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.CreatedAt = createdAt.String
	return &m, nil
}

// HasNewerUserAfter 检查指定消息之后是否存在更新的 user 消息。
func (s *ChatHistoryStore) HasNewerUserAfter(sessionID string, messageID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM chat_messages WHERE session_id = ? AND type = 'user' AND id > ?`,
		sessionID, messageID).Scan(&n)
	return n > 0, err
}

// UserTextBefore 取指定消息之前最近一条 user 消息文本（无则 ok=false）。
func (s *ChatHistoryStore) UserTextBefore(sessionID string, messageID int64) (string, bool) {
	var text string
	err := s.db.QueryRow(
		`SELECT text FROM chat_messages WHERE session_id = ? AND type = 'user' AND id < ?
		ORDER BY id DESC LIMIT 1`, sessionID, messageID).Scan(&text)
	if err != nil {
		return "", false
	}
	return text, true
}

// DeleteMessage 删除一条消息（事务内级联删版本行；不存在或不属于该会话返回错误）。
func (s *ChatHistoryStore) DeleteMessage(sessionID string, messageID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`DELETE FROM chat_messages WHERE id = ? AND session_id = ?`, messageID, sessionID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("消息不存在或不属于本会话")
	}
	if _, err := tx.Exec(`DELETE FROM chat_reply_versions WHERE message_id = ?`, messageID); err != nil {
		return err
	}
	return tx.Commit()
}

// SetChatHistory 配置聊天记录存储（Start 前调用；nil 则历史功能关闭）。
func (s *Server) SetChatHistory(st ChatLogger) {
	s.history = st
}

// recordChat 记录一条聊天消息，返回新行 id（存储未配置或失败时静默返回 0）。
func (s *Server) recordChat(sessionID, typ, text string) int64 {
	if s.history != nil {
		return s.history.Add(sessionID, typ, text)
	}
	return 0
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

// Package gamelog 提供跑团日志记录功能。
// 实现 core.Hook 接口，在 TRPG 模式下自动记录所有玩家发言和 AI KP 回复。
//
// 这是「功能层」与「Agent 层」联动的典型例子：
//   - 功能层: GameLogger 提供 .log 指令管理和导出日志
//   - Agent 层: AI KP 的回复通过 Hook.OnReply 自动被记录
//   - 联动: 玩家无需手动记录，系统自动完成
package gamelog

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
)

// Entry 是一条日志记录。
type Entry struct {
	Timestamp string `json:"timestamp"` // 时间
	Role      string `json:"role"`      // user / assistant / system
	UserID    string `json:"user_id"`   // 发送者 ID
	Content   string `json:"content"`   // 消息内容
}

// LogSession 是一次跑团的日志会话。
type LogSession struct {
	SessionID string    `json:"session_id"`
	Started   time.Time `json:"started"`
	Ended     time.Time `json:"ended,omitempty"`
	Note      string    `json:"note"`
	Entries   []Entry   `json:"entries"`
}

// GameLogger 是跑团日志记录器，实现 core.Hook 接口。
type GameLogger struct {
	mu       sync.RWMutex
	sessions map[string]*LogSession // sessionID -> 日志会话
}

// NewGameLogger 创建日志记录器。
func NewGameLogger() *GameLogger {
	return &GameLogger{
		sessions: make(map[string]*LogSession),
	}
}

// Name 实现 core.Hook 接口。
func (g *GameLogger) Name() string { return "gamelog" }

// OnBeforeProcess 实现 core.Hook 接口。
// 在消息处理前记录玩家发言。
func (g *GameLogger) OnBeforeProcess(ctx *core.MessageContext) {
	if g.IsRecording(ctx.SessionID) {
		g.RecordUserMessage(ctx.SessionID, ctx.UserID, ctx.Content)
	}
}

// OnReply 实现 core.Hook 接口。
// 在 AI 回复后记录回复内容。
func (g *GameLogger) OnReply(ctx *core.MessageContext, reply string) {
	if g.IsRecording(ctx.SessionID) {
		g.RecordAssistantMessage(ctx.SessionID, reply)
	}
}

// StartSession 开始记录一个跑团日志会话。
func (g *GameLogger) StartSession(sessionID, note string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.sessions[sessionID]; exists {
		return fmt.Errorf("会话 %s 的日志已在记录中", sessionID)
	}
	g.sessions[sessionID] = &LogSession{
		SessionID: sessionID,
		Started:   time.Now(),
		Note:      note,
		Entries:   make([]Entry, 0),
	}
	log.Printf("[GameLogger] 开始记录会话 %s", sessionID)
	return nil
}

// EndSession 结束日志记录，返回记录的消息数。
func (g *GameLogger) EndSession(sessionID string) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.sessions[sessionID]
	if !ok {
		return 0, fmt.Errorf("会话 %s 无日志记录", sessionID)
	}
	s.Ended = time.Now()
	count := len(s.Entries)
	log.Printf("[GameLogger] 结束记录会话 %s, 共 %d 条", sessionID, count)
	return count, nil
}

// GetEntries 获取会话的日志条目。
func (g *GameLogger) GetEntries(sessionID string) ([]Entry, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s, ok := g.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("会话 %s 无日志记录", sessionID)
	}
	result := make([]Entry, len(s.Entries))
	copy(result, s.Entries)
	return result, nil
}

// Export 导出日志为 JSON。
func (g *GameLogger) Export(sessionID string) ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	s, ok := g.sessions[sessionID]
	if !ok {
		return nil, fmt.Errorf("会话 %s 无日志记录", sessionID)
	}
	return json.MarshalIndent(s, "", "  ")
}

// RecordUserMessage 记录玩家消息。
// 由 Bot 路由器在 TRPG 模式下调用。
func (g *GameLogger) RecordUserMessage(sessionID, userID, content string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.sessions[sessionID]
	if !ok {
		return // 未开始记录，忽略
	}
	s.Entries = append(s.Entries, Entry{
		Timestamp: time.Now().Format("15:04:05"),
		Role:      "user",
		UserID:    userID,
		Content:   content,
	})
}

// RecordAssistantMessage 记录 AI 回复。
// 由 Bot 路由器在 AI Agent 回复后调用。
func (g *GameLogger) RecordAssistantMessage(sessionID, content string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	s, ok := g.sessions[sessionID]
	if !ok {
		return
	}
	s.Entries = append(s.Entries, Entry{
		Timestamp: time.Now().Format("15:04:05"),
		Role:      "assistant",
		Content:   content,
	})
}

// IsRecording 判断指定会话是否正在记录日志。
func (g *GameLogger) IsRecording(sessionID string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	_, ok := g.sessions[sessionID]
	return ok
}

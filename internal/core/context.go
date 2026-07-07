// Package core 定义机器人框架的核心抽象层。
// 包含统一消息上下文、会话管理、Handler 接口和 Hook 机制，
// 使「指令/插件功能」与「AI Agent 功能」可以独立开发并联动协作。
package core

import (
	"context"
	"sync"
)

// SessionMode 会话模式，决定消息如何路由。
type SessionMode int

const (
	ModeNormal  SessionMode = iota // 普通模式：仅响应指令
	ModeTRPG                       // 跑团模式：AI KP 主持 + 指令 + 日志记录
	ModeFreeChat                   // 自由聊天模式：所有消息交给 AI Agent
)

// String 返回模式的可读名称。
func (m SessionMode) String() string {
	switch m {
	case ModeNormal:
		return "normal"
	case ModeTRPG:
		return "trpg"
	case ModeFreeChat:
		return "freechat"
	default:
		return "unknown"
	}
}

// MessageSource 消息来源类型。
type MessageSource int

const (
	SourceGroup    MessageSource = iota // 群聊
	SourceC2C                           // 单聊
	SourceChannel                       // 频道
)

// MessageContext 是贯穿整个消息处理流程的统一上下文。
// Handler 和 Agent 都通过它获取消息信息、访问 API、操作会话状态。
type MessageContext struct {
	Ctx       context.Context // 原始 context
	Source    MessageSource   // 消息来源
	SessionID string          // 会话 ID（group:xxx 或 c2c:xxx）
	UserID    string          // 发送者 ID（openid）
	OpenID    string          // 回复目标 openid
	MsgID     string          // 消息 ID，用于被动回复
	Content   string          // 消息文本内容
	IsGroup   bool            // 是否群聊

	// 共享数据区，Handler 和 Agent 可通过它传递信息
	Extra map[string]interface{}
}

// Reply 回复消息。由 Bot 层注入实际的发送函数。
type ReplyFunc func(ctx context.Context, openid, msgID, text string, isGroup bool) error

// Session 是一个独立的会话状态（对应一个 QQ 群或私聊）。
// 跨 Handler 和 Agent 共享，支持联动场景。
type Session struct {
	ID      string
	Mode    SessionMode
	AgentID string                 // 当前会话使用的 Agent ID（如 "kp", "npc"）
	Data    map[string]interface{} // 共享游戏状态

	mu sync.RWMutex
}

// Lock 加锁。
func (s *Session) Lock() { s.mu.Lock() }

// Unlock 解锁。
func (s *Session) Unlock() { s.mu.Unlock() }

// RLock 读锁。
func (s *Session) RLock() { s.mu.RLock() }

// RUnlock 读解锁。
func (s *Session) RUnlock() { s.mu.RUnlock() }

// Set 设置共享数据。
func (s *Session) Set(key string, value interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Data == nil {
		s.Data = make(map[string]interface{})
	}
	s.Data[key] = value
}

// Get 获取共享数据。
func (s *Session) Get(key string) (interface{}, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.Data[key]
	return v, ok
}

// SessionManager 管理所有会话状态。
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewSessionManager 创建会话管理器。
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// GetSession 获取或创建会话。
func (sm *SessionManager) GetSession(id string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if s, ok := sm.sessions[id]; ok {
		return s
	}
	s := &Session{
		ID:   id,
		Mode: ModeNormal,
		Data: make(map[string]interface{}),
	}
	sm.sessions[id] = s
	return s
}

// SetMode 设置会话模式。
func (sm *SessionManager) SetMode(id string, mode SessionMode) {
	sm.GetSession(id).Mode = mode
}

// GetMode 获取会话模式。
func (sm *SessionManager) GetMode(id string) SessionMode {
	return sm.GetSession(id).Mode
}

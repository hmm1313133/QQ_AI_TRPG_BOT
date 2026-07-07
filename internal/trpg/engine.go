// Package trpg 是 TRPG 游戏核心引擎，
// 提供骰子、角色卡、规则集和模组管理功能。
package trpg

import (
	"fmt"
	"sync"
)

// Engine 是 TRPG 游戏引擎，管理游戏状态和规则。
type Engine struct {
	mu        sync.RWMutex
	ruleSets  map[string]RuleSet          // 注册的规则集
	sessions  map[string]*Session         // 按 sessionID 隔离的游戏会话
}

// NewEngine 创建 TRPG 引擎实例并加载默认规则集。
func NewEngine() *Engine {
	e := &Engine{
		ruleSets: make(map[string]RuleSet),
		sessions: make(map[string]*Session),
	}
	// TODO: 注册默认规则集 (CoC7, DnD5e)
	return e
}

// Session 表示一个独立的 TRPG 游戏会话（对应一个 QQ 群/私聊）。
type Session struct {
	ID         string
	RuleSet    RuleSet
	Characters map[string]*Character      // userID -> 角色
	Module     string                      // 当前运行的模组名称
	State      map[string]interface{}      // 游戏状态数据
}

// GetSession 获取或创建指定 sessionID 的游戏会话。
func (e *Engine) GetSession(sessionID string) *Session {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s, ok := e.sessions[sessionID]; ok {
		return s
	}
	s := &Session{
		ID:         sessionID,
		Characters: make(map[string]*Character),
		State:      make(map[string]interface{}),
	}
	e.sessions[sessionID] = s
	return s
}

// RollDice 执行骰子表达式投掷。
func (e *Engine) RollDice(sessionID, expr string) (*RollResult, error) {
	session := e.GetSession(sessionID)
	if session.RuleSet == nil {
		return nil, fmt.Errorf("未设置规则集")
	}
	return session.RuleSet.Roll(expr)
}

// RuleSet 接口定义 TRPG 规则集需要实现的能力。
type RuleSet interface {
	Name() string
	Roll(diceExpr string) (*RollResult, error)
	Check(action string, char *Character) (*CheckResult, error)
}

// Character 表示玩家角色卡。
type Character struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Player   string                 `json:"player"`    // QQ userID
	Attrs    map[string]int         `json:"attrs"`     // 属性值
	Skills   map[string]int         `json:"skills"`    // 技能值
	Extra    map[string]interface{} `json:"extra"`     // 额外数据
}

// RollResult 骰子投掷结果。
type RollResult struct {
	Expr    string `json:"expr"`     // 骰子表达式，如 "3d6"
	Rolls   []int  `json:"rolls"`    // 每个骰子的结果
	Total   int    `json:"total"`    // 总计
	Detail  string `json:"detail"`   // 可读描述
}

// CheckResult 技能检定结果。
type CheckResult struct {
	Skill     string `json:"skill"`
	Value     int    `json:"value"`      // 角色技能值
	Roll      int    `json:"roll"`       // 骰点结果
	Success   bool   `json:"success"`    // 是否成功
	Level     string `json:"level"`      // 成功等级: 大成功/困难成功/大失败 等
}

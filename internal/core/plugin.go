package core

import (
	"context"
	"fmt"
	"log"
	"sync"
)

// PluginManager 是插件/Handler 注册中心。
// 管理所有 Handler 和 Agent，支持按优先级匹配。
type PluginManager struct {
	mu       sync.RWMutex
	handlers []Handler            // 按注册顺序排列的 Handler
	agents   map[string]AgentHandler // AgentID -> Agent
	hooks    []Hook               // 全局 Hook 列表
}

// NewPluginManager 创建插件管理器。
func NewPluginManager() *PluginManager {
	return &PluginManager{
		agents: make(map[string]AgentHandler),
	}
}

// RegisterHandler 注册一个指令处理器。
func (pm *PluginManager) RegisterHandler(h Handler) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.handlers = append(pm.handlers, h)
	log.Printf("[Plugin] 注册 Handler: %s", h.Name())
}

// RegisterAgent 注册一个 AI Agent。
func (pm *PluginManager) RegisterAgent(a AgentHandler) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	id := a.AgentID()
	if _, exists := pm.agents[id]; exists {
		return fmt.Errorf("Agent %s 已存在", id)
	}
	pm.agents[id] = a
	log.Printf("[Plugin] 注册 Agent: %s", id)
	return nil
}

// RegisterHook 注册一个全局 Hook。
func (pm *PluginManager) RegisterHook(h Hook) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.hooks = append(pm.hooks, h)
	log.Printf("[Plugin] 注册 Hook: %s", h.Name())
}

// GetAgent 获取指定 ID 的 Agent。
func (pm *PluginManager) GetAgent(id string) (AgentHandler, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	agent, ok := pm.agents[id]
	if !ok {
		return nil, fmt.Errorf("Agent %s 未注册", id)
	}
	return agent, nil
}

// MatchHandler 遍历已注册的 Handler，返回第一个匹配的。
func (pm *PluginManager) MatchHandler(ctx *MessageContext) Handler {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for _, h := range pm.handlers {
		if h.Match(ctx) {
			return h
		}
	}
	return nil
}

// ExecuteHandler 执行指定 Handler。
// 会触发 Hook.OnBeforeProcess 和 Hook.OnReply。
func (pm *PluginManager) ExecuteHandler(ctx *MessageContext, h Handler, reply ReplyFunc) error {
	// 前置 Hook
	pm.runBeforeHooks(ctx)

	// 包装 reply 以触发 OnReply Hook
	wrappedReply := func(_ context.Context, openid, msgID, text string, isGroup bool) error {
		pm.runReplyHooks(ctx, text)
		return reply(nil, openid, msgID, text, isGroup)
	}

	err := h.Execute(ctx, wrappedReply)
	if err != nil {
		log.Printf("[Plugin] Handler %s 执行失败: %v", h.Name(), err)
	}
	return err
}

// ChatAgent 调用 AI Agent 处理对话消息。
// 触发 Hook 并返回回复。
func (pm *PluginManager) ChatAgent(ctx *MessageContext, session *Session, reply ReplyFunc) error {
	// 前置 Hook
	pm.runBeforeHooks(ctx)

	// 获取会话指定的 Agent，默认 "kp"
	agentID := session.AgentID
	if agentID == "" {
		agentID = "kp"
	}

	agent, err := pm.GetAgent(agentID)
	if err != nil {
		// 没有可用 Agent，回退到提示
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "AI Agent 不可用", ctx.IsGroup)
	}

	// 调用 Agent
	resp, err := agent.Chat(ctx, session)
	if err != nil {
		log.Printf("[Plugin] Agent %s 处理失败: %v", agentID, err)
		resp = "处理消息时出错，请稍后重试。"
	}

	// 触发 OnReply Hook
	pm.runReplyHooks(ctx, resp)

	// 发送回复
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// runBeforeHooks 执行所有前置 Hook。
func (pm *PluginManager) runBeforeHooks(ctx *MessageContext) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for _, h := range pm.hooks {
		h.OnBeforeProcess(ctx)
	}
}

// runReplyHooks 执行所有回复 Hook。
func (pm *PluginManager) runReplyHooks(ctx *MessageContext, reply string) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for _, h := range pm.hooks {
		h.OnReply(ctx, reply)
	}
}

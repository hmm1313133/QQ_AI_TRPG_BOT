// Package core - Router 统一消息路由（渠道共用）。
//
// 渠道只负责构建 MessageContext 与提供 ReplyFunc。
//
// 路由策略:
//  1. 指令消息 (以 . 开头) → 优先匹配 Handler（当前无注册 Handler，保留扩展点）
//  2. 其余消息一律交给 AI Agent；会话模式只影响 Agent 侧是否注入世界上下文
//    （normal=纯对话，freechat=世界内扮演，见 agent.TurnEngine）
package core

import (
	"context"
	"strings"
)

// Router 统一消息路由器。
type Router struct {
	Plugins  *PluginManager
	Sessions *SessionManager
}

// NewRouter 创建路由器。
func NewRouter(plugins *PluginManager, sessions *SessionManager) *Router {
	return &Router{Plugins: plugins, Sessions: sessions}
}

// Route 处理一条消息。reply 由渠道提供。
func (r *Router) Route(mc *MessageContext, reply ReplyFunc) {
	// 1. 尝试匹配指令 Handler (以 . 开头的消息)
	if strings.HasPrefix(mc.Content, ".") {
		if handler := r.Plugins.MatchHandler(mc); handler != nil {
			_ = r.Plugins.ExecuteHandler(mc, handler, reply)
			return
		}
		// 无匹配指令：落入下方统一转 Agent
	}

	// 2. 其余消息一律交给 AI Agent
	session := r.Sessions.GetSession(mc.SessionID)
	r.Plugins.ChatAgent(mc, session, func(ctx context.Context, openid, msgID, text string, isGroup bool) error {
		return reply(ctx, openid, msgID, text, isGroup)
	})
}

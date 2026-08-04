// Package core - Router 统一消息路由（渠道共用）。
//
// 自 internal/bot route() 抽取：QQ 与 Web 渠道共用同一份路由逻辑，
// 渠道只负责构建 MessageContext 与提供 ReplyFunc。
//
// 路由策略:
//  1. 指令消息 (以 . 开头) → 优先匹配 Handler
//  2. 非指令消息，根据会话模式:
//     - ModeNormal: 忽略
//     - ModeTRPG / ModeFreeChat: 交给 AI Agent
package core

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// MessageRecorder 消息日志记录器（由 gamelog.GameLogger 隐式实现）。
// 定义在 core 以避免 core -> gamelog 的循环依赖。
type MessageRecorder interface {
	IsRecording(sessionID string) bool
	RecordUserMessage(sessionID, userID, content string)
	RecordAssistantMessage(sessionID, content string)
}

// Router 统一消息路由器。
type Router struct {
	Plugins  *PluginManager
	Sessions *SessionManager
	Recorder MessageRecorder // 可为 nil
}

// NewRouter 创建路由器。
func NewRouter(plugins *PluginManager, sessions *SessionManager, recorder MessageRecorder) *Router {
	return &Router{Plugins: plugins, Sessions: sessions, Recorder: recorder}
}

// Route 处理一条消息。reply 由渠道提供。
func (r *Router) Route(mc *MessageContext, reply ReplyFunc) {
	// TRPG/FreeChat 模式下，记录玩家消息到日志
	if r.Recorder != nil && r.Recorder.IsRecording(mc.SessionID) {
		r.Recorder.RecordUserMessage(mc.SessionID, mc.UserID, mc.Content)
	}

	// 文件附件处理：缓存到会话，支持文件和指令分条发送的场景
	if mc.HasFileAttachment() {
		fileAtt := mc.GetFileAttachment()
		session := r.Sessions.GetSession(mc.SessionID)
		session.Set("last_file_attachment", fileAtt)
		session.Set("last_file_time", time.Now().Unix())

		log.Printf("[Router] 收到文件附件: %s (%s) session=%s",
			fileAtt.Filename, fileAtt.ContentType, mc.SessionID)

		// 场景: 用户先发 .script upload，再发文件 → 等待状态触发
		if waiting, ok := session.Get("waiting_script_upload"); ok {
			if w, ok := waiting.(bool); ok && w {
				session.Set("waiting_script_upload", false)
				mc.Content = ".script upload"
				if handler := r.Plugins.MatchHandler(mc); handler != nil {
					if err := handler.Execute(mc, reply); err != nil {
						log.Printf("[Router] Handler %s 执行失败: %v", handler.Name(), err)
					}
				}
				return
			}
		}

		// 文件已缓存，但没有指令文本 → 提示用户
		if mc.Content == "" {
			_ = reply(mc.Ctx, mc.OpenID, mc.MsgID,
				fmt.Sprintf("收到文件: %s\n如需上传剧本，请发送 .script upload", fileAtt.Filename),
				mc.IsGroup)
			return
		}
		// 文件 + 指令同时发送 → 走正常指令路由（ScriptHandler 会处理附件）
	}

	// 1. 尝试匹配指令 Handler (以 . 开头的消息)
	if strings.HasPrefix(mc.Content, ".") {
		handler := r.Plugins.MatchHandler(mc)
		if handler != nil {
			if err := handler.Execute(mc, reply); err != nil {
				log.Printf("[Router] Handler %s 执行失败: %v", handler.Name(), err)
			}
			return
		}
		// 未匹配的指令
		_ = reply(mc.Ctx, mc.OpenID, mc.MsgID, "未知指令，输入 .help 查看帮助", mc.IsGroup)
		return
	}

	// 2. 非指令消息，根据会话模式路由
	session := r.Sessions.GetSession(mc.SessionID)
	switch session.Mode {
	case ModeNormal:
		// 普通模式，不处理非指令消息
		return

	case ModeTRPG, ModeFreeChat:
		// 交给 AI Agent 处理
		r.Plugins.ChatAgent(mc, session, func(ctx context.Context, openid, msgID, text string, isGroup bool) error {
			// AI 回复也记录到日志
			if r.Recorder != nil && r.Recorder.IsRecording(mc.SessionID) {
				r.Recorder.RecordAssistantMessage(mc.SessionID, text)
			}
			return reply(ctx, openid, msgID, text, isGroup)
		})

	default:
		log.Printf("[Router] 未知会话模式: %s", session.Mode)
	}
}

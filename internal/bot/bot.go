// Package bot 负责 QQ 消息接收和路由分发。
// 它是整个框架的「消息入口层」，将消息路由到:
//   - Handler 层: 指令匹配 → 确定性功能 (骰子/角色卡/日志等)
//   - Agent 层: 对话消息 → AI Agent (KP/DM 等)
//   - 联动模式: TRPG 模式下两者协作 (AI主持 + 自动日志)
package bot

import (
	"context"
	"log"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/gamelog"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/pkg/qqbot"
)

// Config 是 Bot 的配置。
type Config struct {
	AppID        string
	ClientSecret string
}

// Bot 是 QQ 机器人实例，负责消息路由和组件编排。
type Bot struct {
	config     *Config
	qqBot      *qqbot.Bot
	plugins    *core.PluginManager
	sessions   *core.SessionManager
	gameLogger *gamelog.GameLogger
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewBot 创建 Bot 实例。
// 所有功能组件通过 PluginManager 注册，Bot 只负责路由。
func NewBot(cfg *Config, plugins *core.PluginManager, sessions *core.SessionManager, gameLogger *gamelog.GameLogger) (*Bot, error) {
	qqBot := qqbot.NewBot(&qqbot.Config{
		AppID:        cfg.AppID,
		ClientSecret: cfg.ClientSecret,
	})

	b := &Bot{
		config:     cfg,
		qqBot:      qqBot,
		plugins:    plugins,
		sessions:   sessions,
		gameLogger: gameLogger,
	}

	b.registerQQHandlers()
	return b, nil
}

// registerQQHandlers 注册 QQ 消息事件处理函数，将消息转为统一 MessageContext。
func (b *Bot) registerQQHandlers() {
	// 群聊@机器人消息
	b.qqBot.OnGroupAtMessage(func(ctx *qqbot.EventContext, msg *qqbot.GroupMessageEvent) {
		content := strings.TrimSpace(msg.Content)
		log.Printf("[Bot] 群聊消息 group=%s user=%s content=%q", msg.GroupOpenid, msg.Author.MemberOpenid, content)

		mc := &core.MessageContext{
			Ctx:       context.Background(),
			Source:    core.SourceGroup,
			SessionID: "group:" + msg.GroupOpenid,
			UserID:    msg.Author.MemberOpenid,
			OpenID:    msg.GroupOpenid,
			MsgID:     msg.ID,
			Content:   content,
			IsGroup:   true,
			Extra:     make(map[string]interface{}),
		}
		b.route(mc)
	})

	// 单聊消息
	b.qqBot.OnC2CMessage(func(ctx *qqbot.EventContext, msg *qqbot.C2CMessageEvent) {
		content := strings.TrimSpace(msg.Content)
		log.Printf("[Bot] 单聊消息 user=%s content=%q", msg.Author.UserOpenid, content)

		mc := &core.MessageContext{
			Ctx:       context.Background(),
			Source:    core.SourceC2C,
			SessionID: "c2c:" + msg.Author.UserOpenid,
			UserID:    msg.Author.UserOpenid,
			OpenID:    msg.Author.UserOpenid,
			MsgID:     msg.ID,
			Content:   content,
			IsGroup:   false,
			Extra:     make(map[string]interface{}),
		}
		b.route(mc)
	})

	// 机器人加入/退出群聊
	b.qqBot.OnGroupAddRobot(func(ctx *qqbot.EventContext, event *qqbot.GroupRobotEvent) {
		log.Printf("[Bot] 被添加到群聊 group=%s", event.GroupOpenid)
	})
	b.qqBot.OnGroupDelRobot(func(ctx *qqbot.EventContext, event *qqbot.GroupRobotEvent) {
		log.Printf("[Bot] 被移出群聊 group=%s", event.GroupOpenid)
	})
}

// route 是核心路由逻辑，根据消息内容和会话模式决定处理路径。
//
// 路由策略:
//  1. 所有指令消息 (以 . 开头) → 优先匹配 Handler
//  2. 非指令消息，根据会话模式:
//     - ModeNormal: 忽略
//     - ModeTRPG: 交给 AI Agent (KP) + 自动记录日志
//     - ModeFreeChat: 交给 AI Agent
func (b *Bot) route(mc *core.MessageContext) {
	reply := b.makeReplyFunc()

	// TRPG/FreeChat 模式下，记录玩家消息到日志
	if b.gameLogger.IsRecording(mc.SessionID) {
		b.gameLogger.RecordUserMessage(mc.SessionID, mc.UserID, mc.Content)
	}

	// 1. 尝试匹配指令 Handler (以 . 开头的消息)
	if strings.HasPrefix(mc.Content, ".") {
		handler := b.plugins.MatchHandler(mc)
		if handler != nil {
			if err := handler.Execute(mc, reply); err != nil {
				log.Printf("[Bot] Handler %s 执行失败: %v", handler.Name(), err)
			}
			return
		}
		// 未匹配的指令
		reply(mc.Ctx, mc.OpenID, mc.MsgID, "未知指令，输入 .help 查看帮助", mc.IsGroup)
		return
	}

	// 2. 非指令消息，根据会话模式路由
	session := b.sessions.GetSession(mc.SessionID)
	switch session.Mode {
	case core.ModeNormal:
		// 普通模式，不处理非指令消息
		return

	case core.ModeTRPG, core.ModeFreeChat:
		// 交给 AI Agent 处理
		b.plugins.ChatAgent(mc, session, func(ctx context.Context, openid, msgID, text string, isGroup bool) error {
			// AI 回复也记录到日志
			if b.gameLogger.IsRecording(mc.SessionID) {
				b.gameLogger.RecordAssistantMessage(mc.SessionID, text)
			}
			return b.sendReply(openid, msgID, text, isGroup)
		})

	default:
		log.Printf("[Bot] 未知会话模式: %s", session.Mode)
	}
}

// makeReplyFunc 创建标准的回复函数。
func (b *Bot) makeReplyFunc() core.ReplyFunc {
	return func(ctx context.Context, openid, msgID, text string, isGroup bool) error {
		return b.sendReply(openid, msgID, text, isGroup)
	}
}

// sendReply 发送回复消息到 QQ。
func (b *Bot) sendReply(openid, msgID, text string, isGroup bool) error {
	api := b.qqBot.API()
	var err error
	if isGroup {
		_, err = api.ReplyGroupText(context.Background(), openid, msgID, text)
	} else {
		_, err = api.ReplyC2CText(context.Background(), openid, msgID, text)
	}
	if err != nil {
		log.Printf("[Bot] 发送回复失败: %v", err)
	}
	return err
}

// Start 启动 Bot。
func (b *Bot) Start() error {
	b.ctx, b.cancel = context.WithCancel(context.Background())
	go func() {
		if err := b.qqBot.Run(b.ctx); err != nil {
			log.Printf("[Bot] 运行出错: %v", err)
		}
	}()
	return nil
}

// Stop 停止 Bot。
func (b *Bot) Stop() error {
	if b.cancel != nil {
		b.cancel()
	}
	b.qqBot.Stop()
	return nil
}

package qqbot

import (
	"context"
	"fmt"
	"log"
)

// Config 是 QQ 机器人的配置。
type Config struct {
	AppID        string  // 机器人 AppID
	ClientSecret string  // 机器人 ClientSecret
	Intents      Intent  // 事件订阅标记位，0 则使用默认值
}

// Bot 是 QQ 机器人高层客户端，整合 OpenAPI 和 WebSocket。
// 使用方式:
//
//	bot := qqbot.NewBot(cfg)
//	bot.OnGroupAtMessage(func(ctx *qqbot.EventContext, msg *qqbot.GroupMessageEvent) {
//	    ctx.API.ReplyGroupText(context.Background(), msg.GroupOpenid, msg.ID, "收到!")
//	})
//	bot.Run(context.Background())
type Bot struct {
	config     *Config
	tokenMgr   *tokenManager
	api        *OpenAPI
	wsClient   *WSClient
	dispatcher *EventDispatcher
}

// NewBot 创建 Bot 实例。
func NewBot(cfg *Config) *Bot {
	if cfg.Intents == 0 {
		cfg.Intents = DefaultIntents()
	}

	tokenMgr := newTokenManager(cfg.AppID, cfg.ClientSecret)
	api := newOpenAPI(tokenMgr)
	dispatcher := NewEventDispatcher()
	wsClient := NewWSClient(tokenMgr, api, dispatcher, cfg.Intents)

	return &Bot{
		config:     cfg,
		tokenMgr:   tokenMgr,
		api:        api,
		wsClient:   wsClient,
		dispatcher: dispatcher,
	}
}

// API 返回 OpenAPI 客户端，用于主动调用接口。
func (b *Bot) API() *OpenAPI {
	return b.api
}

// On 注册原始事件处理函数。
func (b *Bot) On(eventType string, handler EventHandler) {
	b.dispatcher.On(eventType, handler)
}

// OnC2CMessage 注册单聊消息处理函数。
func (b *Bot) OnC2CMessage(handler func(ctx *EventContext, msg *C2CMessageEvent)) {
	b.dispatcher.OnC2CMessage(handler)
}

// OnGroupAtMessage 注册群聊@机器人消息处理函数。
func (b *Bot) OnGroupAtMessage(handler func(ctx *EventContext, msg *GroupMessageEvent)) {
	b.dispatcher.OnGroupAtMessage(handler)
}

// OnGroupMessage 注册群聊全量消息处理函数。
func (b *Bot) OnGroupMessage(handler func(ctx *EventContext, msg *GroupMessageEvent)) {
	b.dispatcher.OnGroupMessage(handler)
}

// OnInteraction 注册交互事件处理函数。
func (b *Bot) OnInteraction(handler func(ctx *EventContext, event *InteractionEvent)) {
	b.dispatcher.OnInteraction(handler)
}

// OnChannelMessage 注册频道消息处理函数。
// 适用于 AT_MESSAGE_CREATE、MESSAGE_CREATE、DIRECT_MESSAGE_CREATE。
func (b *Bot) OnChannelMessage(handler func(ctx *EventContext, msg *ChannelMessageEvent)) {
	b.dispatcher.OnChannelMessage(handler)
}

// OnMessageAudit 注册消息审核事件处理函数。
func (b *Bot) OnMessageAudit(handler func(ctx *EventContext, event *MessageAuditEvent)) {
	b.dispatcher.OnMessageAudit(handler)
}

// OnFriendAdd 注册添加好友事件处理函数。
func (b *Bot) OnFriendAdd(handler func(ctx *EventContext, event *FriendEvent)) {
	b.dispatcher.On(EventFriendAdd, func(ctx *EventContext) {
		if evt, ok := ctx.RawData.(*FriendEvent); ok {
			handler(ctx, evt)
		}
	})
}

// OnGroupAddRobot 注册机器人被添加到群聊事件处理函数。
func (b *Bot) OnGroupAddRobot(handler func(ctx *EventContext, event *GroupRobotEvent)) {
	b.dispatcher.On(EventGroupAddRobot, func(ctx *EventContext) {
		if evt, ok := ctx.RawData.(*GroupRobotEvent); ok {
			handler(ctx, evt)
		}
	})
}

// OnGroupDelRobot 注册机器人被移出群聊事件处理函数。
func (b *Bot) OnGroupDelRobot(handler func(ctx *EventContext, event *GroupRobotEvent)) {
	b.dispatcher.On(EventGroupDelRobot, func(ctx *EventContext) {
		if evt, ok := ctx.RawData.(*GroupRobotEvent); ok {
			handler(ctx, evt)
		}
	})
}

// Run 启动机器人，连接 WebSocket 开始接收事件。
// 阻塞直到 ctx 被取消。
func (b *Bot) Run(ctx context.Context) error {
	log.Printf("[Bot] 启动 QQ 机器人, AppID=%s, intents=%d", b.config.AppID, b.config.Intents)

	// 预先获取一次 token 验证配置
	_, err := b.tokenMgr.getToken(ctx)
	if err != nil {
		return fmt.Errorf("初始鉴权失败，请检查 AppID 和 ClientSecret: %w", err)
	}
	log.Printf("[Bot] 鉴权成功")

	// 启动 WebSocket 事件订阅
	return b.wsClient.Run(ctx)
}

// Stop 停止机器人。
func (b *Bot) Stop() {
	b.wsClient.Stop()
	log.Printf("[Bot] 已停止")
}

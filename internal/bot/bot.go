// Package bot 负责 QQ 消息接收和路由分发。
// 它是整个框架的「消息入口层」，将消息路由到:
//   - Handler 层: 指令匹配 → 确定性功能 (骰子/角色卡/日志等)
//   - Agent 层: 对话消息 → AI Agent (KP/DM 等)
//   - 联动模式: TRPG 模式下两者协作 (AI主持 + 自动日志)
package bot

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/gamelog"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/pkg/qqbot"
)

// Config 是 Bot 的配置。
type Config struct {
	AppID        string
	ClientSecret string
}

// msgDedup 用于消息去重，避免 GROUP_AT_MESSAGE_CREATE 和 GROUP_MESSAGE_CREATE 重复处理同一条消息。
type msgDedup struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newMsgDedup() *msgDedup {
	return &msgDedup{seen: make(map[string]time.Time)}
}

// isDuplicate 检查消息 ID 是否在近期已处理过，同时清理过期记录。
func (d *msgDedup) isDuplicate(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	// 清理超过 60 秒的记录
	for k, t := range d.seen {
		if now.Sub(t) > 60*time.Second {
			delete(d.seen, k)
		}
	}
	if _, ok := d.seen[id]; ok {
		return true
	}
	d.seen[id] = now
	return false
}

// Bot 是 QQ 机器人实例，负责消息路由和组件编排。
type Bot struct {
	config     *Config
	qqBot      *qqbot.Bot
	plugins    *core.PluginManager
	sessions   *core.SessionManager
	gameLogger *gamelog.GameLogger
	dedup      *msgDedup
	replySeqMu sync.Mutex
	replySeq   map[string]int // msgID → 已回复次数（用于生成递增 msg_seq）
	ctx        context.Context
	cancel     context.CancelFunc
}

// mentionRegex 匹配 QQ 消息中的 @ 提及，格式 <@openid> 或 <@!openid>。
var mentionRegex = regexp.MustCompile(`<@!?[0-9A-Za-z]+>`)

// stripMention 剥除消息中的 @ 提及标记并修剪空白。
// 群全量消息中 @机器人 的内容为 "<@xxx> .help"，需清理后才能匹配指令。
func stripMention(s string) string {
	return strings.TrimSpace(mentionRegex.ReplaceAllString(s, ""))
}

// convertAttachments 将 qqbot.Attachment 转为 core.Attachment。
func convertAttachments(qqAtts []qqbot.Attachment) []core.Attachment {
	if len(qqAtts) == 0 {
		return nil
	}
	atts := make([]core.Attachment, len(qqAtts))
	for i, a := range qqAtts {
		atts[i] = core.Attachment{
			ContentType: a.ContentType,
			Filename:    a.Filename,
			URL:         a.URL,
			Size:        a.Size,
			Height:      a.Height,
			Width:       a.Width,
		}
	}
	return atts
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
		dedup:      newMsgDedup(),
		replySeq:   make(map[string]int),
	}

	b.registerQQHandlers()
	return b, nil
}

// registerQQHandlers 注册 QQ 消息事件处理函数，将消息转为统一 MessageContext。
func (b *Bot) registerQQHandlers() {
	// 群聊@机器人消息
	b.qqBot.OnGroupAtMessage(func(ctx *qqbot.EventContext, msg *qqbot.GroupMessageEvent) {
		if b.dedup.isDuplicate(msg.ID) {
			return
		}
		content := strings.TrimSpace(msg.Content)
		log.Printf("[Bot] 群聊@消息 group=%s user=%s content=%q", msg.GroupOpenid, msg.Author.MemberOpenid, content)

		mc := &core.MessageContext{
			Ctx:         context.Background(),
			Source:      core.SourceGroup,
			SessionID:   "group:" + msg.GroupOpenid,
			UserID:      msg.Author.MemberOpenid,
			OpenID:      msg.GroupOpenid,
			MsgID:       msg.ID,
			Content:     content,
			IsGroup:     true,
			Attachments: convertAttachments(msg.Attachments),
			Extra:       make(map[string]interface{}),
		}
		b.route(mc)
	})

	// 群聊全量消息（群主开启后可收到群内所有消息，不限于@机器人）
	b.qqBot.OnGroupMessage(func(ctx *qqbot.EventContext, msg *qqbot.GroupMessageEvent) {
		// 跳过机器人自身消息
		if msg.Author.Bot {
			return
		}
		if b.dedup.isDuplicate(msg.ID) {
			return
		}
		// 剥除 @机器人 等提及标记，使指令能正确匹配
		content := stripMention(msg.Content)
		log.Printf("[Bot] 群聊全量消息 group=%s user=%s content=%q", msg.GroupOpenid, msg.Author.MemberOpenid, content)

		mc := &core.MessageContext{
			Ctx:           context.Background(),
			Source:        core.SourceGroup,
			SessionID:     "group:" + msg.GroupOpenid,
			UserID:        msg.Author.MemberOpenid,
			OpenID:        msg.GroupOpenid,
			MsgID:         msg.ID,
			Content:       content,
			IsGroup:       true,
			Attachments:   convertAttachments(msg.Attachments),
			MentionUserID: msg.Author.MemberOpenid, // 群全量消息回复时需 @发送者
			Extra:         make(map[string]interface{}),
		}
		b.route(mc)
	})

	// 单聊消息
	b.qqBot.OnC2CMessage(func(ctx *qqbot.EventContext, msg *qqbot.C2CMessageEvent) {
		content := strings.TrimSpace(msg.Content)
		log.Printf("[Bot] 单聊消息 user=%s content=%q", msg.Author.UserOpenid, content)

		mc := &core.MessageContext{
			Ctx:         context.Background(),
			Source:      core.SourceC2C,
			SessionID:   "c2c:" + msg.Author.UserOpenid,
			UserID:      msg.Author.UserOpenid,
			OpenID:      msg.Author.UserOpenid,
			MsgID:       msg.ID,
			Content:     content,
			IsGroup:     false,
			Attachments: convertAttachments(msg.Attachments),
			Extra:       make(map[string]interface{}),
		}
		b.route(mc)
	})

	// 频道消息（@机器人 / 私域全量 / 频道私信）
	b.qqBot.OnChannelMessage(func(ctx *qqbot.EventContext, msg *qqbot.ChannelMessageEvent) {
		if msg.Author.Bot {
			return
		}
		content := strings.TrimSpace(msg.Content)
		log.Printf("[Bot] 频道消息 channel=%s guild=%s user=%s content=%q", msg.ChannelID, msg.GuildID, msg.Author.ID, content)

		mc := &core.MessageContext{
			Ctx:       context.Background(),
			Source:    core.SourceChannel,
			SessionID: "channel:" + msg.ChannelID,
			UserID:    msg.Author.ID,
			OpenID:    msg.ChannelID,
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
	reply := b.makeReplyFunc(mc)

	// TRPG/FreeChat 模式下，记录玩家消息到日志
	if b.gameLogger.IsRecording(mc.SessionID) {
		b.gameLogger.RecordUserMessage(mc.SessionID, mc.UserID, mc.Content)
	}

	// 文件附件处理：缓存到会话，支持文件和指令分条发送的场景
	if mc.HasFileAttachment() {
		fileAtt := mc.GetFileAttachment()
		session := b.sessions.GetSession(mc.SessionID)
		session.Set("last_file_attachment", fileAtt)
		session.Set("last_file_time", time.Now().Unix())

		log.Printf("[Bot] 收到文件附件: %s (%s) session=%s",
			fileAtt.Filename, fileAtt.ContentType, mc.SessionID)

		// 场景2: 用户先发 .script upload，再发文件 → 等待状态触发
		if waiting, ok := session.Get("waiting_script_upload"); ok && waiting.(bool) {
			session.Set("waiting_script_upload", false)
			mc.Content = ".script upload"
			if handler := b.plugins.MatchHandler(mc); handler != nil {
				if err := handler.Execute(mc, reply); err != nil {
					log.Printf("[Bot] Handler %s 执行失败: %v", handler.Name(), err)
				}
			}
			return
		}

		// 文件已缓存，但没有指令文本 → 提示用户
		if mc.Content == "" {
			reply(mc.Ctx, mc.OpenID, mc.MsgID,
				fmt.Sprintf("收到文件: %s\n如需上传剧本，请发送 .script upload", fileAtt.Filename),
				mc.IsGroup)
			return
		}
		// 文件 + 指令同时发送 → 走正常指令路由（ScriptHandler 会处理附件）
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
			return b.sendReply(mc.Source, openid, msgID, text, mc.MentionUserID)
		})

	default:
		log.Printf("[Bot] 未知会话模式: %s", session.Mode)
	}
}

// makeReplyFunc 创建标准的回复函数，根据消息来源选择回复方式。
func (b *Bot) makeReplyFunc(mc *core.MessageContext) core.ReplyFunc {
	source := mc.Source
	mentionID := mc.MentionUserID
	return func(ctx context.Context, openid, msgID, text string, isGroup bool) error {
		return b.sendReply(source, openid, msgID, text, mentionID)
	}
}

// sendReply 发送回复消息到 QQ，根据消息来源选择对应的 API。
// mentionUserID 非空时（群全量消息），通过 message_reference 引用原消息实现 @回复效果。
// 同一条消息多次回复时自动递增 msg_seq 避免被 QQ 去重；
// 若仍被去重，则降级为直接发送（不带 msg_id 的主动消息）。
func (b *Bot) sendReply(source core.MessageSource, openid, msgID, text, mentionUserID string) error {
	api := b.qqBot.API()
	seq := b.nextMsgSeq(msgID)

	var err error
	switch source {
	case core.SourceChannel:
		_, err = api.SendChannelMessage(context.Background(), openid, &qqbot.MessageReq{
			Content: text,
			MsgType: qqbot.MsgTypeText,
			MsgID:   msgID,
			MsgSeq:  seq,
		})
	case core.SourceGroup:
		if mentionUserID != "" {
			// 群全量消息：用 message_reference 引用原消息，实现"回复"效果
			_, err = api.SendGroupMessage(context.Background(), openid, &qqbot.MessageReq{
				Content: text,
				MsgType: qqbot.MsgTypeText,
				MsgID:   msgID,
				MsgSeq:  seq,
				MessageReference: &qqbot.MessageRef{
					MessageID: msgID,
				},
			})
		} else {
			_, err = api.SendGroupMessage(context.Background(), openid, &qqbot.MessageReq{
				Content: text,
				MsgType: qqbot.MsgTypeText,
				MsgID:   msgID,
				MsgSeq:  seq,
			})
		}
	default:
		_, err = api.SendC2CMessage(context.Background(), openid, &qqbot.MessageReq{
			Content: text,
			MsgType: qqbot.MsgTypeText,
			MsgID:   msgID,
			MsgSeq:  seq,
		})
	}

	if err != nil {
		// 检查是否为去重错误，降级为直接发送（不带 msg_id）
		if isDedupError(err) {
			log.Printf("[Bot] 消息被去重 (msgID=%s seq=%d)，降级为直接发送", msgID, seq)
			return b.sendDirect(source, openid, text)
		}
		log.Printf("[Bot] 发送回复失败: %v", err)
	}
	return err
}

// sendDirect 不带 msg_id 直接发送消息（主动消息），用于去重降级。
func (b *Bot) sendDirect(source core.MessageSource, openid, text string) error {
	api := b.qqBot.API()
	var err error
	switch source {
	case core.SourceChannel:
		_, err = api.SendChannelMessage(context.Background(), openid, &qqbot.MessageReq{
			Content: text,
			MsgType: qqbot.MsgTypeText,
		})
	case core.SourceGroup:
		_, err = api.SendGroupMessage(context.Background(), openid, &qqbot.MessageReq{
			Content: text,
			MsgType: qqbot.MsgTypeText,
		})
	default:
		_, err = api.SendC2CMessage(context.Background(), openid, &qqbot.MessageReq{
			Content: text,
			MsgType: qqbot.MsgTypeText,
		})
	}
	if err != nil {
		log.Printf("[Bot] 直接发送失败: %v", err)
	}
	return err
}

// nextMsgSeq 返回指定 msgID 的下一个回复序号（递增，避免 QQ 去重）。
func (b *Bot) nextMsgSeq(msgID string) int {
	b.replySeqMu.Lock()
	defer b.replySeqMu.Unlock()
	b.replySeq[msgID]++
	seq := b.replySeq[msgID]
	// 清理过期记录（超过100条时清理最早的）
	if len(b.replySeq) > 100 {
		for k := range b.replySeq {
			delete(b.replySeq, k)
			if len(b.replySeq) <= 50 {
				break
			}
		}
	}
	return seq
}

// isDedupError 检查是否为 QQ API 消息去重错误。
func isDedupError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "去重")
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

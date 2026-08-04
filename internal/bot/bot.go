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
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/web"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/pkg/qqbot"
)

// Config 是 Bot 的配置。
type Config struct {
	AppID        string                        // 静态凭证（CredFn 为空时使用）
	ClientSecret string                        // 静态凭证（CredFn 为空时使用）
	CredFn       func() (appID, secret string) // 凭证回调：Start/Restart 时读取，支持改凭证后重启生效
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

// Bot 是 QQ 机器人实例（QQChannel），负责 QQ 消息收发。
// 路由逻辑已抽取到 core.Router，Bot 只负责构建 MessageContext 与回复。
// 实现 web.BotController：Start/Stop/Restart 幂等，Restart 重读凭证。
type Bot struct {
	config     *Config
	router     *core.Router
	dedup      *msgDedup
	replySeqMu sync.Mutex
	replySeq   map[string]int // msgID → 已回复次数（用于生成递增 msg_seq）

	// 生命周期（mu 保护；管理后台启停与消息收发并发）
	mu        sync.Mutex
	qqBot     *qqbot.Bot
	ctx       context.Context
	cancel    context.CancelFunc
	running   bool
	startedAt time.Time
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
// router 为渠道共用的统一路由器（含插件管理、会话与日志记录器）。
// qqbot.Bot 延迟到 Start 时按当前凭证构建（改凭证 → 重启生效）。
func NewBot(cfg *Config, router *core.Router) (*Bot, error) {
	b := &Bot{
		config:   cfg,
		router:   router,
		dedup:    newMsgDedup(),
		replySeq: make(map[string]int),
	}
	return b, nil
}

// creds 读取当前凭证（优先 CredFn，回落静态配置）。
func (b *Bot) creds() (appID, secret string) {
	if b.config.CredFn != nil {
		return b.config.CredFn()
	}
	return b.config.AppID, b.config.ClientSecret
}

// current 返回当前 qqbot.Bot（可能为 nil，未启动时）。
func (b *Bot) current() *qqbot.Bot {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.qqBot
}

// registerQQHandlers 注册 QQ 消息事件处理函数，将消息转为统一 MessageContext。
func (b *Bot) registerQQHandlers(qb *qqbot.Bot) {
	// 群聊@机器人消息
	qb.OnGroupAtMessage(func(ctx *qqbot.EventContext, msg *qqbot.GroupMessageEvent) {
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
	qb.OnGroupMessage(func(ctx *qqbot.EventContext, msg *qqbot.GroupMessageEvent) {
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
	qb.OnC2CMessage(func(ctx *qqbot.EventContext, msg *qqbot.C2CMessageEvent) {
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
	qb.OnChannelMessage(func(ctx *qqbot.EventContext, msg *qqbot.ChannelMessageEvent) {
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
	qb.OnGroupAddRobot(func(ctx *qqbot.EventContext, event *qqbot.GroupRobotEvent) {
		log.Printf("[Bot] 被添加到群聊 group=%s", event.GroupOpenid)
	})
	qb.OnGroupDelRobot(func(ctx *qqbot.EventContext, event *qqbot.GroupRobotEvent) {
		log.Printf("[Bot] 被移出群聊 group=%s", event.GroupOpenid)
	})
}

// route 将消息交给统一路由器，回复函数由本渠道提供。
func (b *Bot) route(mc *core.MessageContext) {
	b.router.Route(mc, b.makeReplyFunc(mc))
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
	qb := b.current()
	if qb == nil {
		return fmt.Errorf("机器人未启动")
	}
	api := qb.API()
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
	qb := b.current()
	if qb == nil {
		return fmt.Errorf("机器人未启动")
	}
	api := qb.API()
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

// ID 实现 core.Channel。
func (b *Bot) ID() string { return "qq" }

// Start 启动 Bot（幂等：已运行则直接返回 nil）。
// 每次启动按 CredFn 读取最新凭证构建 qqbot.Bot。
func (b *Bot) Start() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running {
		return nil
	}

	appID, secret := b.creds()
	if appID == "" || secret == "" {
		return fmt.Errorf("QQ 凭证未配置（qq_appid / qq_secret）")
	}

	qb := qqbot.NewBot(&qqbot.Config{AppID: appID, ClientSecret: secret})
	b.registerQQHandlers(qb)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := qb.Run(ctx); err != nil {
			log.Printf("[Bot] 运行出错: %v", err)
		}
	}()

	b.qqBot = qb
	b.ctx = ctx
	b.cancel = cancel
	b.running = true
	b.startedAt = time.Now()
	return nil
}

// Stop 停止 Bot（幂等：未运行则直接返回 nil）。
func (b *Bot) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		return nil
	}
	if b.cancel != nil {
		b.cancel()
	}
	if b.qqBot != nil {
		b.qqBot.Stop()
	}
	b.running = false
	return nil
}

// Restart 重启 Bot：Stop + Start，Start 时重读凭证使新配置生效。
func (b *Bot) Restart() error {
	if err := b.Stop(); err != nil {
		return err
	}
	return b.Start()
}

// Status 返回 Bot 运行状态（实现 web.BotController；Secret 永不外露）。
func (b *Bot) Status() web.BotStatus {
	b.mu.Lock()
	running := b.running
	startedAt := b.startedAt
	qb := b.qqBot
	b.mu.Unlock()

	appID, _ := b.creds()
	st := web.BotStatus{Running: running, AppID: appID}
	if !running {
		return st
	}
	st.StartedAt = startedAt.Format("2006-01-02 15:04:05")
	st.Uptime = time.Since(startedAt).Round(time.Second).String()
	if qb != nil {
		ws := qb.Stats()
		st.Connected = ws.Connected
		st.LastConnectedAt = ws.LastConnectedAt
		st.ReconnectCount = ws.ReconnectCount
		st.RxCount = ws.RxCount
		st.TxCount = ws.TxCount
	}
	return st
}

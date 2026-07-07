package qqbot

import "encoding/json"

// 事件类型常量。
const (
	// --- 群聊和单聊事件 (Intent 1<<25) ---
	EventC2CMessageCreate     = "C2C_MESSAGE_CREATE"       // 单聊消息
	EventGroupAtMessageCreate = "GROUP_AT_MESSAGE_CREATE"  // 群聊@机器人
	EventGroupMessageCreate   = "GROUP_MESSAGE_CREATE"     // 群聊全量消息
	EventFriendAdd            = "FRIEND_ADD"               // 添加好友
	EventFriendDel            = "FRIEND_DEL"               // 删除好友
	EventC2CMsgReject         = "C2C_MSG_REJECT"           // 单聊拒收
	EventC2CMsgReceive        = "C2C_MSG_RECEIVE"          // 单聊接收
	EventGroupAddRobot        = "GROUP_ADD_ROBOT"          // 群聊添加机器人
	EventGroupDelRobot        = "GROUP_DEL_ROBOT"          // 群聊移除机器人
	EventGroupMsgReject       = "GROUP_MSG_REJECT"         // 群聊拒收
	EventGroupMsgReceive      = "GROUP_MSG_RECEIVE"        // 群聊接收

	// --- 交互事件 (Intent 1<<26) ---
	EventInteractionCreate = "INTERACTION_CREATE" // 交互回调

	// --- 频道事件 (Intent 1<<0) ---
	EventGuildCreate  = "GUILD_CREATE"   // 频道创建
	EventGuildUpdate  = "GUILD_UPDATE"   // 频道更新
	EventGuildDelete  = "GUILD_DELETE"   // 频道删除
	EventChannelCreate = "CHANNEL_CREATE" // 子频道创建
	EventChannelUpdate = "CHANNEL_UPDATE" // 子频道更新
	EventChannelDelete = "CHANNEL_DELETE" // 子频道删除

	// --- 频道成员事件 (Intent 1<<1) ---
	EventGuildMemberAdd    = "GUILD_MEMBER_ADD"    // 成员加入
	EventGuildMemberUpdate = "GUILD_MEMBER_UPDATE" // 成员更新
	EventGuildMemberRemove = "GUILD_MEMBER_REMOVE" // 成员退出

	// --- 公域频道消息事件 (Intent 1<<30) ---
	EventAtMessageCreate     = "AT_MESSAGE_CREATE"      // 频道@机器人
	EventPublicMessageDelete = "PUBLIC_MESSAGE_DELETE"  // 公域消息撤回

	// --- 频道私信事件 (Intent 1<<12) ---
	EventDirectMessageCreate = "DIRECT_MESSAGE_CREATE" // 频道私信
	EventDirectMessageDelete = "DIRECT_MESSAGE_DELETE" // 频道私信撤回

	// --- 系统事件 ---
	EventReady    = "READY"    // 鉴权成功
	EventResumed  = "RESUMED"  // 恢复会话成功
)

// Attachment 是消息中的富媒体附件。
type Attachment struct {
	ContentType  string `json:"content_type,omitempty"`  // 如 "image/jpeg", "video/mp4"
	Filename     string `json:"filename,omitempty"`      // 文件名
	Height       int    `json:"height,omitempty"`        // 图片高度
	Width        int    `json:"width,omitempty"`         // 图片宽度
	Size         int    `json:"size,omitempty"`          // 文件大小
	URL          string `json:"url,omitempty"`           // 文件链接
	VoiceWavURL  string `json:"voice_wav_url,omitempty"` // 语音 wav 链接
	AsrReferText string `json:"asr_refer_text,omitempty"` // 语音识别结果
}

// C2CAuthor 是单聊消息的发送者。
type C2CAuthor struct {
	UserOpenid string `json:"user_openid"`
}

// GroupAuthor 是群聊消息的发送者。
type GroupAuthor struct {
	MemberOpenid string `json:"member_openid"` // 用户在群内的 openid
	MemberRole   string `json:"member_role"`   // 身份: owner / admin / member
	Bot          bool   `json:"bot"`           // 是否是机器人
}

// C2CMessageEvent 是单聊消息事件 (C2C_MESSAGE_CREATE)。
type C2CMessageEvent struct {
	ID          string       `json:"id"`          // 消息 ID，用于被动回复
	Author      C2CAuthor    `json:"author"`      // 发送者
	Content     string       `json:"content"`     // 消息内容
	Timestamp   string       `json:"timestamp"`   // RFC3339 时间
	Attachments []Attachment `json:"attachments"` // 附件
}

// GroupMessageEvent 是群聊@机器人消息事件 (GROUP_AT_MESSAGE_CREATE)。
// 也用于 GROUP_MESSAGE_CREATE 群聊全量消息事件。
type GroupMessageEvent struct {
	ID          string       `json:"id"`          // 消息 ID
	Author      GroupAuthor  `json:"author"`      // 发送者
	Content     string       `json:"content"`     // 消息内容
	Timestamp   string       `json:"timestamp"`   // RFC3339 时间
	GroupOpenid string       `json:"group_openid"` // 群 openid
	Attachments []Attachment `json:"attachments"` // 附件
}

// ChannelAuthor 是频道消息的发送者。
type ChannelAuthor struct {
	ID       string `json:"id"`       // 用户 ID
	Username string `json:"username"` // 用户名
	Avatar   string `json:"avatar"`   // 头像 URL
	Bot      bool   `json:"bot"`      // 是否是机器人
}

// ChannelMember 是频道成员信息。
type ChannelMember struct {
	JoinedAt string   `json:"joined_at"` // 加入时间
	Roles    []string `json:"roles"`     // 身份组 ID 列表
}

// ChannelMessageEvent 是频道消息事件 (AT_MESSAGE_CREATE / MESSAGE_CREATE / DIRECT_MESSAGE_CREATE)。
type ChannelMessageEvent struct {
	ID        string         `json:"id"`         // 消息 ID
	Author    ChannelAuthor  `json:"author"`     // 发送者
	Content   string         `json:"content"`    // 消息内容
	Timestamp string         `json:"timestamp"`  // RFC3339 时间
	ChannelID string         `json:"channel_id"` // 子频道 ID
	GuildID   string         `json:"guild_id"`   // 频道 ID
	Member    ChannelMember  `json:"member"`     // 成员信息
	Seq       int            `json:"seq"`        // 消息序号
}

// FriendEvent 是好友变动事件 (FRIEND_ADD / FRIEND_DEL)。
type FriendEvent struct {
	Openid    string `json:"openid"`
	Timestamp string `json:"timestamp"`
}

// GroupRobotEvent 是群机器人变动事件 (GROUP_ADD_ROBOT / GROUP_DEL_ROBOT)。
type GroupRobotEvent struct {
	GroupOpenid string `json:"group_openid"`
	OpMemberOpenid string `json:"op_member_openid"` // 操作者 openid
	Timestamp    string `json:"timestamp"`
}

// InteractionEvent 是交互回调事件 (INTERACTION_CREATE)。
type InteractionEvent struct {
	ID         string          `json:"id"`          // 事件 ID
	ChatType   int             `json:"chat_type"`   // 1=频道 2=群聊 3=单聊
	GroupOpenid string         `json:"group_openid,omitempty"` // 群 openid
	UserOpenid string          `json:"user_openid,omitempty"`  // 用户 openid
	Data       json.RawMessage `json:"data,omitempty"`         // 交互数据
	Timestamp  string          `json:"timestamp"`
	Type       int             `json:"type"` // 交互类型
}

// MessageRejectEvent 是消息拒收/接收事件 (C2C_MSG_REJECT / C2C_MSG_RECEIVE / GROUP_MSG_REJECT / GROUP_MSG_RECEIVE)。
type MessageRejectEvent struct {
	Openid    string `json:"openid"`
	Timestamp string `json:"timestamp"`
}

// EventDispatcher 是事件分发器，管理各类型事件的回调。
type EventDispatcher struct {
	handlers map[string][]EventHandler
}

// EventHandler 是事件处理函数类型。
// data 是事件 payload 的 d 字段（已反序列化为对应类型）。
type EventHandler func(ctx *EventContext)

// EventContext 是事件处理上下文，包含事件元数据和解析后的数据。
type EventContext struct {
	EventType string      // 事件类型 (如 C2C_MESSAGE_CREATE)
	Seq       int64       // 消息序列号
	EventID   string      // 事件 ID
	RawData   interface{} // 原始解析数据
	API       *OpenAPI    // API 客户端，可在处理函数中发送回复
}

// NewEventDispatcher 创建事件分发器。
func NewEventDispatcher() *EventDispatcher {
	return &EventDispatcher{
		handlers: make(map[string][]EventHandler),
	}
}

// On 注册事件处理函数。
func (d *EventDispatcher) On(eventType string, handler EventHandler) {
	d.handlers[eventType] = append(d.handlers[eventType], handler)
}

// OnC2CMessage 注册单聊消息处理函数。
func (d *EventDispatcher) OnC2CMessage(handler func(ctx *EventContext, msg *C2CMessageEvent)) {
	d.On(EventC2CMessageCreate, func(ctx *EventContext) {
		if msg, ok := ctx.RawData.(*C2CMessageEvent); ok {
			handler(ctx, msg)
		}
	})
}

// OnGroupAtMessage 注册群聊@机器人消息处理函数。
func (d *EventDispatcher) OnGroupAtMessage(handler func(ctx *EventContext, msg *GroupMessageEvent)) {
	d.On(EventGroupAtMessageCreate, func(ctx *EventContext) {
		if msg, ok := ctx.RawData.(*GroupMessageEvent); ok {
			handler(ctx, msg)
		}
	})
}

// OnGroupMessage 注册群聊全量消息处理函数。
func (d *EventDispatcher) OnGroupMessage(handler func(ctx *EventContext, msg *GroupMessageEvent)) {
	d.On(EventGroupMessageCreate, func(ctx *EventContext) {
		if msg, ok := ctx.RawData.(*GroupMessageEvent); ok {
			handler(ctx, msg)
		}
	})
}

// OnInteraction 注册交互事件处理函数。
func (d *EventDispatcher) OnInteraction(handler func(ctx *EventContext, event *InteractionEvent)) {
	d.On(EventInteractionCreate, func(ctx *EventContext) {
		if evt, ok := ctx.RawData.(*InteractionEvent); ok {
			handler(ctx, evt)
		}
	})
}

// Dispatch 分发事件给已注册的处理函数。
func (d *EventDispatcher) Dispatch(ctx *EventContext) {
	handlers, ok := d.handlers[ctx.EventType]
	if !ok {
		return
	}
	for _, h := range handlers {
		h(ctx)
	}
}

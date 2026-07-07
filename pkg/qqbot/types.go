// Package qqbot 实现 QQ 机器人官方 API v2 交互协议。
// 文档: https://bot.q.qq.com/wiki/develop/api-v2/
//
// 包含 AccessToken 鉴权、OpenAPI HTTP 调用、WebSocket 事件订阅三大核心能力。
package qqbot

// Op 是 WebSocket 通信的 Opcode 定义。
// 参考: https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/interface-framework/event-emit.html
type Op int

const (
	OpDispatch           Op = 0  // 服务端消息推送
	OpHeartbeat          Op = 1  // 客户端/服务端发送心跳
	OpIdentify           Op = 2  // 客户端发送鉴权
	OpResume             Op = 6  // 客户端恢复连接
	OpReconnect          Op = 7  // 服务端通知重新连接
	OpInvalidSession     Op = 9  // 鉴权或恢复参数错误
	OpHello              Op = 10 // 建立连接后第一条消息
	OpHeartbeatACK       Op = 11 // 心跳成功响应
	OpHTTPCallbackACK    Op = 12 // HTTP回调回包
	OpCallbackVerify     Op = 13 // 回调地址验证
)

// Intent 是事件订阅标记位。
// 需要接收某类事件，将对应位置为1。
type Intent int

const (
	IntentGUILDS               Intent = 1 << 0  // 频道事件
	IntentGUILDMembers         Intent = 1 << 1  // 频道成员事件
	IntentGUILDMessages        Intent = 1 << 9  // 频道消息（仅私域）
	IntentGUILDMessageReactions Intent = 1 << 10 // 频道消息表情表态
	IntentDirectMessage        Intent = 1 << 12 // 频道私信
	IntentGroupAndC2CEvent     Intent = 1 << 25 // 群聊和单聊事件
	IntentInteraction          Intent = 1 << 26 // 交互事件
	IntentMessageAudit         Intent = 1 << 27 // 消息审核事件
	IntentForumsEvent          Intent = 1 << 28 // 论坛事件（仅私域）
	IntentAudioAction          Intent = 1 << 29 // 音频事件
	IntentPublicGUILDMessages  Intent = 1 << 30 // 公域频道消息
)

// DefaultIntents 返回 TRPG Bot 常用的事件订阅组合。
// 包含群聊/单聊事件和交互事件。
func DefaultIntents() Intent {
	return IntentGroupAndC2CEvent | IntentInteraction | IntentPublicGUILDMessages
}

// Payload 是 WebSocket 通信的通用数据结构。
type Payload struct {
	Op  Op          `json:"op"`           // 操作码
	S   int64       `json:"s,omitempty"`  // 消息序列号，仅 Dispatch 有值
	T   string      `json:"t,omitempty"`  // 事件类型，仅 op=0 Dispatch 有值
	ID  string      `json:"id,omitempty"` // 事件 ID
	D   interface{} `json:"d,omitempty"`  // 事件内容
}

// HelloData 是 OpCode 10 Hello 的数据结构。
type HelloData struct {
	HeartbeatInterval int `json:"heartbeat_interval"` // 心跳周期，单位毫秒
}

// IdentifyData 是 OpCode 2 Identify 的数据结构。
type IdentifyData struct {
	Token      string                 `json:"token"`      // 格式 "QQBot {AccessToken}"
	Intents    Intent                 `json:"intents"`    // 事件订阅标记位
	Shard      []int                  `json:"shard"`      // 分片 [shard_id, num_shards]
	Properties map[string]interface{} `json:"properties"` // 无实际作用，可留空
}

// ResumeData 是 OpCode 6 Resume 的数据结构。
type ResumeData struct {
	Token     string `json:"token"`      // 格式 "QQBot {AccessToken}"
	SessionID string `json:"session_id"` // 上次连接的 session_id
	Seq       int64  `json:"seq"`        // 最后处理事件的 s 值
}

// ReadyEvent 是鉴权成功后收到的 READY 事件数据。
type ReadyEvent struct {
	Version   int    `json:"version"`   // 协议版本
	SessionID string `json:"session_id"` // 会话 ID，用于 Resume
	User      struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	} `json:"user"`
	Shard []int `json:"shard"`
}

// APIBaseURL 是 OpenAPI 的统一基础地址。
const APIBaseURL = "https://api.sgroup.qq.com"

// AccessTokenURL 是获取 AccessToken 的接口地址。
const AccessTokenURL = "https://bots.qq.com/app/getAppAccessToken"

// TokenType 是鉴权 Token 的类型前缀。
const TokenType = "QQBot"

// MsgType 消息类型。
type MsgType int

const (
	MsgTypeText     MsgType = 0 // 文本消息
	MsgTypeMarkdown MsgType = 2 // Markdown 消息
	MsgTypeArk      MsgType = 3 // Ark 消息
	MsgTypeEmbed    MsgType = 4 // Embed 消息
	MsgTypeMedia    MsgType = 7 // 富媒体消息
)

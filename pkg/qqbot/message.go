package qqbot

import (
	"context"
	"fmt"
)

// MessageReq 是发送消息的请求体。
// 文档: https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/send-receive/send.html
type MessageReq struct {
	Content          string           `json:"content,omitempty"`           // 文本消息内容
	MsgType          MsgType          `json:"msg_type"`                    // 消息类型
	Markdown         *Markdown        `json:"markdown,omitempty"`          // Markdown 对象
	Keyboard         interface{}      `json:"keyboard,omitempty"`          // Keyboard 对象
	Ark              interface{}      `json:"ark,omitempty"`               // Ark 对象
	Media            *Media           `json:"media,omitempty"`             // 富媒体
	MessageReference *MessageRef      `json:"message_reference,omitempty"` // 消息引用
	EventID          string           `json:"event_id,omitempty"`          // 事件ID（被动消息）
	MsgID            string           `json:"msg_id,omitempty"`            // 消息ID（被动回复）
	MsgSeq           int              `json:"msg_seq,omitempty"`           // 回复序号，与 msg_id 联合使用
	IsWakeup         bool             `json:"is_wakeup,omitempty"`         // 互动召回消息
}

// Markdown 是 Markdown 消息对象。
type Markdown struct {
	Content string                 `json:"content,omitempty"` // Markdown 内容
	Params  map[string]interface{} `json:"params,omitempty"`  // 模板参数
}

// Media 是富媒体消息对象。
type Media struct {
	FileInfo string `json:"file_info"` // 文件信息
}

// MessageRef 是消息引用对象。
type MessageRef struct {
	MessageID string `json:"message_id"` // 引用的消息 ID
	IgnoreGetError bool `json:"ignore_get_error,omitempty"`
}

// MessageResp 是发送消息的响应。
type MessageResp struct {
	ID        string `json:"id"`        // 消息唯一 ID
	Timestamp int    `json:"timestamp"` // 发送时间戳
}

// SendC2CMessage 发送单聊消息。
// 接口: POST /v2/users/{openid}/messages
func (a *OpenAPI) SendC2CMessage(ctx context.Context, openid string, req *MessageReq) (*MessageResp, error) {
	if req.MsgSeq == 0 {
		req.MsgSeq = 1
	}
	path := fmt.Sprintf("/v2/users/%s/messages", openid)
	var resp MessageResp
	if err := a.doPOST(ctx, path, req, &resp); err != nil {
		return nil, fmt.Errorf("发送单聊消息失败: %w", err)
	}
	return &resp, nil
}

// SendGroupMessage 发送群聊消息。
// 接口: POST /v2/groups/{group_openid}/messages
func (a *OpenAPI) SendGroupMessage(ctx context.Context, groupOpenid string, req *MessageReq) (*MessageResp, error) {
	if req.MsgSeq == 0 {
		req.MsgSeq = 1
	}
	path := fmt.Sprintf("/v2/groups/%s/messages", groupOpenid)
	var resp MessageResp
	if err := a.doPOST(ctx, path, req, &resp); err != nil {
		return nil, fmt.Errorf("发送群聊消息失败: %w", err)
	}
	return &resp, nil
}

// SendChannelMessage 发送文字子频道消息。
// 接口: POST /channels/{channel_id}/messages
func (a *OpenAPI) SendChannelMessage(ctx context.Context, channelID string, req *MessageReq) (*MessageResp, error) {
	path := fmt.Sprintf("/channels/%s/messages", channelID)
	var resp MessageResp
	if err := a.doPOST(ctx, path, req, &resp); err != nil {
		return nil, fmt.Errorf("发送频道消息失败: %w", err)
	}
	return &resp, nil
}

// SendDMSMessage 发送频道私信消息。
// 接口: POST /dms/{guild_id}/messages
func (a *OpenAPI) SendDMSMessage(ctx context.Context, guildID string, req *MessageReq) (*MessageResp, error) {
	path := fmt.Sprintf("/dms/%s/messages", guildID)
	var resp MessageResp
	if err := a.doPOST(ctx, path, req, &resp); err != nil {
		return nil, fmt.Errorf("发送私信消息失败: %w", err)
	}
	return &resp, nil
}

// ReplyC2CText 快捷发送单聊文本回复。
func (a *OpenAPI) ReplyC2CText(ctx context.Context, openid, msgID, content string) (*MessageResp, error) {
	return a.SendC2CMessage(ctx, openid, &MessageReq{
		Content: content,
		MsgType: MsgTypeText,
		MsgID:   msgID,
	})
}

// ReplyGroupText 快捷发送群聊文本回复。
func (a *OpenAPI) ReplyGroupText(ctx context.Context, groupOpenid, msgID, content string) (*MessageResp, error) {
	return a.SendGroupMessage(ctx, groupOpenid, &MessageReq{
		Content: content,
		MsgType: MsgTypeText,
		MsgID:   msgID,
	})
}

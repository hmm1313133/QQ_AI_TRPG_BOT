// Package agent - 回复长度档位（会话级偏好，prompt 指令注入实现）。
//
// 刻意简化：不改 maxTokens/temperature（那是上限不是目标），
// 仅在每轮用户消息尾部注入一段长度要求文本（必需分区，不参与裁剪）。
package agent

import (
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
)

// 回复长度档位。
const (
	ReplyLengthShort    = "short"    // 简短（≤150字）
	ReplyLengthStandard = "standard" // 标准（~300字，默认）
	ReplyLengthDetailed = "detailed" // 详细（~600字）
)

// sessionKeyReplyLength 会话级回复长度偏好的存储键（内存级，重启丢档）。
const sessionKeyReplyLength = "reply_length"

// lengthHints 档位 -> 注入文本（standard 也注入，明确预期）。
var lengthHints = map[string]string{
	ReplyLengthShort:    "【回复长度要求】本次回复务必控制在 150 字以内，只保留最关键的信息与行动结果，省略铺陈。",
	ReplyLengthStandard: "【回复长度要求】本次回复请控制在 300 字左右，兼顾氛围描写与信息推进。",
	ReplyLengthDetailed: "【回复长度要求】本次回复请写到 600 字左右，充分展开场景细节、NPC 反应与氛围描写。",
}

// lengthAliases 中文别名 -> 档位（QQ 指令输入归一化用）。
var lengthAliases = map[string]string{
	"简短": ReplyLengthShort,
	"标准": ReplyLengthStandard,
	"详细": ReplyLengthDetailed,
}

// NormalizeReplyLength 把用户输入（英文档位或中文别名）归一化为档位常量；
// 无法识别时返回空串。
func NormalizeReplyLength(input string) string {
	switch input {
	case ReplyLengthShort, ReplyLengthStandard, ReplyLengthDetailed:
		return input
	}
	return lengthAliases[input]
}

// LengthHint 返回档位对应的注入文本；空/未知 pref 返回空串。
func LengthHint(pref string) string {
	return lengthHints[pref]
}

// SetSessionReplyLength 设置会话级回复长度偏好（pref 须为档位常量）。
func SetSessionReplyLength(session *core.Session, pref string) {
	session.Set(sessionKeyReplyLength, pref)
}

// SessionReplyLength 读取会话级回复长度偏好（未设置返回空串）。
func SessionReplyLength(session *core.Session) string {
	if session == nil {
		return ""
	}
	v, ok := session.Get(sessionKeyReplyLength)
	if !ok {
		return ""
	}
	pref, _ := v.(string)
	return pref
}

// LengthHintFromSession 读取会话级回复长度偏好并生成注入文本（未设置返回空串）。
func LengthHintFromSession(session *core.Session) string {
	return LengthHint(SessionReplyLength(session))
}

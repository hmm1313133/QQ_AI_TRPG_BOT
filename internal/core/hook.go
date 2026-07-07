package core

// Hook 接口定义跨切面关注点，可同时观察 Handler 和 Agent 的行为。
// 典型用途：跑团日志记录、消息审计、统计计数等。
//
// 联动示例:
//   - TRPG 模式下，GameLoggerHook 同时记录玩家发言和 AI KP 回复
//   - DiceResultHook 在 AI KP 要求检定时自动触发骰子并回填结果
type Hook interface {
	// Name 返回 Hook 名称。
	Name() string

	// OnBeforeProcess 在消息处理前调用（Handler 或 Agent 之前）。
	// 可用于记录玩家发言、修改上下文等。
	OnBeforeProcess(ctx *MessageContext)

	// OnReply 在生成回复后、发送前调用。
	// 可用于记录机器人回复、追加处理等。
	OnReply(ctx *MessageContext, reply string)
}

// SimpleHook 基于函数的简单 Hook 实现。
type SimpleHook struct {
	name     string
	before   func(ctx *MessageContext)
	onReply  func(ctx *MessageContext, reply string)
}

// NewSimpleHook 创建简单 Hook。
func NewSimpleHook(name string, before func(ctx *MessageContext), onReply func(ctx *MessageContext, reply string)) *SimpleHook {
	return &SimpleHook{name: name, before: before, onReply: onReply}
}

func (h *SimpleHook) Name() string { return h.name }
func (h *SimpleHook) OnBeforeProcess(ctx *MessageContext) {
	if h.before != nil {
		h.before(ctx)
	}
}
func (h *SimpleHook) OnReply(ctx *MessageContext, reply string) {
	if h.onReply != nil {
		h.onReply(ctx, reply)
	}
}

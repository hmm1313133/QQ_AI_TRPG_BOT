package core

// Handler 接口定义指令/插件处理器。
// 实现此接口即可注册为可被 Bot 路由器调用的功能模块。
//
// 两种功能层的关系:
//   - Handler 层: 基于 Go 代码的确定性功能（骰子、角色卡、日志记录等）
//   - Agent 层: 基于 trpc-agent-go 的 AI 能力（KP 主持、NPC 扮演等）
//   - 两者通过 Session 共享状态，通过 Hook 联动协作
type Handler interface {
	// Name 返回处理器名称（唯一标识）。
	Name() string

	// Match 判断是否匹配当前消息。
	// 返回 true 则由该处理器处理。
	Match(ctx *MessageContext) bool

	// Execute 执行处理逻辑。
	// reply 回复消息后，会触发 Hook.OnReply。
	Execute(ctx *MessageContext, reply ReplyFunc) error
}

// HandlerFunc 是 Handler.Execute 的函数类型，便于快速注册。
type HandlerFunc func(ctx *MessageContext, reply ReplyFunc) error

// SimpleHandler 是一个基于函数的简单 Handler 实现。
type SimpleHandler struct {
	name    string
	match   func(ctx *MessageContext) bool
	execute HandlerFunc
}

// NewSimpleHandler 创建简单 Handler。
func NewSimpleHandler(name string, match func(ctx *MessageContext) bool, execute HandlerFunc) *SimpleHandler {
	return &SimpleHandler{name: name, match: match, execute: execute}
}

func (h *SimpleHandler) Name() string                                  { return h.name }
func (h *SimpleHandler) Match(ctx *MessageContext) bool                { return h.match(ctx) }
func (h *SimpleHandler) Execute(ctx *MessageContext, reply ReplyFunc) error { return h.execute(ctx, reply) }

// AgentHandler 接口供 AI Agent 实现，使其也能被路由器统一调用。
// 与 Handler 不同，Agent 处理的是对话类消息（非指令），
// 且通常需要会话上下文和游戏状态。
type AgentHandler interface {
	// AgentID 返回 Agent 标识（如 "kp", "npc"）。
	AgentID() string

	// Chat 处理用户对话，返回 AI 回复。
	// sessionID 用于隔离不同会话的上下文。
	// 在 TRPG 模式下，Agent 可以通过 session 获取游戏状态。
	Chat(ctx *MessageContext, session *Session) (string, error)
}

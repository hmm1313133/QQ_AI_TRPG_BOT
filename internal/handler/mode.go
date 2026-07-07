package handler

import (
	"fmt"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
)

// ModeHandler 处理会话模式切换 (.mode <模式>)。
// 这是功能层和 Agent 层联动的核心入口：
//   - normal: 仅响应指令，不调用 AI
//   - trpg: AI KP 主持 + 指令 + 自动日志记录
//   - freechat: 所有消息交给 AI Agent
type ModeHandler struct {
	sessionMgr *core.SessionManager
}

// NewModeHandler 创建模式切换处理器。
func NewModeHandler(sm *core.SessionManager) *ModeHandler {
	return &ModeHandler{sessionMgr: sm}
}

func (h *ModeHandler) Name() string { return "mode" }

func (h *ModeHandler) Match(ctx *core.MessageContext) bool {
	return strings.HasPrefix(ctx.Content, ".mode ") || ctx.Content == ".mode"
}

func (h *ModeHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	parts := strings.SplitN(ctx.Content, " ", 2)
	if len(parts) < 2 {
		session := h.sessionMgr.GetSession(ctx.SessionID)
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			fmt.Sprintf("当前模式: %s\n可用模式: normal / trpg / freechat", session.Mode), ctx.IsGroup)
	}

	modeStr := strings.TrimSpace(parts[1])
	var mode core.SessionMode
	switch modeStr {
	case "normal":
		mode = core.ModeNormal
	case "trpg":
		mode = core.ModeTRPG
	case "freechat":
		mode = core.ModeFreeChat
	default:
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			fmt.Sprintf("未知模式: %s\n可用模式: normal / trpg / freechat", modeStr), ctx.IsGroup)
	}

	h.sessionMgr.SetMode(ctx.SessionID, mode)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		fmt.Sprintf("✅ 会话模式已切换为: %s", mode), ctx.IsGroup)
}

var _ core.Handler = (*ModeHandler)(nil)

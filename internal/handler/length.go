package handler

import (
	"fmt"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/agent"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
)

// LengthHandler 处理回复长度档位切换 (.length [档位])。
// 会话级偏好存 core.Session（内存级，重启丢档，一条指令即可重设），
// 经 TurnEngine 每轮注入 Narrator 用户消息（prompt 控制，不动生成参数）。
type LengthHandler struct {
	sessionMgr *core.SessionManager
}

// NewLengthHandler 创建回复长度处理器。
func NewLengthHandler(sm *core.SessionManager) *LengthHandler {
	return &LengthHandler{sessionMgr: sm}
}

func (h *LengthHandler) Name() string { return "length" }

func (h *LengthHandler) Match(ctx *core.MessageContext) bool {
	return strings.HasPrefix(ctx.Content, ".length ") || ctx.Content == ".length"
}

// lengthLabels 档位 -> 展示文案。
var lengthLabels = map[string]string{
	agent.ReplyLengthShort:    "简短（≤150字）",
	agent.ReplyLengthStandard: "标准（~300字）",
	agent.ReplyLengthDetailed: "详细（~600字）",
}

func (h *LengthHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	session := h.sessionMgr.GetSession(ctx.SessionID)

	parts := strings.SplitN(ctx.Content, " ", 2)
	if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
		cur := agent.SessionReplyLength(session)
		curLabel := "标准（~300字，默认）"
		if label, ok := lengthLabels[cur]; ok {
			curLabel = label
		}
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			fmt.Sprintf("当前回复长度: %s\n可用档位: short 简短 / standard 标准 / detailed 详细\n用法: .length <档位>（中文别名亦可）", curLabel), ctx.IsGroup)
	}

	pref := agent.NormalizeReplyLength(strings.TrimSpace(parts[1]))
	if pref == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			fmt.Sprintf("未知档位: %s\n可用档位: short 简短 / standard 标准 / detailed 详细", strings.TrimSpace(parts[1])), ctx.IsGroup)
	}

	agent.SetSessionReplyLength(session, pref)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		fmt.Sprintf("✅ 回复长度已切换为: %s", lengthLabels[pref]), ctx.IsGroup)
}

var _ core.Handler = (*LengthHandler)(nil)

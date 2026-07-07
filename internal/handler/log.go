package handler

import (
	"fmt"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/gamelog"
)

// LogHandler 处理跑团日志指令 (.log <操作>)。
// 与 GameLogger 联动：在 TRPG 模式下 GameLogger Hook 自动记录所有对话，
// 用户可通过此指令手动管理日志。
type LogHandler struct {
	logger *gamelog.GameLogger
}

// NewLogHandler 创建日志处理器。
func NewLogHandler(logger *gamelog.GameLogger) *LogHandler {
	return &LogHandler{logger: logger}
}

func (h *LogHandler) Name() string { return "log" }

func (h *LogHandler) Match(ctx *core.MessageContext) bool {
	return strings.HasPrefix(ctx.Content, ".log ") || ctx.Content == ".log"
}

func (h *LogHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	parts := strings.SplitN(ctx.Content, " ", 3)
	if len(parts) < 2 {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			"用法: .log <start|end|show|export> [备注]", ctx.IsGroup)
	}

	action := strings.TrimSpace(parts[1])
	note := ""
	if len(parts) > 2 {
		note = parts[2]
	}

	switch action {
	case "start":
		err := h.logger.StartSession(ctx.SessionID, note)
		if err != nil {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "启动日志失败: "+err.Error(), ctx.IsGroup)
		}
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "📝 跑团日志记录已开始", ctx.IsGroup)

	case "end":
		summary, err := h.logger.EndSession(ctx.SessionID)
		if err != nil {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "结束日志失败: "+err.Error(), ctx.IsGroup)
		}
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			fmt.Sprintf("📝 跑团日志已结束，共记录 %d 条消息", summary), ctx.IsGroup)

	case "show":
		entries, err := h.logger.GetEntries(ctx.SessionID)
		if err != nil {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "获取日志失败: "+err.Error(), ctx.IsGroup)
		}
		if len(entries) == 0 {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "暂无日志记录", ctx.IsGroup)
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("📋 日志记录 (%d 条):\n", len(entries)))
		for i, e := range entries {
			if i >= 20 {
				sb.WriteString(fmt.Sprintf("... 还有 %d 条\n", len(entries)-20))
				break
			}
			sb.WriteString(fmt.Sprintf("  [%s] %s: %s\n", e.Timestamp, e.Role, e.Content))
		}
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, sb.String(), ctx.IsGroup)

	case "export":
		data, err := h.logger.Export(ctx.SessionID)
		if err != nil {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "导出失败: "+err.Error(), ctx.IsGroup)
		}
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			fmt.Sprintf("📄 日志已导出 (%d 字节):\n%s", len(data), string(data[:min(len(data), 500)])), ctx.IsGroup)

	default:
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			"未知操作: "+action+"\n可用: start | end | show | export", ctx.IsGroup)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ core.Handler = (*LogHandler)(nil)

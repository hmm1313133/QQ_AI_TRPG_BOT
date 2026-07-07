// Package handler 实现基于指令的机器人功能处理器。
// 这些处理器是「代码功能层」的一部分，提供确定性的游戏工具。
// 与 AI Agent 层通过 Session 共享状态实现联动。
package handler

import (
	"fmt"
	"log"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/dice"
)

// DiceHandler 处理骰子指令 (.r <表达式>)。
type DiceHandler struct{}

// NewDiceHandler 创建骰子处理器。
func NewDiceHandler() *DiceHandler { return &DiceHandler{} }

func (h *DiceHandler) Name() string { return "dice" }

func (h *DiceHandler) Match(ctx *core.MessageContext) bool {
	return strings.HasPrefix(ctx.Content, ".r ") || ctx.Content == ".r"
}

func (h *DiceHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	parts := strings.SplitN(ctx.Content, " ", 2)
	expr := "1d100"
	if len(parts) > 1 {
		expr = strings.TrimSpace(parts[1])
	}

	result, err := dice.Roll(expr)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "骰子表达式无效: "+err.Error(), ctx.IsGroup)
	}

	resp := fmt.Sprintf("🎲 %s", result.String())
	log.Printf("[DiceHandler] %s", resp)

	// 将骰子结果存入 Extra，供 Hook 或 Agent 读取
	if ctx.Extra == nil {
		ctx.Extra = make(map[string]interface{})
	}
	ctx.Extra["dice_result"] = result

	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// _ 确保 DiceHandler 实现 core.Handler 接口。
var _ core.Handler = (*DiceHandler)(nil)

// HelpHandler 处理帮助指令 (.help)。
type HelpHandler struct{}

// NewHelpHandler 创建帮助处理器。
func NewHelpHandler() *HelpHandler { return &HelpHandler{} }

func (h *HelpHandler) Name() string { return "help" }

func (h *HelpHandler) Match(ctx *core.MessageContext) bool {
	return ctx.Content == ".help" || ctx.Content == ".h"
}

func (h *HelpHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	help := `📋 指令列表:
  .r <表达式>     投掷骰子 (如 .r 3d6, .r 1d100+5)
  .mode <模式>    切换会话模式 (normal/trpg/freechat)
  .log <操作>     跑团日志 (start/end/show/export)
  .help           显示此帮助

跑团模式 (.mode trpg) 下，非指令消息将交给 AI KP 主持。`
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, help, ctx.IsGroup)
}

var _ core.Handler = (*HelpHandler)(nil)

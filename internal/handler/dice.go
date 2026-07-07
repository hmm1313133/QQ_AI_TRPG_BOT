// Package handler implements command-based bot functionality handlers.
// These handlers are part of the "code function layer", providing deterministic game tools.
// They share state with the AI Agent layer via Service for联动 (collaboration).
package handler

import (
	"fmt"
	"log"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/dice"
)

// DiceHandler handles dice roll commands (.r and .rh).
type DiceHandler struct {
	svc *trpg.Service
}

// NewDiceHandler creates a dice handler.
func NewDiceHandler(svc *trpg.Service) *DiceHandler {
	return &DiceHandler{svc: svc}
}

func (h *DiceHandler) Name() string { return "dice" }

func (h *DiceHandler) Match(ctx *core.MessageContext) bool {
	c := ctx.Content
	// Match .r and .rh, but NOT .ra, .rah, .rc, .rav, etc.
	if c == ".r" || strings.HasPrefix(c, ".r ") {
		return true
	}
	if c == ".rh" || strings.HasPrefix(c, ".rh ") {
		return true
	}
	return false
}

func (h *DiceHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	content := ctx.Content
	hidden := strings.HasPrefix(content, ".rh")

	// Extract expression
	parts := strings.SplitN(content, " ", 2)
	expr := "1d100"
	if len(parts) > 1 {
		expr = strings.TrimSpace(parts[1])
	}

	// Check for default dice sides override
	if expr == "1d100" {
		if n := h.svc.GetDefaultDice(ctx.SessionID); n > 0 {
			expr = fmt.Sprintf("1d%d", n)
		}
	}

	result, err := dice.Roll(expr)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "骰子表达式无效: "+err.Error(), ctx.IsGroup)
	}

	// Store result in Extra for hooks/agents
	if ctx.Extra == nil {
		ctx.Extra = make(map[string]interface{})
	}
	ctx.Extra["dice_result"] = result

	// Store in core.Session for AI Agent to read
	h.svc.StoreDiceResult(ctx.SessionID, result.String(), result.Total)

	if hidden {
		// Hidden roll: show brief message, full result stored in session
		resp := fmt.Sprintf("🔒 暗骰: %s", result.String())
		log.Printf("[DiceHandler] %s", resp)
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
	}

	resp := fmt.Sprintf("🎲 %s", result.String())
	log.Printf("[DiceHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

var _ core.Handler = (*DiceHandler)(nil)

// HelpHandler handles the help command (.help / .h).
type HelpHandler struct{}

func NewHelpHandler() *HelpHandler { return &HelpHandler{} }

func (h *HelpHandler) Name() string { return "help" }

func (h *HelpHandler) Match(ctx *core.MessageContext) bool {
	return ctx.Content == ".help" || ctx.Content == ".h"
}

func (h *HelpHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	help := `📋 指令列表:

【通用】
  .r <表达式>        投掷骰子 (如 .r 3d6, .r 1d100+5, .r 4d6kh3)
  .rh <表达式>       暗骰 (结果仅自己可见)
  .set coc|dnd       切换规则集
  .set <面数>        设置默认骰子面数
  .mode <模式>       切换会话模式 (normal/trpg/freechat)
  .log <操作>        跑团日志 (start/end/show/export)
  .help              显示此帮助

【角色卡】
  .pc new <名>       创建角色卡
  .pc tag <名>       绑定角色卡到当前群
  .pc list           列出所有角色卡
  .pc del <名>       删除角色卡
  .nn <名>           切换/重命名角色
  .st <属性> <值>    录入属性/技能
  .st show [属性]    查看角色卡数据

【CoC 7版】 (.set coc 后生效)
  .ra [b/p] <技能> [值]   技能检定 (b=奖励骰 p=惩罚骰)
  .rah <技能>             暗骰检定
  .sc <成功>/<失败>       SAN 检定 (如 .sc 0/1, .sc 1/1d4)
  .en <技能>              技能成长
  .coc [数量]             生成属性
  .ti / .li               疯狂症状 (即时/总结)
  .setcoc [编号]          房规设置
  .rav <自身> <对手>      对抗检定

【DnD 5e】 (.set dnd 后生效)
  .rc [优势|劣势] <调整值|技能>  检定
  .dnd [数量]                    生成属性 (4d6kh3)
  .ri [角色名] <值|+调整值|=表达式>  先攻
  .init [clear]                  先攻列表
  .ds                            死亡豁免
  .longrest                      长休

跑团模式 (.mode trpg) 下，非指令消息将交给 AI KP 主持。`
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, help, ctx.IsGroup)
}

var _ core.Handler = (*HelpHandler)(nil)

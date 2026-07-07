// Package handler — ruleset switching commands.
// Implements: .set coc/dnd (switch ruleset), .set <面数> (default dice sides).
// All game logic is delegated to trpg.Service.
package handler

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"
)

// RulesetHandler handles ruleset switching commands.
type RulesetHandler struct {
	svc *trpg.Service
}

// NewRulesetHandler creates a ruleset handler.
func NewRulesetHandler(svc *trpg.Service) *RulesetHandler {
	return &RulesetHandler{svc: svc}
}

func (h *RulesetHandler) Name() string { return "ruleset" }

func (h *RulesetHandler) Match(ctx *core.MessageContext) bool {
	// Match .set and .set <args> but NOT .setcoc (handled by CoC handler)
	c := ctx.Content
	if c == ".set" {
		return true
	}
	if strings.HasPrefix(c, ".set ") {
		return true
	}
	return false
}

func (h *RulesetHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	parts := strings.SplitN(ctx.Content, " ", 2)
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	if args == "" {
		return h.showStatus(ctx, reply)
	}

	// Check if it's a ruleset name
	switch strings.ToLower(args) {
	case "coc", "coc7":
		return h.setRuleset(ctx, reply, "coc7")
	case "dnd", "dnd5e":
		return h.setRuleset(ctx, reply, "dnd5e")
	}

	// Try to parse as default dice sides
	if n, err := strconv.Atoi(args); err == nil && n > 0 {
		return h.setDefaultDice(ctx, reply, n)
	}

	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		"用法: .set <coc|dnd|面数>\n例如: .set coc, .set dnd, .set 100", ctx.IsGroup)
}

var _ core.Handler = (*RulesetHandler)(nil)

func (h *RulesetHandler) showStatus(ctx *core.MessageContext, reply core.ReplyFunc) error {
	rs := h.svc.GetRuleSet(ctx.SessionID)
	rsName := "未设置"
	if rs != nil {
		rsName = rs.Name()
	}

	defaultDice := h.svc.GetDefaultDice(ctx.SessionID)
	if defaultDice == 0 {
		defaultDice = 100
	}

	resp := fmt.Sprintf("当前规则集: %s\n默认骰子面数: %d\n可用规则集: coc, dnd", rsName, defaultDice)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

func (h *RulesetHandler) setRuleset(ctx *core.MessageContext, reply core.ReplyFunc, name string) error {
	if err := h.svc.SetRuleSet(ctx.SessionID, name); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	label := "CoC 7版"
	if name == "dnd5e" {
		label = "DnD 5e"
	}
	resp := fmt.Sprintf("✅ 规则集已切换为: %s", label)
	log.Printf("[RulesetHandler] session=%s ruleset=%s", ctx.SessionID, name)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

func (h *RulesetHandler) setDefaultDice(ctx *core.MessageContext, reply core.ReplyFunc, sides int) error {
	h.svc.SetDefaultDice(ctx.SessionID, sides)

	resp := fmt.Sprintf("✅ 默认骰子面数已设置为: %d", sides)
	log.Printf("[RulesetHandler] session=%s default_dice=%d", ctx.SessionID, sides)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

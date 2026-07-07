// Package handler — DnD 5th Edition command handler.
// Implements: .rc (ability check), .dnd (attribute generation),
// .ri/.init (initiative), .ds (death save), .longrest.
// All game logic is delegated to trpg.Service.
package handler

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/ruleset"
)

// DnDHandler handles all DnD 5e commands.
type DnDHandler struct {
	svc *trpg.Service
}

// NewDnDHandler creates a DnD command handler.
func NewDnDHandler(svc *trpg.Service) *DnDHandler {
	return &DnDHandler{svc: svc}
}

func (h *DnDHandler) Name() string { return "dnd" }

func (h *DnDHandler) Match(ctx *core.MessageContext) bool {
	c := ctx.Content
	if strings.HasPrefix(c, ".rc ") || c == ".rc" {
		return true
	}
	if strings.HasPrefix(c, ".dnd") {
		return true
	}
	if strings.HasPrefix(c, ".ri ") || c == ".ri" {
		return true
	}
	if c == ".init" || strings.HasPrefix(c, ".init ") {
		return true
	}
	if c == ".ds" || strings.HasPrefix(c, ".ds ") {
		return true
	}
	if c == ".longrest" {
		return true
	}
	return false
}

func (h *DnDHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	parts := strings.SplitN(ctx.Content, " ", 2)
	cmd := parts[0]
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case ".rc":
		return h.handleCheck(ctx, reply, args)
	case ".dnd":
		return h.handleGenerateAttrs(ctx, reply, args)
	case ".ri":
		return h.handleInitiative(ctx, reply, args)
	case ".init":
		return h.handleInitList(ctx, reply, args)
	case ".ds":
		return h.handleDeathSave(ctx, reply)
	case ".longrest":
		return h.handleLongRest(ctx, reply)
	default:
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "未知指令", ctx.IsGroup)
	}
}

var _ core.Handler = (*DnDHandler)(nil)

// --- command handlers ---

// handleCheck processes .rc [优势|劣势] <modifier|skill>.
func (h *DnDHandler) handleCheck(ctx *core.MessageContext, reply core.ReplyFunc, args string) error {
	advantage, disadvantage, remaining := parseDnDCheckArgs(args)
	if remaining == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			"用法: .rc [优势|劣势] <调整值|技能>\n例如: .rc +5, .rc 优势 侦查, .rc -3", ctx.IsGroup)
	}

	// Try to parse as modifier
	modifier := 0
	hasModifier := false
	token := strings.TrimPrefix(remaining, "+")
	if n, err := strconv.Atoi(token); err == nil {
		modifier = n
		hasModifier = true
	}

	skill := ""
	if !hasModifier {
		// Not a number: look up from character card via Service
		skill = remaining
	}

	result, err := h.svc.SkillCheck(ctx.SessionID, ctx.UserID, skill, modifier, ruleset.CheckOptions{
		Advantage:    advantage,
		Disadvantage: disadvantage,
	})
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	charName := h.svc.GetCharName(ctx.SessionID, ctx.UserID)
	label := "检定"
	if skill != "" {
		label = skill + "检定"
	}
	resp := fmt.Sprintf("🎲 %s %s: %s", charName, label, result.Detail)
	log.Printf("[DnDHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// handleGenerateAttrs processes .dnd [count].
func (h *DnDHandler) handleGenerateAttrs(ctx *core.MessageContext, reply core.ReplyFunc, args string) error {
	count := 1
	if args != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(args)); err == nil && n > 0 && n <= 10 {
			count = n
		}
	}

	attrNames := []string{"力量", "敏捷", "体质", "智力", "感知", "魅力"}
	var sb strings.Builder
	sb.WriteString("🎲 DnD 5e 属性生成 (4d6kh3):\n")
	for i := 0; i < count; i++ {
		attrs, err := h.svc.GenerateAttrs(ctx.SessionID)
		if err != nil {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
		}
		if count > 1 {
			sb.WriteString(fmt.Sprintf("— 第%d组 —\n", i+1))
		}
		for _, name := range attrNames {
			mod := (attrs[name] - 10) / 2
			modStr := fmt.Sprintf("%+d", mod)
			sb.WriteString(fmt.Sprintf("%s:%d(%s) ", name, attrs[name], modStr))
		}
		sb.WriteString("\n")
	}

	log.Printf("[DnDHandler] 属性生成 x%d", count)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, sb.String(), ctx.IsGroup)
}

// handleInitiative processes .ri [name] <value|+mod|=expr>.
func (h *DnDHandler) handleInitiative(ctx *core.MessageContext, reply core.ReplyFunc, args string) error {
	tokens := strings.Fields(args)
	if len(tokens) == 0 {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			"用法: .ri [角色名] <数值|+调整值|=骰子表达式>\n例如: .ri 12, .ri +2, .ri =1d20+3, .ri 张三 15", ctx.IsGroup)
	}

	// Determine if first token is a name or a value
	name := h.svc.GetCharName(ctx.SessionID, ctx.UserID)
	valueToken := tokens[0]
	if len(tokens) > 1 {
		name = tokens[0]
		valueToken = tokens[1]
	}

	var initValue int
	if strings.HasPrefix(valueToken, "=") {
		// Dice expression
		result, err := h.svc.RollDice(ctx.SessionID, valueToken[1:])
		if err != nil {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "骰子表达式无效: "+err.Error(), ctx.IsGroup)
		}
		initValue = result.Total
		h.svc.SetInitiative(ctx.SessionID, name, initValue)
	} else if strings.HasPrefix(valueToken, "+") || strings.HasPrefix(valueToken, "-") {
		// Modifier: roll 1d20 + modifier
		mod, err := strconv.Atoi(valueToken)
		if err != nil {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "调整值无效: "+valueToken, ctx.IsGroup)
		}
		val, err := h.svc.RollInitiative(ctx.SessionID, name, mod)
		if err != nil {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "先攻骰失败: "+err.Error(), ctx.IsGroup)
		}
		initValue = val
	} else {
		// Direct value
		n, err := strconv.Atoi(valueToken)
		if err != nil {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "数值无效: "+valueToken, ctx.IsGroup)
		}
		initValue = n
		h.svc.SetInitiative(ctx.SessionID, name, initValue)
	}

	resp := fmt.Sprintf("⚔️ %s 先攻: %d", name, initValue)
	log.Printf("[DnDHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// handleInitList processes .init [clear].
func (h *DnDHandler) handleInitList(ctx *core.MessageContext, reply core.ReplyFunc, args string) error {
	if strings.TrimSpace(args) == "clear" {
		h.svc.ClearInitiative(ctx.SessionID)
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "✅ 先攻列表已清空", ctx.IsGroup)
	}

	initList := h.svc.GetInitList(ctx.SessionID)
	if len(initList) == 0 {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "先攻列表为空，使用 .ri <数值> 添加", ctx.IsGroup)
	}

	// Sort by initiative descending
	type entry struct {
		name  string
		value int
	}
	entries := make([]entry, 0, len(initList))
	for name, value := range initList {
		entries = append(entries, entry{name, value})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].value > entries[j].value
	})

	var sb strings.Builder
	sb.WriteString("⚔️ 先攻列表:\n")
	for i, e := range entries {
		sb.WriteString(fmt.Sprintf("%d. %s — %d\n", i+1, e.name, e.value))
	}
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, sb.String(), ctx.IsGroup)
}

// handleDeathSave processes .ds.
func (h *DnDHandler) handleDeathSave(ctx *core.MessageContext, reply core.ReplyFunc) error {
	result, err := h.svc.DeathSave(ctx.SessionID, ctx.UserID)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	charName := h.svc.GetCharName(ctx.SessionID, ctx.UserID)
	resp := fmt.Sprintf("💀 %s %s", charName, result.Detail)
	log.Printf("[DnDHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// handleLongRest processes .longrest.
func (h *DnDHandler) handleLongRest(ctx *core.MessageContext, reply core.ReplyFunc) error {
	maxHP, err := h.svc.LongRest(ctx.SessionID, ctx.UserID)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	charName := h.svc.GetCharName(ctx.SessionID, ctx.UserID)
	resp := fmt.Sprintf("😴 %s 完成长休: 重置死亡豁免计数", charName)
	if maxHP > 0 {
		resp = fmt.Sprintf("😴 %s 完成长休: HP恢复至%d，重置死亡豁免计数", charName, maxHP)
	}
	log.Printf("[DnDHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// --- helpers ---

// parseDnDCheckArgs parses .rc arguments: [优势|劣势] <remaining>.
func parseDnDCheckArgs(args string) (advantage, disadvantage bool, remaining string) {
	tokens := strings.Fields(args)
	idx := 0
	for idx < len(tokens) {
		t := strings.ToLower(tokens[idx])
		if t == "优势" || t == "advantage" || t == "adv" {
			advantage = true
			idx++
			continue
		}
		if t == "劣势" || t == "disadvantage" || t == "dis" {
			disadvantage = true
			idx++
			continue
		}
		break
	}
	remaining = strings.Join(tokens[idx:], " ")
	return
}

// Package handler — character card management commands.
// Implements: .pc new/tag/list/del/save, .nn (rename/switch), .st show/del/录入.
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

// CharacterHandler handles character card commands.
type CharacterHandler struct {
	svc *trpg.Service
}

// NewCharacterHandler creates a character card handler.
func NewCharacterHandler(svc *trpg.Service) *CharacterHandler {
	return &CharacterHandler{svc: svc}
}

func (h *CharacterHandler) Name() string { return "character" }

func (h *CharacterHandler) Match(ctx *core.MessageContext) bool {
	c := ctx.Content
	if strings.HasPrefix(c, ".pc ") || c == ".pc" {
		return true
	}
	if strings.HasPrefix(c, ".nn ") || c == ".nn" {
		return true
	}
	if strings.HasPrefix(c, ".st ") || c == ".st" {
		return true
	}
	return false
}

func (h *CharacterHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	parts := strings.SplitN(ctx.Content, " ", 2)
	cmd := parts[0]
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case ".pc":
		return h.handlePc(ctx, reply, args)
	case ".nn":
		return h.handleNn(ctx, reply, args)
	case ".st":
		return h.handleSt(ctx, reply, args)
	default:
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "未知指令", ctx.IsGroup)
	}
}

var _ core.Handler = (*CharacterHandler)(nil)

// --- .pc commands ---

func (h *CharacterHandler) handlePc(ctx *core.MessageContext, reply core.ReplyFunc, args string) error {
	parts := strings.SplitN(args, " ", 2)
	subCmd := parts[0]
	subArgs := ""
	if len(parts) > 1 {
		subArgs = strings.TrimSpace(parts[1])
	}

	switch subCmd {
	case "new":
		return h.pcNew(ctx, reply, subArgs)
	case "tag":
		return h.pcTag(ctx, reply, subArgs)
	case "list":
		return h.pcList(ctx, reply)
	case "del":
		return h.pcDel(ctx, reply, subArgs)
	case "save":
		return h.pcSave(ctx, reply)
	case "":
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			"用法: .pc <new|tag|list|del|save> [角色名]", ctx.IsGroup)
	default:
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
			"未知子指令: "+subCmd+"\n可用: new, tag, list, del, save", ctx.IsGroup)
	}
}

func (h *CharacterHandler) pcNew(ctx *core.MessageContext, reply core.ReplyFunc, name string) error {
	if name == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "请提供角色名: .pc new <角色名>", ctx.IsGroup)
	}

	card, err := h.svc.CreateCharacter(ctx.SessionID, ctx.UserID, name)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "创建角色卡失败: "+err.Error(), ctx.IsGroup)
	}

	resp := fmt.Sprintf("✅ 角色卡「%s」已创建并绑定 (规则: %s)", card.Name, card.System)
	log.Printf("[CharacterHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

func (h *CharacterHandler) pcTag(ctx *core.MessageContext, reply core.ReplyFunc, name string) error {
	if name == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "请提供角色名: .pc tag <角色名>", ctx.IsGroup)
	}

	card, err := h.svc.BindCharacter(ctx.SessionID, ctx.UserID, name)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	resp := fmt.Sprintf("✅ 已绑定角色卡「%s」(规则: %s)", card.Name, card.System)
	log.Printf("[CharacterHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

func (h *CharacterHandler) pcList(ctx *core.MessageContext, reply core.ReplyFunc) error {
	names := h.svc.ListCharacters(ctx.UserID)
	if len(names) == 0 {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "你还没有角色卡，使用 .pc new <角色名> 创建", ctx.IsGroup)
	}

	active := h.svc.GetActiveCharacter(ctx.SessionID, ctx.UserID)
	activeName := ""
	if active != nil {
		activeName = active.Name
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 你的角色卡 (%d):\n", len(names)))
	for _, name := range names {
		marker := "  "
		if name == activeName {
			marker = "▶ "
		}
		sb.WriteString(fmt.Sprintf("%s%s\n", marker, name))
	}
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, sb.String(), ctx.IsGroup)
}

func (h *CharacterHandler) pcDel(ctx *core.MessageContext, reply core.ReplyFunc, name string) error {
	if name == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "请提供角色名: .pc del <角色名>", ctx.IsGroup)
	}

	if err := h.svc.DeleteCharacter(ctx.SessionID, ctx.UserID, name); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	resp := fmt.Sprintf("✅ 角色卡「%s」已删除", name)
	log.Printf("[CharacterHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

func (h *CharacterHandler) pcSave(ctx *core.MessageContext, reply core.ReplyFunc) error {
	card := h.svc.GetActiveCharacter(ctx.SessionID, ctx.UserID)
	if card == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "未绑定角色卡", ctx.IsGroup)
	}
	// Cards are auto-saved, just confirm
	resp := fmt.Sprintf("✅ 角色卡「%s」已保存", card.Name)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// --- .nn command ---

func (h *CharacterHandler) handleNn(ctx *core.MessageContext, reply core.ReplyFunc, args string) error {
	name := strings.TrimSpace(args)
	if name == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "用法: .nn <角色名>\n切换到已有角色或重命名当前角色", ctx.IsGroup)
	}

	// If player has a card with this name, switch to it
	if card, err := h.svc.BindCharacter(ctx.SessionID, ctx.UserID, name); err == nil {
		resp := fmt.Sprintf("✅ 已切换到角色「%s」", card.Name)
		log.Printf("[CharacterHandler] %s", resp)
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
	}

	// Otherwise, rename the current character
	card := h.svc.GetActiveCharacter(ctx.SessionID, ctx.UserID)
	if card == nil {
		// No active card, create a new one
		return h.pcNew(ctx, reply, name)
	}

	// Rename: delete old, create new
	oldName := card.Name
	oldID := card.ID
	_ = h.svc.CharMgr().Delete(oldID)

	card.Name = name
	card.ID = ""
	card.FilePath = ""
	// Use CreateCharacter which will set ID from Player+Name
	if err := h.svc.CharMgr().Create(card); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "重命名失败: "+err.Error(), ctx.IsGroup)
	}
	h.svc.SetActiveCharacter(ctx.SessionID, ctx.UserID, card)

	resp := fmt.Sprintf("✅ 角色已从「%s」重命名为「%s」", oldName, name)
	log.Printf("[CharacterHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// --- .st command ---

func (h *CharacterHandler) handleSt(ctx *core.MessageContext, reply core.ReplyFunc, args string) error {
	if args == "" || args == "show" {
		return h.stShow(ctx, reply, "")
	}

	parts := strings.SplitN(args, " ", 2)
	subCmd := parts[0]

	if subCmd == "show" {
		attr := ""
		if len(parts) > 1 {
			attr = strings.TrimSpace(parts[1])
		}
		return h.stShow(ctx, reply, attr)
	}

	if subCmd == "del" || subCmd == "rm" {
		if len(parts) < 2 {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "用法: .st del <属性名>", ctx.IsGroup)
		}
		return h.stDel(ctx, reply, strings.TrimSpace(parts[1]))
	}

	// Try to parse as "<attr> <value>" (space-separated)
	if len(parts) == 2 {
		attr := parts[0]
		if n, err := strconv.Atoi(parts[1]); err == nil {
			return h.stSet(ctx, reply, attr, n)
		}
	}

	// Try to parse as "<attr><value>" (no space, e.g. "力量60")
	if attr, value, ok := parseAttrValue(subCmd); ok {
		return h.stSet(ctx, reply, attr, value)
	}

	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID,
		"用法: .st <属性> <值> 或 .st <属性><值>\n例如: .st 力量 60, .st 力量60, .st show, .st del 力量", ctx.IsGroup)
}

func (h *CharacterHandler) stShow(ctx *core.MessageContext, reply core.ReplyFunc, filter string) error {
	card := h.svc.GetActiveCharacter(ctx.SessionID, ctx.UserID)
	if card == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "未绑定角色卡，请先使用 .pc new 创建", ctx.IsGroup)
	}

	if filter != "" {
		// Show specific attribute
		if v, ok := card.Skills[filter]; ok {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, fmt.Sprintf("%s: %d", filter, v), ctx.IsGroup)
		}
		if v, ok := card.Attrs[filter]; ok {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, fmt.Sprintf("%s: %d", filter, v), ctx.IsGroup)
		}
		if v, ok := card.Status[filter]; ok {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, fmt.Sprintf("%s: %d", filter, v), ctx.IsGroup)
		}
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, fmt.Sprintf("未找到属性「%s」", filter), ctx.IsGroup)
	}

	// Show all
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 角色卡: %s (%s)\n", card.Name, card.System))

	if len(card.Attrs) > 0 {
		sb.WriteString("【属性】\n")
		for k, v := range card.Attrs {
			sb.WriteString(fmt.Sprintf("  %s: %d\n", k, v))
		}
	}
	if len(card.Skills) > 0 {
		sb.WriteString("【技能】\n")
		for k, v := range card.Skills {
			sb.WriteString(fmt.Sprintf("  %s: %d\n", k, v))
		}
	}
	if len(card.Status) > 0 {
		sb.WriteString("【状态】\n")
		for k, v := range card.Status {
			sb.WriteString(fmt.Sprintf("  %s: %d\n", k, v))
		}
	}

	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, sb.String(), ctx.IsGroup)
}

func (h *CharacterHandler) stSet(ctx *core.MessageContext, reply core.ReplyFunc, attr string, value int) error {
	card := h.svc.GetActiveCharacter(ctx.SessionID, ctx.UserID)
	if card == nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "未绑定角色卡，请先使用 .pc new 创建", ctx.IsGroup)
	}

	if err := h.svc.SetCharacterAttr(ctx.SessionID, ctx.UserID, attr, value); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	resp := fmt.Sprintf("✅ %s: %s = %d", card.Name, attr, value)
	log.Printf("[CharacterHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

func (h *CharacterHandler) stDel(ctx *core.MessageContext, reply core.ReplyFunc, attr string) error {
	if err := h.svc.DeleteCharacterAttr(ctx.SessionID, ctx.UserID, attr); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	resp := fmt.Sprintf("✅ 已删除属性「%s」", attr)
	log.Printf("[CharacterHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// parseAttrValue parses tokens like "力量60" into attr="力量", value=60.
func parseAttrValue(s string) (attr string, value int, ok bool) {
	i := 0
	for i < len(s) && (s[i] < '0' || s[i] > '9') {
		// Handle UTF-8: count runes, not bytes
		r := rune(s[i])
		if r >= 0x80 {
			// Multi-byte character, advance to next rune
			_, size := decodeRune(s[i:])
			i += size
		} else {
			i++
		}
	}
	if i == 0 || i >= len(s) {
		return "", 0, false
	}
	attr = s[:i]
	n, err := strconv.Atoi(s[i:])
	if err != nil {
		return "", 0, false
	}
	return attr, n, true
}

// decodeRune decodes the first UTF-8 rune in s and returns the rune and its byte size.
func decodeRune(s string) (rune, int) {
	for i, r := range s {
		return r, i
	}
	return 0, 0
}

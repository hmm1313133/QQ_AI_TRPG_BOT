// Package handler — CoC 7th Edition command handler.
// Implements: .ra/.rah (skill check), .sc (SAN check), .en (skill growth),
// .coc (attribute generation), .ti/.li (madness), .setcoc (house rules),
// .rav (opposed check).
// All game logic is delegated to trpg.Service.
package handler

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/core"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg"
	"github.com/hmm1313133/QQ_AI_TRPG_BOT/internal/trpg/ruleset"
)

// CoCHandler handles all Call of Cthulhu 7e commands.
type CoCHandler struct {
	svc *trpg.Service
}

// NewCoCHandler creates a CoC command handler.
func NewCoCHandler(svc *trpg.Service) *CoCHandler {
	return &CoCHandler{svc: svc}
}

func (h *CoCHandler) Name() string { return "coc" }

func (h *CoCHandler) Match(ctx *core.MessageContext) bool {
	c := ctx.Content
	// .rah must be checked before .ra
	if strings.HasPrefix(c, ".rah ") || c == ".rah" {
		return true
	}
	if strings.HasPrefix(c, ".ra ") || c == ".ra" {
		return true
	}
	if strings.HasPrefix(c, ".sc ") || c == ".sc" {
		return true
	}
	if strings.HasPrefix(c, ".en ") || c == ".en" {
		return true
	}
	if strings.HasPrefix(c, ".coc") {
		return true
	}
	if c == ".ti" || strings.HasPrefix(c, ".ti ") {
		return true
	}
	if c == ".li" || strings.HasPrefix(c, ".li ") {
		return true
	}
	if c == ".setcoc" || strings.HasPrefix(c, ".setcoc ") {
		return true
	}
	if strings.HasPrefix(c, ".rav ") || c == ".rav" {
		return true
	}
	return false
}

func (h *CoCHandler) Execute(ctx *core.MessageContext, reply core.ReplyFunc) error {
	parts := strings.SplitN(ctx.Content, " ", 2)
	cmd := parts[0]
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	switch cmd {
	case ".ra", ".rah":
		return h.handleCheck(ctx, reply, args, cmd == ".rah")
	case ".sc":
		return h.handleSANCheck(ctx, reply, args)
	case ".en":
		return h.handleSkillGrowth(ctx, reply, args)
	case ".coc":
		return h.handleGenerateAttrs(ctx, reply, args)
	case ".ti":
		return h.handleMadness(ctx, reply, true)
	case ".li":
		return h.handleMadness(ctx, reply, false)
	case ".setcoc":
		return h.handleSetCoc(ctx, reply, args)
	case ".rav":
		return h.handleOpposedCheck(ctx, reply, args)
	default:
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "未知指令", ctx.IsGroup)
	}
}

var _ core.Handler = (*CoCHandler)(nil)

// --- command handlers ---

// handleCheck processes .ra and .rah (skill check with optional bonus/penalty dice).
func (h *CoCHandler) handleCheck(ctx *core.MessageContext, reply core.ReplyFunc, args string, hidden bool) error {
	skill, value, hasValue, bonus, penalty := parseCheckArgs(args)
	if skill == "" && !hasValue {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "用法: .ra <技能> [数值] 或 .ra <数值>\n例如: .ra 侦查, .ra 侦查 60, .ra b2 侦查", ctx.IsGroup)
	}

	// If no explicit value, Service will auto-lookup from character card
	if !hasValue {
		if skill == "" {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "请提供技能名称或数值", ctx.IsGroup)
		}
	} else if skill == "" {
		// Direct value check without skill name
		skill = "检定"
	}

	result, err := h.svc.SkillCheck(ctx.SessionID, ctx.UserID, skill, value, ruleset.CheckOptions{
		BonusDice:   bonus,
		PenaltyDice: penalty,
	})
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	charName := h.svc.GetCharName(ctx.SessionID, ctx.UserID)
	resp := fmt.Sprintf("🎲 %s %s检定: %s", charName, skill, result.Detail)
	if hidden {
		resp = fmt.Sprintf("🔒 %s 进行了暗骰检定 (%s)", charName, skill)
	}

	log.Printf("[CoCHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// handleSANCheck processes .sc <success_loss>/<fail_loss>.
func (h *CoCHandler) handleSANCheck(ctx *core.MessageContext, reply core.ReplyFunc, args string) error {
	parts := strings.Split(args, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "用法: .sc <成功损失>/<失败损失>\n例如: .sc 0/1, .sc 1/1d4", ctx.IsGroup)
	}

	result, err := h.svc.SANCheck(ctx.SessionID, ctx.UserID, parts[0], parts[1])
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	charName := h.svc.GetCharName(ctx.SessionID, ctx.UserID)
	resp := fmt.Sprintf("🧠 %s %s", charName, result.Detail)
	log.Printf("[CoCHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// handleSkillGrowth processes .en <技能>.
func (h *CoCHandler) handleSkillGrowth(ctx *core.MessageContext, reply core.ReplyFunc, args string) error {
	skill := strings.TrimSpace(args)
	if skill == "" {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "用法: .en <技能名>\n例如: .en 侦查", ctx.IsGroup)
	}

	result, err := h.svc.SkillGrowth(ctx.SessionID, ctx.UserID, skill)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	charName := h.svc.GetCharName(ctx.SessionID, ctx.UserID)
	resp := fmt.Sprintf("📈 %s %s", charName, result.Detail)
	log.Printf("[CoCHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// handleGenerateAttrs processes .coc [count].
func (h *CoCHandler) handleGenerateAttrs(ctx *core.MessageContext, reply core.ReplyFunc, args string) error {
	count := 1
	if args != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(args)); err == nil && n > 0 && n <= 10 {
			count = n
		}
	}

	var sb strings.Builder
	sb.WriteString("🎭 CoC 7版属性生成:\n")
	for i := 0; i < count; i++ {
		attrs, err := h.svc.GenerateAttrs(ctx.SessionID)
		if err != nil {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
		}
		if count > 1 {
			sb.WriteString(fmt.Sprintf("— 第%d组 —\n", i+1))
		}
		sb.WriteString(fmt.Sprintf("力量:%d 体质:%d 体型:%d 敏捷:%d 外貌:%d 智力:%d 意志:%d 教育:%d\n",
			attrs["力量"], attrs["体质"], attrs["体型"], attrs["敏捷"],
			attrs["外貌"], attrs["智力"], attrs["意志"], attrs["教育"]))
		sb.WriteString(fmt.Sprintf("幸运:%d SAN:%d HP:%d MP:%d\n",
			attrs["幸运"], attrs["SAN"], attrs["HP"], attrs["MP"]))
	}

	log.Printf("[CoCHandler] 属性生成 x%d", count)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, sb.String(), ctx.IsGroup)
}

// handleMadness processes .ti (temporary) and .li (underlying) madness.
func (h *CoCHandler) handleMadness(ctx *core.MessageContext, reply core.ReplyFunc, temporary bool) error {
	symptom, err := h.svc.RandomMadness(ctx.SessionID, temporary)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	label := "即时疯狂"
	if !temporary {
		label = "总结性疯狂"
	}

	resp := fmt.Sprintf("🤯 %s: %s", label, symptom)
	log.Printf("[CoCHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// handleSetCoc processes .setcoc [index].
func (h *CoCHandler) handleSetCoc(ctx *core.MessageContext, reply core.ReplyFunc, args string) error {
	if args == "" {
		current, all, err := h.svc.GetHouseRules(ctx.SessionID)
		if err != nil {
			return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
		}
		var sb strings.Builder
		sb.WriteString("🏠 CoC 房规设置:\n")
		sb.WriteString(fmt.Sprintf("当前: %s\n", current.Name))
		sb.WriteString("可选:\n")
		for i, hr := range all {
			sb.WriteString(fmt.Sprintf("  %d. %s\n", i, hr.Name))
		}
		sb.WriteString("使用 .setcoc <编号> 切换")
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, sb.String(), ctx.IsGroup)
	}

	idx, err := strconv.Atoi(strings.TrimSpace(args))
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "请输入房规编号 (0-4)", ctx.IsGroup)
	}
	if err := h.svc.SetHouseRule(ctx.SessionID, idx); err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	current, _, _ := h.svc.GetHouseRules(ctx.SessionID)
	resp := fmt.Sprintf("✅ CoC 房规已切换为: %s", current.Name)
	log.Printf("[CoCHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// handleOpposedCheck processes .rav <self_skill> <opp_skill>.
func (h *CoCHandler) handleOpposedCheck(ctx *core.MessageContext, reply core.ReplyFunc, args string) error {
	tokens := strings.Fields(args)
	if len(tokens) < 2 {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "用法: .rav <自身技能值> <对手技能值>\n例如: .rav 60 50", ctx.IsGroup)
	}

	selfVal, err := strconv.Atoi(tokens[0])
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "自身技能值无效: "+tokens[0], ctx.IsGroup)
	}
	oppVal, err := strconv.Atoi(tokens[1])
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, "对手技能值无效: "+tokens[1], ctx.IsGroup)
	}

	result, err := h.svc.OpposedCheck(ctx.SessionID, selfVal, oppVal)
	if err != nil {
		return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, err.Error(), ctx.IsGroup)
	}

	charName := h.svc.GetCharName(ctx.SessionID, ctx.UserID)
	resp := fmt.Sprintf("⚔️ %s %s", charName, result.Detail)
	log.Printf("[CoCHandler] %s", resp)
	return reply(ctx.Ctx, ctx.OpenID, ctx.MsgID, resp, ctx.IsGroup)
}

// --- helpers ---

// parseCheckArgs parses .ra arguments: [b/p dice] <skill|value> [value].
// Returns skill name, explicit value (if provided), and bonus/penalty dice counts.
func parseCheckArgs(args string) (skill string, value int, hasValue bool, bonus, penalty int) {
	tokens := strings.Fields(args)
	idx := 0

	// Parse bonus/penalty dice tokens (b2, p1, b1p2, etc.)
	for idx < len(tokens) {
		b, p, ok := parseBonusPenalty(tokens[idx])
		if ok {
			bonus += b
			penalty += p
			idx++
		} else {
			break
		}
	}

	if idx >= len(tokens) {
		return "", 0, false, bonus, penalty
	}

	// If first remaining token is a number, treat as direct value check
	if n, err := strconv.Atoi(tokens[idx]); err == nil {
		return "", n, true, bonus, penalty
	}

	// First token is skill name
	skill = tokens[idx]
	idx++

	// Optional explicit value
	if idx < len(tokens) {
		if n, err := strconv.Atoi(tokens[idx]); err == nil {
			value = n
			hasValue = true
		}
	}

	return skill, value, hasValue, bonus, penalty
}

// parseBonusPenalty parses tokens like "b2", "p1", "b1p2", "p1b1".
// Returns bonus count, penalty count, and whether the token was valid.
func parseBonusPenalty(token string) (bonus, penalty int, ok bool) {
	token = strings.ToLower(token)
	if len(token) < 2 || (token[0] != 'b' && token[0] != 'p') {
		return 0, 0, false
	}

	i := 0
	for i < len(token) {
		isBonus := token[i] == 'b'
		isPenalty := token[i] == 'p'
		if !isBonus && !isPenalty {
			return 0, 0, false
		}
		i++
		numStart := i
		for i < len(token) && token[i] >= '0' && token[i] <= '9' {
			i++
		}
		if i == numStart {
			return 0, 0, false // no digits after letter
		}
		n, _ := strconv.Atoi(token[numStart:i])
		if isBonus {
			bonus += n
		} else {
			penalty += n
		}
	}
	return bonus, penalty, true
}
